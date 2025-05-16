package v1alpha1

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
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

		It("should return an error for invalid object type", func() {
			obj := &runtime.Unknown{}
			warnings, err := webhook.ValidateCreate(ctx, obj)
			Expect(warnings).To(BeNil())
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsBadRequest(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("expected an OpenshiftAssistedConfig"))
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

		It("should return an error for invalid old object type", func() {
			oldObj := &runtime.Unknown{}
			newObj := &OpenshiftAssistedConfig{}
			warnings, err := webhook.ValidateUpdate(ctx, oldObj, newObj)
			Expect(warnings).To(BeNil())
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsBadRequest(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("expected a OpenshiftAssistedConfig"))
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
