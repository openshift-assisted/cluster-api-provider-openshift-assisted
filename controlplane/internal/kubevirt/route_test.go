package kubevirt_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/kubevirt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var routeGVK = schema.GroupVersionKind{Group: "route.openshift.io", Version: "v1", Kind: "Route"}

var _ = Describe("EnsureExternalRoutes", func() {
	var (
		ctx        context.Context
		scheme     *runtime.Scheme
		fakeClient client.Client
		oacp       *controlplanev1alpha3.OpenshiftAssistedControlPlane
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		utilruntime.Must(controlplanev1alpha3.AddToScheme(scheme))

		routeSchemeBuilder := runtime.NewSchemeBuilder(func(s *runtime.Scheme) error {
			s.AddKnownTypeWithName(routeGVK, &unstructured.Unstructured{})
			s.AddKnownTypeWithName(
				schema.GroupVersionKind{Group: "route.openshift.io", Version: "v1", Kind: "RouteList"},
				&unstructured.UnstructuredList{},
			)
			return nil
		})
		utilruntime.Must(routeSchemeBuilder.AddToScheme(scheme))

		oacp = &controlplanev1alpha3.OpenshiftAssistedControlPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test-ns",
			},
			Spec: controlplanev1alpha3.OpenshiftAssistedControlPlaneSpec{
				Config: controlplanev1alpha3.OpenshiftAssistedControlPlaneConfigSpec{
					BaseDomain: "apps.mgmt.example.com",
				},
			},
		}
	})

	It("should create API and ingress passthrough routes", func() {
		fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()

		err := kubevirt.EnsureExternalRoutes(ctx, fakeClient, oacp, "test-cluster", "test-ns")
		Expect(err).NotTo(HaveOccurred())

		apiRoute := &unstructured.Unstructured{}
		apiRoute.SetGroupVersionKind(routeGVK)
		err = fakeClient.Get(ctx, client.ObjectKey{Name: "test-cluster-api-route", Namespace: "test-ns"}, apiRoute)
		Expect(err).NotTo(HaveOccurred())

		host, _, _ := unstructured.NestedString(apiRoute.Object, "spec", "host")
		Expect(host).To(Equal("api.test-cluster.apps.mgmt.example.com"))

		tls, _, _ := unstructured.NestedString(apiRoute.Object, "spec", "tls", "termination")
		Expect(tls).To(Equal("passthrough"))

		ingressRoute := &unstructured.Unstructured{}
		ingressRoute.SetGroupVersionKind(routeGVK)
		err = fakeClient.Get(ctx, client.ObjectKey{Name: "test-cluster-ingress-route", Namespace: "test-ns"}, ingressRoute)
		Expect(err).NotTo(HaveOccurred())

		ingressHost, _, _ := unstructured.NestedString(ingressRoute.Object, "spec", "host")
		Expect(ingressHost).To(Equal("wildcard.apps.test-cluster.apps.mgmt.example.com"))

		wildcardPolicy, _, _ := unstructured.NestedString(ingressRoute.Object, "spec", "wildcardPolicy")
		Expect(wildcardPolicy).To(Equal("Subdomain"))
	})

	It("should target the correct services", func() {
		fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()

		err := kubevirt.EnsureExternalRoutes(ctx, fakeClient, oacp, "test-cluster", "test-ns")
		Expect(err).NotTo(HaveOccurred())

		apiRoute := &unstructured.Unstructured{}
		apiRoute.SetGroupVersionKind(routeGVK)
		_ = fakeClient.Get(ctx, client.ObjectKey{Name: "test-cluster-api-route", Namespace: "test-ns"}, apiRoute)

		svcName, _, _ := unstructured.NestedString(apiRoute.Object, "spec", "to", "name")
		Expect(svcName).To(Equal("test-cluster-api"))

		ingressRoute := &unstructured.Unstructured{}
		ingressRoute.SetGroupVersionKind(routeGVK)
		_ = fakeClient.Get(ctx, client.ObjectKey{Name: "test-cluster-ingress-route", Namespace: "test-ns"}, ingressRoute)

		ingressSvcName, _, _ := unstructured.NestedString(ingressRoute.Object, "spec", "to", "name")
		Expect(ingressSvcName).To(Equal("test-cluster-ingress"))
	})

	It("should be idempotent on re-creation", func() {
		fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()

		err := kubevirt.EnsureExternalRoutes(ctx, fakeClient, oacp, "test-cluster", "test-ns")
		Expect(err).NotTo(HaveOccurred())

		err = kubevirt.EnsureExternalRoutes(ctx, fakeClient, oacp, "test-cluster", "test-ns")
		Expect(err).NotTo(HaveOccurred())
	})
})
