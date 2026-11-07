package kubevirt_test

import (
	"context"
	stderrors "errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/kubevirt"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("EnsureDNSProxy", func() {
	It("uses the admitted router CNAME and infra DNS upstream", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		utilruntime.Must(corev1.AddToScheme(scheme))
		utilruntime.Must(appsv1.AddToScheme(scheme))
		utilruntime.Must(controlplanev1alpha3.AddToScheme(scheme))
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

		oacp := &controlplanev1alpha3.OpenshiftAssistedControlPlane{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "test-ns"},
			Spec: controlplanev1alpha3.OpenshiftAssistedControlPlaneSpec{
				Config: controlplanev1alpha3.OpenshiftAssistedControlPlaneConfigSpec{
					ClusterName: "test-cluster",
					BaseDomain:  "apps.mgmt.example.com",
				},
			},
		}
		_, err := kubevirt.EnsureDNSProxy(ctx, fakeClient, oacp, &kubevirt.DNSProxyConfig{
			APIIP:        "10.96.10.10",
			IngressCNAME: "router-default.apps.mgmt.example.com",
			UpstreamDNS:  kubevirt.InfraDNSResolver,
		}, "test-ns")
		Expect(err).NotTo(HaveOccurred())

		configMap := &corev1.ConfigMap{}
		Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "tenant-dns-corefile", Namespace: "test-ns"}, configMap)).To(Succeed())
		Expect(configMap.Data["Corefile"]).To(ContainSubstring(".:5353"))
		Expect(configMap.Data["Corefile"]).To(ContainSubstring("rewrite stop name regex"))
		Expect(configMap.Data["Corefile"]).To(ContainSubstring("router-default.apps.mgmt.example.com."))
		Expect(configMap.Data["Corefile"]).To(ContainSubstring("forward . /etc/resolv.conf"))
		Expect(configMap.Data).NotTo(HaveKey("apps.db"))
	})

	It("updates the router rewrite when the admitted router CNAME changes", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		utilruntime.Must(corev1.AddToScheme(scheme))
		utilruntime.Must(appsv1.AddToScheme(scheme))
		utilruntime.Must(controlplanev1alpha3.AddToScheme(scheme))
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

		oacp := &controlplanev1alpha3.OpenshiftAssistedControlPlane{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "test-ns"},
			Spec: controlplanev1alpha3.OpenshiftAssistedControlPlaneSpec{
				Config: controlplanev1alpha3.OpenshiftAssistedControlPlaneConfigSpec{
					ClusterName: "test-cluster",
					BaseDomain:  "apps.mgmt.example.com",
				},
			},
		}
		for _, cname := range []string{
			"router-old.apps.mgmt.example.com",
			"router-new.apps.mgmt.example.com",
		} {
			_, err := kubevirt.EnsureDNSProxy(ctx, fakeClient, oacp, &kubevirt.DNSProxyConfig{
				APIIP:        "10.96.10.10",
				IngressCNAME: cname,
				UpstreamDNS:  kubevirt.InfraDNSResolver,
			}, "test-ns")
			Expect(err).NotTo(HaveOccurred())
		}

		configMap := &corev1.ConfigMap{}
		Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "tenant-dns-corefile", Namespace: "test-ns"}, configMap)).To(Succeed())
		Expect(configMap.Data["Corefile"]).To(ContainSubstring("router-new.apps.mgmt.example.com."))
		Expect(configMap.Data["Corefile"]).NotTo(ContainSubstring("router-old.apps.mgmt.example.com."))
	})

	It("reports missing DNS inputs as not ready", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		utilruntime.Must(corev1.AddToScheme(scheme))
		utilruntime.Must(appsv1.AddToScheme(scheme))
		utilruntime.Must(controlplanev1alpha3.AddToScheme(scheme))
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

		oacp := &controlplanev1alpha3.OpenshiftAssistedControlPlane{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "test-ns"},
		}
		_, err := kubevirt.EnsureDNSProxy(ctx, fakeClient, oacp, &kubevirt.DNSProxyConfig{}, "test-ns")
		Expect(stderrors.Is(err, kubevirt.ErrDNSProxyNotReady)).To(BeTrue())
	})
})

var _ = Describe("LookupInfraCoreDNSIP", func() {
	It("returns the actual dns-default ClusterIP", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		utilruntime.Must(corev1.AddToScheme(scheme))
		dnsService := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "dns-default", Namespace: "openshift-dns"},
			Spec:       corev1.ServiceSpec{ClusterIP: "10.96.100.53"},
		}
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dnsService).Build()

		ip, err := kubevirt.LookupInfraCoreDNSIP(ctx, fakeClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(ip).To(Equal("10.96.100.53"))
	})

	It("does not guess when the dns-default Service is unavailable", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		utilruntime.Must(corev1.AddToScheme(scheme))
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

		ip, err := kubevirt.LookupInfraCoreDNSIP(ctx, fakeClient)
		Expect(err).To(HaveOccurred())
		Expect(ip).To(BeEmpty())
	})
})

var _ = Describe("GetTenantDNSProxyClusterIP", func() {
	It("returns the namespaced proxy Service ClusterIP", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		utilruntime.Must(corev1.AddToScheme(scheme))
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "tenant-dns", Namespace: "test-ns"},
			Spec:       corev1.ServiceSpec{ClusterIP: "10.96.53.10"},
		}
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(service).Build()

		ip, err := kubevirt.GetTenantDNSProxyClusterIP(ctx, fakeClient, "test-ns")
		Expect(err).NotTo(HaveOccurred())
		Expect(ip).To(Equal("10.96.53.10"))
	})
})
