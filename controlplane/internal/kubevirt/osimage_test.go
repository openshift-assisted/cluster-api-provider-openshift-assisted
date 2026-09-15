package kubevirt_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/kubevirt"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("EnsureOSImageInAgentServiceConfig", func() {
	var (
		ctx        context.Context
		scheme     *runtime.Scheme
		fakeClient client.Client
		asc        *aiv1beta1.AgentServiceConfig
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		utilruntime.Must(aiv1beta1.AddToScheme(scheme))

		asc = &aiv1beta1.AgentServiceConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name: "agent",
			},
			Spec: aiv1beta1.AgentServiceConfigSpec{
				OSImages: []aiv1beta1.OSImage{},
			},
		}
	})

	Context("When the OS image does not exist", func() {
		It("should add it to AgentServiceConfig", func() {
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(asc).Build()

			err := kubevirt.EnsureOSImageInAgentServiceConfig(ctx, fakeClient, "4.22.2", "x86_64")
			Expect(err).NotTo(HaveOccurred())

			updated := &aiv1beta1.AgentServiceConfig{}
			err = fakeClient.Get(ctx, client.ObjectKey{Name: "agent"}, updated)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Spec.OSImages).To(HaveLen(1))
			Expect(updated.Spec.OSImages[0].OpenshiftVersion).To(Equal("4.22"))
			Expect(updated.Spec.OSImages[0].CPUArchitecture).To(Equal("x86_64"))
			Expect(updated.Spec.OSImages[0].Url).To(ContainSubstring("4.22"))
		})
	})

	Context("When the OS image already exists", func() {
		It("should not add a duplicate", func() {
			asc.Spec.OSImages = []aiv1beta1.OSImage{
				{OpenshiftVersion: "4.22", Version: "4.22", CPUArchitecture: "x86_64"},
			}
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(asc).Build()

			err := kubevirt.EnsureOSImageInAgentServiceConfig(ctx, fakeClient, "4.22.8", "x86_64")
			Expect(err).NotTo(HaveOccurred())

			updated := &aiv1beta1.AgentServiceConfig{}
			err = fakeClient.Get(ctx, client.ObjectKey{Name: "agent"}, updated)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Spec.OSImages).To(HaveLen(1))
		})
	})

	Context("When the version is invalid", func() {
		It("should return an error", func() {
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(asc).Build()

			err := kubevirt.EnsureOSImageInAgentServiceConfig(ctx, fakeClient, "bad", "x86_64")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot extract major.minor"))
		})
	})

	Context("When AgentServiceConfig does not exist", func() {
		It("should return an error", func() {
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()

			err := kubevirt.EnsureOSImageInAgentServiceConfig(ctx, fakeClient, "4.22.2", "x86_64")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get AgentServiceConfig"))
		})
	})
})
