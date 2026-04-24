package v1alpha2

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

var _ = Describe("OpenshiftAssistedConfig Webhook", func() {
	var (
		webhook *OpenshiftAssistedConfig
		ctx     context.Context
	)

	BeforeEach(func() {
		webhook = &OpenshiftAssistedConfig{}
		ctx = context.TODO()
	})

	Describe("ValidateCreate", func() {
		It("should return no error for valid object", func() {
			obj := &OpenshiftAssistedConfig{}
			warnings, err := webhook.ValidateCreate(ctx, obj)
			Expect(warnings).To(BeNil())
			Expect(err).To(BeNil())
		})

		It("should accept config with Name", func() {
			obj := &OpenshiftAssistedConfig{
				Spec: OpenshiftAssistedConfigSpec{
					NodeRegistration: NodeRegistrationOptions{
						Name: "METADATA_HOSTNAME",
					},
				},
			}
			warnings, err := webhook.ValidateCreate(ctx, obj)
			Expect(warnings).To(BeNil())
			Expect(err).To(BeNil())
		})
	})

	Describe("ValidateUpdate", func() {
		It("should return no error if specs are equal", func() {
			oldObj := &OpenshiftAssistedConfig{Spec: OpenshiftAssistedConfigSpec{CpuArchitecture: "x86_64"}}
			newObj := &OpenshiftAssistedConfig{Spec: OpenshiftAssistedConfigSpec{CpuArchitecture: "x86_64"}}
			warnings, err := webhook.ValidateUpdate(ctx, oldObj, newObj)
			Expect(warnings).To(BeNil())
			Expect(err).To(BeNil())
		})

		It("should return an error if specs are different", func() {
			oldObj := &OpenshiftAssistedConfig{Spec: OpenshiftAssistedConfigSpec{CpuArchitecture: "x86_64"}}
			newObj := &OpenshiftAssistedConfig{Spec: OpenshiftAssistedConfigSpec{CpuArchitecture: "arm64"}}
			warnings, err := webhook.ValidateUpdate(ctx, oldObj, newObj)
			Expect(warnings).To(BeNil())
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsBadRequest(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("spec is immutable"))
		})

	})

	Describe("ValidateDelete", func() {
		It("should return no error for valid object", func() {
			obj := &OpenshiftAssistedConfig{}
			warnings, err := webhook.ValidateDelete(ctx, obj)
			Expect(warnings).To(BeNil())
			Expect(err).To(BeNil())
		})
	})
})
