package kubevirt_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/kubevirt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("EnsureExternalAccessServices", func() {
	var (
		ctx        context.Context
		scheme     *runtime.Scheme
		fakeClient client.Client
		oacp       *controlplanev1alpha3.OpenshiftAssistedControlPlane
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		utilruntime.Must(corev1.AddToScheme(scheme))
		utilruntime.Must(controlplanev1alpha3.AddToScheme(scheme))

		oacp = &controlplanev1alpha3.OpenshiftAssistedControlPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test-ns",
				UID:       "test-uid",
			},
			Spec: controlplanev1alpha3.OpenshiftAssistedControlPlaneSpec{
				Config: controlplanev1alpha3.OpenshiftAssistedControlPlaneConfigSpec{
					BaseDomain: "apps.mgmt.example.com",
				},
			},
		}
	})

	It("should create API and Ingress services with correct ports", func() {
		fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp).Build()

		ips, err := kubevirt.EnsureExternalAccessServices(ctx, fakeClient, oacp, "test-cluster", "test-ns")
		Expect(err).NotTo(HaveOccurred())
		Expect(ips).NotTo(BeNil())

		apiSvc := &corev1.Service{}
		err = fakeClient.Get(ctx, client.ObjectKey{Name: "test-cluster-api", Namespace: "test-ns"}, apiSvc)
		Expect(err).NotTo(HaveOccurred())
		Expect(apiSvc.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
		Expect(apiSvc.Spec.Ports).To(HaveLen(4))

		portNames := make([]string, 0, len(apiSvc.Spec.Ports))
		for _, p := range apiSvc.Spec.Ports {
			portNames = append(portNames, p.Name)
		}
		Expect(portNames).To(ContainElements("api", "mcs", "api-route", "mcs-nodeport"))

		ingressSvc := &corev1.Service{}
		err = fakeClient.Get(ctx, client.ObjectKey{Name: "test-cluster-ingress", Namespace: "test-ns"}, ingressSvc)
		Expect(err).NotTo(HaveOccurred())
		Expect(ingressSvc.Spec.Ports).To(HaveLen(2))
	})

	It("should use CAPI cluster labels as selectors", func() {
		fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp).Build()

		_, err := kubevirt.EnsureExternalAccessServices(ctx, fakeClient, oacp, "test-cluster", "test-ns")
		Expect(err).NotTo(HaveOccurred())

		apiSvc := &corev1.Service{}
		err = fakeClient.Get(ctx, client.ObjectKey{Name: "test-cluster-api", Namespace: "test-ns"}, apiSvc)
		Expect(err).NotTo(HaveOccurred())
		Expect(apiSvc.Spec.Selector["cluster.x-k8s.io/cluster-name"]).To(Equal("test-cluster"))
		Expect(apiSvc.Spec.Selector["cluster.x-k8s.io/role"]).To(Equal("control-plane"))
	})

	It("should be idempotent on re-creation", func() {
		fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp).Build()

		_, err := kubevirt.EnsureExternalAccessServices(ctx, fakeClient, oacp, "test-cluster", "test-ns")
		Expect(err).NotTo(HaveOccurred())

		_, err = kubevirt.EnsureExternalAccessServices(ctx, fakeClient, oacp, "test-cluster", "test-ns")
		Expect(err).NotTo(HaveOccurred())
	})
})
