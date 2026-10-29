package controller

import (
	"context"
	"testing"
	"time"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/kubevirt"
	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var dnsOperatorTestGVK = schema.GroupVersionKind{
	Group:   "operator.openshift.io",
	Version: "v1",
	Kind:    "DNS",
}

func TestReconcileDNSProxyWaitsForReadinessInputs(t *testing.T) {
	tests := []struct {
		name            string
		serviceIPs      *kubevirt.ServiceIPs
		infraCoreDNSIP  string
		routeAdmitted   bool
		wantConfigMap   bool
		wantRequeueFor  time.Duration
		wantAnnotation  bool
		withDNSService  bool
		withDNSOperator bool
	}{
		{
			name:           "waits for the API service ClusterIP",
			infraCoreDNSIP: "10.96.0.10",
			routeAdmitted:  true,
			wantRequeueFor: 30 * time.Second,
		},
		{
			name: "waits for Route admission",
			serviceIPs: &kubevirt.ServiceIPs{
				APIClusterIP: "10.96.10.10",
			},
			infraCoreDNSIP: "10.96.0.10",
			wantRequeueFor: 30 * time.Second,
		},
		{
			name: "waits for the infra CoreDNS ClusterIP",
			serviceIPs: &kubevirt.ServiceIPs{
				APIClusterIP: "10.96.10.10",
			},
			routeAdmitted:  true,
			wantRequeueFor: 30 * time.Second,
		},
		{
			name: "waits for the DNS proxy service ClusterIP",
			serviceIPs: &kubevirt.ServiceIPs{
				APIClusterIP: "10.96.10.10",
			},
			infraCoreDNSIP: "10.96.0.10",
			routeAdmitted:  true,
			wantConfigMap:  true,
			wantRequeueFor: 30 * time.Second,
		},
		{
			name: "creates the DNS forwarding rule when ready",
			serviceIPs: &kubevirt.ServiceIPs{
				APIClusterIP: "10.96.10.10",
			},
			infraCoreDNSIP:  "10.96.0.10",
			routeAdmitted:   true,
			wantConfigMap:   true,
			wantAnnotation:  true,
			withDNSService:  true,
			withDNSOperator: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			oacp := newDNSProxyTestOACP()
			client := newDNSProxyTestClient(t, oacp, dnsProxyTestRoute(test.routeAdmitted))
			if test.withDNSService {
				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{Name: "tenant-dns", Namespace: "test-ns"},
					Spec: corev1.ServiceSpec{
						ClusterIP: "10.96.53.10",
					},
				}
				if err := client.Create(ctx, service); err != nil {
					t.Fatalf("failed to create DNS proxy service: %v", err)
				}
			}
			if test.withDNSOperator {
				dnsOperator := &unstructured.Unstructured{}
				dnsOperator.SetGroupVersionKind(dnsOperatorTestGVK)
				dnsOperator.SetName("default")
				dnsOperator.Object["spec"] = map[string]interface{}{}
				if err := client.Create(ctx, dnsOperator); err != nil {
					t.Fatalf("failed to create DNS operator object: %v", err)
				}
			}

			reconciler := &ClusterDeploymentReconciler{Client: client, Scheme: client.Scheme()}
			manifestDNSIPs, result, err := reconciler.reconcileDNSProxy(
				ctx,
				oacp,
				&kubevirt.InfraClientResult{Client: client, Namespace: "test-ns"},
				dnsProxyTestClusterDeployment(),
				test.serviceIPs,
				test.infraCoreDNSIP,
			)
			if err != nil {
				t.Fatalf("reconcileDNSProxy() error = %v", err)
			}
			if test.serviceIPs != nil && manifestDNSIPs.APIClusterIP != test.serviceIPs.APIClusterIP {
				t.Fatalf("manifest API IP = %q, want %q", manifestDNSIPs.APIClusterIP, test.serviceIPs.APIClusterIP)
			}
			if result.RequeueAfter != test.wantRequeueFor {
				t.Fatalf("requeueAfter = %v, want %v", result.RequeueAfter, test.wantRequeueFor)
			}

			configMap := &corev1.ConfigMap{}
			configMapErr := client.Get(ctx, types.NamespacedName{Name: "tenant-dns-corefile", Namespace: "test-ns"}, configMap)
			if test.wantConfigMap && configMapErr != nil {
				t.Fatalf("expected DNS proxy ConfigMap: %v", configMapErr)
			}
			if !test.wantConfigMap && !apierrors.IsNotFound(configMapErr) {
				t.Fatalf("expected no DNS proxy ConfigMap, got %v", configMapErr)
			}

			if test.wantAnnotation {
				updatedOACP := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
				if err := client.Get(ctx, types.NamespacedName{Name: "test-cluster", Namespace: "test-ns"}, updatedOACP); err != nil {
					t.Fatalf("failed to get OACP: %v", err)
				}
				if updatedOACP.Annotations[kubevirt.DNSRuleCreatedAnnotation] != "test-ns/test-cluster" {
					t.Fatalf("DNS rule annotation = %q, want %q", updatedOACP.Annotations[kubevirt.DNSRuleCreatedAnnotation], "test-ns/test-cluster")
				}
			}
		})
	}
}

func TestDNSProxyRequeue(t *testing.T) {
	tests := []struct {
		name             string
		kubeconfigReady  bool
		wantRequeueAfter time.Duration
	}{
		{
			name:             "during installation",
			wantRequeueAfter: 30 * time.Second,
		},
		{
			name:             "after installation",
			kubeconfigReady:  true,
			wantRequeueAfter: 5 * time.Minute,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			oacp := newDNSProxyTestOACP()
			if test.kubeconfigReady {
				oacp.Status.Conditions = []metav1.Condition{{
					Type:   string(controlplanev1alpha3.KubeconfigAvailableCondition),
					Status: metav1.ConditionTrue,
				}}
			}

			if got := dnsProxyRequeue(oacp); got.RequeueAfter != test.wantRequeueAfter {
				t.Fatalf("requeueAfter = %v, want %v", got.RequeueAfter, test.wantRequeueAfter)
			}
		})
	}
}

func newDNSProxyTestOACP() *controlplanev1alpha3.OpenshiftAssistedControlPlane {
	return &controlplanev1alpha3.OpenshiftAssistedControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "test-ns",
			UID:       types.UID("test-oacp"),
		},
		Spec: controlplanev1alpha3.OpenshiftAssistedControlPlaneSpec{
			Config: controlplanev1alpha3.OpenshiftAssistedControlPlaneConfigSpec{
				ClusterName: "test-cluster",
				BaseDomain:  "apps.mgmt.example.com",
			},
		},
	}
}

func dnsProxyTestClusterDeployment() *hivev1.ClusterDeployment {
	return &hivev1.ClusterDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "test-ns",
		},
		Spec: hivev1.ClusterDeploymentSpec{
			ClusterName: "test-cluster",
			BaseDomain:  "apps.mgmt.example.com",
		},
	}
}

func dnsProxyTestRoute(admitted bool) *routev1.Route {
	status := corev1.ConditionFalse
	if admitted {
		status = corev1.ConditionTrue
	}
	return &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-ingress-route",
			Namespace: "test-ns",
		},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{{
				RouterCanonicalHostname: "router-default.apps.mgmt.example.com",
				Conditions: []routev1.RouteIngressCondition{{
					Type:   routev1.RouteAdmitted,
					Status: status,
				}},
			}},
		},
	}
}

func newDNSProxyTestClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	utilruntime.Must(controlplanev1alpha3.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(configv1.AddToScheme(scheme))
	utilruntime.Must(routev1.Install(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(hivev1.AddToScheme(scheme))
	utilruntime.Must(hiveext.AddToScheme(scheme))
	dnsSchemeBuilder := runtime.NewSchemeBuilder(func(s *runtime.Scheme) error {
		s.AddKnownTypeWithName(dnsOperatorTestGVK, &unstructured.Unstructured{})
		s.AddKnownTypeWithName(
			schema.GroupVersionKind{Group: "operator.openshift.io", Version: "v1", Kind: "DNSList"},
			&unstructured.UnstructuredList{},
		)
		return nil
	})
	utilruntime.Must(dnsSchemeBuilder.AddToScheme(scheme))

	return fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()
}
