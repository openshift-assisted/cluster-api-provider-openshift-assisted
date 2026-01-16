/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	bootstrapv1alpha2 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/bootstrap/api/v1alpha2"
)

var _ = Describe("OpenshiftAssistedConfig", func() {
	Context("ConvertTo", func() {
		It("should convert v1alpha1 to v1alpha2 with basic fields", func() {
			src := &OpenshiftAssistedConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: OpenshiftAssistedConfigSpec{
					SSHAuthorizedKey: "ssh-rsa test-key",
					CpuArchitecture:  "x86_64",
				},
				Status: OpenshiftAssistedConfigStatus{
					Ready:              true,
					DataSecretName:     ptr.To("test-secret"),
					ObservedGeneration: 1,
				},
			}

			dst := &bootstrapv1alpha2.OpenshiftAssistedConfig{}
			Expect(src.ConvertTo(dst)).To(Succeed())

			// Verify ObjectMeta
			Expect(dst.Name).To(Equal("test-config"))
			Expect(dst.Namespace).To(Equal("default"))

			// Verify Spec
			Expect(dst.Spec.SSHAuthorizedKey).To(Equal("ssh-rsa test-key"))
			Expect(dst.Spec.CpuArchitecture).To(Equal("x86_64"))

			// Verify Status
			Expect(dst.Status.DataSecretName).To(Equal("test-secret"))
			Expect(dst.Status.Initialization.DataSecretCreated).To(Equal(ptr.To(true)))
			Expect(dst.Status.ObservedGeneration).To(Equal(int64(1)))
		})

		It("should store InfraEnvRef and AgentRef as migration annotations", func() {
			// v1alpha1 has InfraEnvRef and AgentRef in status
			// These are stored as annotations for the controller to migrate to managed-by labels
			src := &OpenshiftAssistedConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Status: OpenshiftAssistedConfigStatus{
					InfraEnvRef:    &corev1.ObjectReference{Name: "test-infraenv", Namespace: "infra-ns"},
					AgentRef:       &corev1.LocalObjectReference{Name: "test-agent"},
					FailureReason:  "TestReason",
					FailureMessage: "TestMessage",
				},
			}

			dst := &bootstrapv1alpha2.OpenshiftAssistedConfig{}
			Expect(src.ConvertTo(dst)).To(Succeed())

			// Verify migration annotations are set
			Expect(dst.Annotations).To(HaveKeyWithValue(
				bootstrapv1alpha2.MigrateInfraEnvRefAnnotation, "test-infraenv"))
			Expect(dst.Annotations).To(HaveKeyWithValue(
				bootstrapv1alpha2.MigrateInfraEnvRefNamespaceAnnotation, "infra-ns"))
			Expect(dst.Annotations).To(HaveKeyWithValue(
				bootstrapv1alpha2.MigrateAgentRefAnnotation, "test-agent"))
		})

		It("should not set migration annotations when refs are nil", func() {
			src := &OpenshiftAssistedConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Status: OpenshiftAssistedConfigStatus{
					InfraEnvRef: nil,
					AgentRef:    nil,
				},
			}

			dst := &bootstrapv1alpha2.OpenshiftAssistedConfig{}
			Expect(src.ConvertTo(dst)).To(Succeed())

			// Migration annotations should not be set when refs are nil
			Expect(dst.Annotations).NotTo(HaveKey(bootstrapv1alpha2.MigrateInfraEnvRefAnnotation))
			Expect(dst.Annotations).NotTo(HaveKey(bootstrapv1alpha2.MigrateAgentRefAnnotation))
		})

		It("should preserve existing annotations when adding migration annotations", func() {
			src := &OpenshiftAssistedConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
					Annotations: map[string]string{
						"existing-annotation": "existing-value",
					},
				},
				Status: OpenshiftAssistedConfigStatus{
					InfraEnvRef: &corev1.ObjectReference{Name: "test-infraenv"},
				},
			}

			dst := &bootstrapv1alpha2.OpenshiftAssistedConfig{}
			Expect(src.ConvertTo(dst)).To(Succeed())

			// Both existing and migration annotations should be present
			Expect(dst.Annotations).To(HaveKeyWithValue("existing-annotation", "existing-value"))
			Expect(dst.Annotations).To(HaveKeyWithValue(
				bootstrapv1alpha2.MigrateInfraEnvRefAnnotation, "test-infraenv"))
		})

		It("should handle nil DataSecretName", func() {
			src := &OpenshiftAssistedConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Status: OpenshiftAssistedConfigStatus{
					Ready:          false,
					DataSecretName: nil,
				},
			}

			dst := &bootstrapv1alpha2.OpenshiftAssistedConfig{}
			Expect(src.ConvertTo(dst)).To(Succeed())

			Expect(dst.Status.DataSecretName).To(Equal(""))
			Expect(dst.Status.Initialization.DataSecretCreated).To(BeNil())
		})
	})

	Context("ConvertFrom", func() {
		It("should convert v1alpha2 to v1alpha1 with basic fields", func() {
			src := &bootstrapv1alpha2.OpenshiftAssistedConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: bootstrapv1alpha2.OpenshiftAssistedConfigSpec{
					SSHAuthorizedKey: "ssh-rsa test-key",
					CpuArchitecture:  "x86_64",
				},
				Status: bootstrapv1alpha2.OpenshiftAssistedConfigStatus{
					DataSecretName: "test-secret",
					Initialization: bootstrapv1alpha2.OpenshiftAssistedConfigInitializationStatus{
						DataSecretCreated: ptr.To(true),
					},
					ObservedGeneration: 1,
				},
			}

			dst := &OpenshiftAssistedConfig{}
			Expect(dst.ConvertFrom(src)).To(Succeed())

			// Verify ObjectMeta
			Expect(dst.Name).To(Equal("test-config"))
			Expect(dst.Namespace).To(Equal("default"))

			// Verify Spec
			Expect(dst.Spec.SSHAuthorizedKey).To(Equal("ssh-rsa test-key"))
			Expect(dst.Spec.CpuArchitecture).To(Equal("x86_64"))

			// Verify Status
			Expect(*dst.Status.DataSecretName).To(Equal("test-secret"))
			Expect(dst.Status.Ready).To(BeTrue())
			Expect(dst.Status.ObservedGeneration).To(Equal(int64(1)))

			// v1alpha1 deprecated fields should be nil/empty when converting from v1alpha2
			Expect(dst.Status.InfraEnvRef).To(BeNil())
			Expect(dst.Status.AgentRef).To(BeNil())
			Expect(dst.Status.FailureReason).To(Equal(""))
			Expect(dst.Status.FailureMessage).To(Equal(""))
		})

		It("should handle empty DataSecretName", func() {
			src := &bootstrapv1alpha2.OpenshiftAssistedConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Status: bootstrapv1alpha2.OpenshiftAssistedConfigStatus{
					DataSecretName: "",
					Initialization: bootstrapv1alpha2.OpenshiftAssistedConfigInitializationStatus{
						DataSecretCreated: ptr.To(false),
					},
				},
			}

			dst := &OpenshiftAssistedConfig{}
			Expect(dst.ConvertFrom(src)).To(Succeed())

			Expect(dst.Status.DataSecretName).To(BeNil())
			Expect(dst.Status.Ready).To(BeFalse())
		})
	})
})

var _ = Describe("OpenshiftAssistedConfigTemplate", func() {
	Context("ConvertTo", func() {
		It("should convert v1alpha1 template to v1alpha2", func() {
			src := &OpenshiftAssistedConfigTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-template",
					Namespace: "default",
				},
				Spec: OpenshiftAssistedConfigTemplateSpec{
					Template: OpenshiftAssistedConfigTemplateResource{
						Spec: OpenshiftAssistedConfigSpec{
							SSHAuthorizedKey: "ssh-rsa test-key",
							CpuArchitecture:  "x86_64",
						},
					},
				},
			}

			dst := &bootstrapv1alpha2.OpenshiftAssistedConfigTemplate{}
			Expect(src.ConvertTo(dst)).To(Succeed())

			Expect(dst.Name).To(Equal("test-template"))
			Expect(dst.Spec.Template.Spec.SSHAuthorizedKey).To(Equal("ssh-rsa test-key"))
			Expect(dst.Spec.Template.Spec.CpuArchitecture).To(Equal("x86_64"))
		})
	})

	Context("ConvertFrom", func() {
		It("should convert v1alpha2 template to v1alpha1", func() {
			src := &bootstrapv1alpha2.OpenshiftAssistedConfigTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-template",
					Namespace: "default",
				},
				Spec: bootstrapv1alpha2.OpenshiftAssistedConfigTemplateSpec{
					Template: bootstrapv1alpha2.OpenshiftAssistedConfigTemplateResource{
						Spec: bootstrapv1alpha2.OpenshiftAssistedConfigSpec{
							SSHAuthorizedKey: "ssh-rsa test-key",
							CpuArchitecture:  "x86_64",
						},
					},
				},
			}

			dst := &OpenshiftAssistedConfigTemplate{}
			Expect(dst.ConvertFrom(src)).To(Succeed())

			Expect(dst.Name).To(Equal("test-template"))
			Expect(dst.Spec.Template.Spec.SSHAuthorizedKey).To(Equal("ssh-rsa test-key"))
			Expect(dst.Spec.Template.Spec.CpuArchitecture).To(Equal("x86_64"))
		})
	})
})
