package kubevirt_test

import (
	"context"
	"os"

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

var _ = Describe("EnsureMCSProxy", func() {
	var (
		ctx        context.Context
		scheme     *runtime.Scheme
		fakeClient client.Client
		oacp       *controlplanev1alpha3.OpenshiftAssistedControlPlane
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		utilruntime.Must(appsv1.AddToScheme(scheme))
		utilruntime.Must(corev1.AddToScheme(scheme))
		utilruntime.Must(controlplanev1alpha3.AddToScheme(scheme))

		oacp = &controlplanev1alpha3.OpenshiftAssistedControlPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test-ns",
				UID:       "test-uid",
			},
		}
	})

	It("should create a proxy deployment and service when called with same namespace", func() {
		fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp).Build()

		url, err := kubevirt.EnsureMCSProxy(ctx, fakeClient, oacp, "test-cluster", "test-ns", "")
		Expect(err).NotTo(HaveOccurred())
		Expect(url).To(ContainSubstring("test-cluster-mcs-proxy"))
		Expect(url).To(ContainSubstring("test-ns"))

		deploy := &appsv1.Deployment{}
		err = fakeClient.Get(ctx, client.ObjectKey{Name: "test-cluster-mcs-proxy", Namespace: "test-ns"}, deploy)
		Expect(err).NotTo(HaveOccurred())
		Expect(deploy.Spec.Template.Spec.Containers).To(HaveLen(1))
		Expect(deploy.Spec.Template.Spec.Containers[0].Name).To(Equal("socat"))

		svc := &corev1.Service{}
		err = fakeClient.Get(ctx, client.ObjectKey{Name: "test-cluster-mcs-proxy", Namespace: "test-ns"}, svc)
		Expect(err).NotTo(HaveOccurred())
		Expect(svc.Spec.Ports).To(HaveLen(1))
		Expect(svc.Spec.Ports[0].Port).To(Equal(int32(kubevirt.MCSProxyPort)))
	})

	It("should set owner reference when deployed in the same namespace as OACP", func() {
		fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp).Build()

		_, err := kubevirt.EnsureMCSProxy(ctx, fakeClient, oacp, "test-cluster", "test-ns", "")
		Expect(err).NotTo(HaveOccurred())

		deploy := &appsv1.Deployment{}
		err = fakeClient.Get(ctx, client.ObjectKey{Name: "test-cluster-mcs-proxy", Namespace: "test-ns"}, deploy)
		Expect(err).NotTo(HaveOccurred())
		Expect(deploy.OwnerReferences).To(HaveLen(1))
		Expect(deploy.OwnerReferences[0].Name).To(Equal("test-cluster"))
	})

	It("should not set owner reference when deployed in a different namespace", func() {
		fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp).Build()

		_, err := kubevirt.EnsureMCSProxy(ctx, fakeClient, oacp, "test-cluster", "infra-ns", "")
		Expect(err).NotTo(HaveOccurred())

		deploy := &appsv1.Deployment{}
		err = fakeClient.Get(ctx, client.ObjectKey{Name: "test-cluster-mcs-proxy", Namespace: "infra-ns"}, deploy)
		Expect(err).NotTo(HaveOccurred())
		Expect(deploy.OwnerReferences).To(BeEmpty())
	})

	It("should use the provided proxy image", func() {
		fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp).Build()

		_, err := kubevirt.EnsureMCSProxy(ctx, fakeClient, oacp, "test-cluster", "test-ns", "custom-image:v1")
		Expect(err).NotTo(HaveOccurred())

		deploy := &appsv1.Deployment{}
		err = fakeClient.Get(ctx, client.ObjectKey{Name: "test-cluster-mcs-proxy", Namespace: "test-ns"}, deploy)
		Expect(err).NotTo(HaveOccurred())
		Expect(deploy.Spec.Template.Spec.Containers[0].Image).To(Equal("custom-image:v1"))
	})

	It("should forward to the API service on the MCS NodePort", func() {
		fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp).Build()

		_, err := kubevirt.EnsureMCSProxy(ctx, fakeClient, oacp, "test-cluster", "test-ns", "")
		Expect(err).NotTo(HaveOccurred())

		deploy := &appsv1.Deployment{}
		err = fakeClient.Get(ctx, client.ObjectKey{Name: "test-cluster-mcs-proxy", Namespace: "test-ns"}, deploy)
		Expect(err).NotTo(HaveOccurred())

		args := deploy.Spec.Template.Spec.Containers[0].Args
		Expect(args).To(HaveLen(2))
		Expect(args[0]).To(ContainSubstring("TCP-LISTEN:8624"))
		Expect(args[1]).To(ContainSubstring("test-cluster-api.test-ns.svc.cluster.local:30624"))
	})

	It("should be idempotent on re-creation", func() {
		fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp).Build()

		url1, err := kubevirt.EnsureMCSProxy(ctx, fakeClient, oacp, "test-cluster", "test-ns", "")
		Expect(err).NotTo(HaveOccurred())

		url2, err := kubevirt.EnsureMCSProxy(ctx, fakeClient, oacp, "test-cluster", "test-ns", "")
		Expect(err).NotTo(HaveOccurred())
		Expect(url2).To(Equal(url1))
	})
})

var _ = Describe("GetMCSProxyImage", func() {
	It("should return the resolved image when provided", func() {
		Expect(kubevirt.GetMCSProxyImage("my-image:v1")).To(Equal("my-image:v1"))
	})

	It("should return the env var image when set and no resolved image", func() {
		os.Setenv(kubevirt.RelatedImageMCSProxyEnv, "env-image:v2")
		defer os.Unsetenv(kubevirt.RelatedImageMCSProxyEnv)
		Expect(kubevirt.GetMCSProxyImage("")).To(Equal("env-image:v2"))
	})

	It("should return the default image when no resolved image or env var", func() {
		os.Unsetenv(kubevirt.RelatedImageMCSProxyEnv)
		Expect(kubevirt.GetMCSProxyImage("")).To(Equal(kubevirt.DefaultMCSProxyImage))
	})

	It("should prefer resolved image over env var", func() {
		os.Setenv(kubevirt.RelatedImageMCSProxyEnv, "env-image:v2")
		defer os.Unsetenv(kubevirt.RelatedImageMCSProxyEnv)
		Expect(kubevirt.GetMCSProxyImage("resolved:v3")).To(Equal("resolved:v3"))
	})
})

var _ = Describe("GenerateMCSNodePortManifests", func() {
	It("should produce a single NodePort service manifest", func() {
		manifests := kubevirt.GenerateMCSNodePortManifests()
		Expect(manifests).To(HaveLen(1))
		Expect(manifests[0].Filename).To(Equal("01-mcs-nodeport-service.yaml"))
		Expect(manifests[0].Content).To(ContainSubstring("type: NodePort"))
		Expect(manifests[0].Content).To(ContainSubstring("machine-config-server"))
		Expect(manifests[0].Content).To(ContainSubstring("30624"))
	})

	It("should target the machine-config-server selector", func() {
		manifests := kubevirt.GenerateMCSNodePortManifests()
		Expect(manifests[0].Content).To(ContainSubstring("k8s-app: machine-config-server"))
	})

	It("should use the openshift-machine-config-operator namespace", func() {
		manifests := kubevirt.GenerateMCSNodePortManifests()
		Expect(manifests[0].Content).To(ContainSubstring("namespace: openshift-machine-config-operator"))
	})
})
