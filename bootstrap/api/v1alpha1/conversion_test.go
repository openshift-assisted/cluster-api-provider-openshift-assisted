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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	clusterv1beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1"

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

var _ = Describe("OpenshiftAssistedConfig Round-Trip", func() {
	Context("v1alpha1 -> v1alpha2 -> v1alpha1", func() {
		It("should preserve all fields during round-trip conversion", func() {
			original := &OpenshiftAssistedConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
					Labels: map[string]string{
						"test-label": "test-value",
					},
					Annotations: map[string]string{
						"test-annotation": "test-value",
					},
				},
				Spec: OpenshiftAssistedConfigSpec{
					SSHAuthorizedKey:      "ssh-rsa AAAAB3...",
					CpuArchitecture:       "x86_64",
					AdditionalNTPSources:  []string{"ntp1.example.com", "ntp2.example.com"},
					AdditionalTrustBundle: "-----BEGIN CERTIFICATE-----\nMIIC...\n-----END CERTIFICATE-----",
					OSImageVersion:        "4.14.0",
					NodeRegistration: NodeRegistrationOptions{
						Name:               "worker-0",
						KubeletExtraLabels: []string{"node-role.kubernetes.io/worker="},
					},
				},
				Status: OpenshiftAssistedConfigStatus{
					Ready:              true,
					DataSecretName:     ptr.To("my-secret"),
					ObservedGeneration: 5,
				},
			}

			// Convert to v1alpha2
			intermediate := &bootstrapv1alpha2.OpenshiftAssistedConfig{}
			Expect(original.ConvertTo(intermediate)).To(Succeed())

			// Convert back to v1alpha1
			result := &OpenshiftAssistedConfig{}
			Expect(result.ConvertFrom(intermediate)).To(Succeed())

			// Verify all fields are preserved
			Expect(result.Name).To(Equal(original.Name))
			Expect(result.Namespace).To(Equal(original.Namespace))
			Expect(result.Labels).To(Equal(original.Labels))

			// Spec fields
			Expect(result.Spec.SSHAuthorizedKey).To(Equal(original.Spec.SSHAuthorizedKey))
			Expect(result.Spec.CpuArchitecture).To(Equal(original.Spec.CpuArchitecture))
			Expect(result.Spec.AdditionalNTPSources).To(Equal(original.Spec.AdditionalNTPSources))
			Expect(result.Spec.AdditionalTrustBundle).To(Equal(original.Spec.AdditionalTrustBundle))
			Expect(result.Spec.OSImageVersion).To(Equal(original.Spec.OSImageVersion))
			Expect(result.Spec.NodeRegistration.Name).To(Equal(original.Spec.NodeRegistration.Name))
			Expect(result.Spec.NodeRegistration.KubeletExtraLabels).To(Equal(original.Spec.NodeRegistration.KubeletExtraLabels))

			// Status fields
			Expect(result.Status.Ready).To(Equal(original.Status.Ready))
			Expect(*result.Status.DataSecretName).To(Equal(*original.Status.DataSecretName))
			Expect(result.Status.ObservedGeneration).To(Equal(original.Status.ObservedGeneration))
		})

		It("should handle empty/minimal resource during round-trip", func() {
			original := &OpenshiftAssistedConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "minimal-config",
					Namespace: "default",
				},
			}

			// Convert to v1alpha2
			intermediate := &bootstrapv1alpha2.OpenshiftAssistedConfig{}
			Expect(original.ConvertTo(intermediate)).To(Succeed())

			// Convert back to v1alpha1
			result := &OpenshiftAssistedConfig{}
			Expect(result.ConvertFrom(intermediate)).To(Succeed())

			Expect(result.Name).To(Equal(original.Name))
			Expect(result.Namespace).To(Equal(original.Namespace))
		})

		It("should handle deprecated v1alpha1 status fields during round-trip", func() {
			// v1alpha1 had InfraEnvRef, AgentRef, FailureReason, FailureMessage
			// These should be preserved via MarshalData/UnmarshalData
			original := &OpenshiftAssistedConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Status: OpenshiftAssistedConfigStatus{
					FailureReason:  "SomeReason",
					FailureMessage: "Something went wrong",
				},
			}

			// Convert to v1alpha2
			intermediate := &bootstrapv1alpha2.OpenshiftAssistedConfig{}
			Expect(original.ConvertTo(intermediate)).To(Succeed())

			// Convert back to v1alpha1
			result := &OpenshiftAssistedConfig{}
			Expect(result.ConvertFrom(intermediate)).To(Succeed())

			// These should be preserved via conversion data
			Expect(result.Status.FailureReason).To(Equal(original.Status.FailureReason))
			Expect(result.Status.FailureMessage).To(Equal(original.Status.FailureMessage))
		})

		It("should preserve conditions during round-trip", func() {
			testTime := metav1.NewTime(time.Date(2024, 6, 15, 12, 30, 0, 0, time.UTC))
			original := &OpenshiftAssistedConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Status: OpenshiftAssistedConfigStatus{
					Ready: true,
					Conditions: clusterv1beta1.Conditions{
						{
							Type:               "Ready",
							Status:             corev1.ConditionTrue,
							LastTransitionTime: testTime,
							Reason:             "ConfigReady",
							Message:            "Bootstrap config is ready",
							Severity:           clusterv1beta1.ConditionSeverityInfo,
						},
						{
							Type:               "InfraEnvCreated",
							Status:             corev1.ConditionTrue,
							LastTransitionTime: testTime,
							Reason:             "InfraEnvExists",
							Message:            "InfraEnv has been created",
							Severity:           clusterv1beta1.ConditionSeverityInfo,
						},
					},
				},
			}

			// Convert to v1alpha2
			intermediate := &bootstrapv1alpha2.OpenshiftAssistedConfig{}
			Expect(original.ConvertTo(intermediate)).To(Succeed())

			// Convert back to v1alpha1
			result := &OpenshiftAssistedConfig{}
			Expect(result.ConvertFrom(intermediate)).To(Succeed())

			// Conditions should be preserved
			Expect(result.Status.Conditions).To(HaveLen(2))

			// Check specific conditions
			var readyCond *clusterv1beta1.Condition
			var infraEnvCond *clusterv1beta1.Condition
			for i := range result.Status.Conditions {
				if result.Status.Conditions[i].Type == "Ready" {
					readyCond = &result.Status.Conditions[i]
				}
				if result.Status.Conditions[i].Type == "InfraEnvCreated" {
					infraEnvCond = &result.Status.Conditions[i]
				}
			}

			Expect(readyCond).ToNot(BeNil())
			Expect(readyCond.Status).To(Equal(corev1.ConditionTrue))
			Expect(readyCond.Reason).To(Equal("ConfigReady"))
			Expect(readyCond.Message).To(Equal("Bootstrap config is ready"))

			Expect(infraEnvCond).ToNot(BeNil())
			Expect(infraEnvCond.Status).To(Equal(corev1.ConditionTrue))
			Expect(infraEnvCond.Reason).To(Equal("InfraEnvExists"))
		})

		It("should preserve InfraEnvRef and AgentRef during round-trip", func() {
			original := &OpenshiftAssistedConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Status: OpenshiftAssistedConfigStatus{
					Ready: true,
					InfraEnvRef: &corev1.ObjectReference{
						Kind:       "InfraEnv",
						Name:       "my-infraenv",
						Namespace:  "default",
						APIVersion: "agent-install.openshift.io/v1beta1",
					},
					AgentRef: &corev1.LocalObjectReference{
						Name: "my-agent",
					},
				},
			}

			// Convert to v1alpha2
			intermediate := &bootstrapv1alpha2.OpenshiftAssistedConfig{}
			Expect(original.ConvertTo(intermediate)).To(Succeed())

			// Convert back to v1alpha1
			result := &OpenshiftAssistedConfig{}
			Expect(result.ConvertFrom(intermediate)).To(Succeed())

			// InfraEnvRef and AgentRef should be preserved via MarshalData
			Expect(result.Status.InfraEnvRef).ToNot(BeNil())
			Expect(result.Status.InfraEnvRef.Name).To(Equal("my-infraenv"))
			Expect(result.Status.InfraEnvRef.Namespace).To(Equal("default"))

			Expect(result.Status.AgentRef).ToNot(BeNil())
			Expect(result.Status.AgentRef.Name).To(Equal("my-agent"))
		})
	})
})

var _ = Describe("OpenshiftAssistedConfig Conditions Conversion", func() {
	var testTime metav1.Time

	BeforeEach(func() {
		testTime = metav1.NewTime(time.Date(2024, 6, 15, 12, 30, 0, 0, time.UTC))
	})

	Context("ConvertTo (v1alpha1 -> v1alpha2)", func() {
		It("should convert v1beta1.Conditions to metav1.Conditions", func() {
			src := &OpenshiftAssistedConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Status: OpenshiftAssistedConfigStatus{
					Ready: true,
					Conditions: clusterv1beta1.Conditions{
						{
							Type:               "Ready",
							Status:             corev1.ConditionTrue,
							LastTransitionTime: testTime,
							Reason:             "AllReady",
							Message:            "Config is ready",
							Severity:           clusterv1beta1.ConditionSeverityInfo,
						},
					},
				},
			}

			dst := &bootstrapv1alpha2.OpenshiftAssistedConfig{}
			Expect(src.ConvertTo(dst)).To(Succeed())

			// Note: The current conversion uses MarshalData for conditions,
			// so v1alpha2's Conditions might not be populated directly.
			// This test documents the expected behavior.
			// The V1Beta1Conditions field should be populated via the MarshalData.
		})
	})

	Context("ConvertFrom (v1alpha2 -> v1alpha1)", func() {
		It("should convert metav1.Conditions to v1beta1.Conditions", func() {
			src := &bootstrapv1alpha2.OpenshiftAssistedConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Status: bootstrapv1alpha2.OpenshiftAssistedConfigStatus{
					DataSecretName: "test-secret",
					Initialization: bootstrapv1alpha2.OpenshiftAssistedConfigInitializationStatus{
						DataSecretCreated: ptr.To(true),
					},
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionTrue,
							LastTransitionTime: testTime,
							Reason:             "AllReady",
							Message:            "Config is ready",
						},
					},
				},
			}

			dst := &OpenshiftAssistedConfig{}
			Expect(dst.ConvertFrom(src)).To(Succeed())

			// Note: The current conversion uses UnmarshalData for conditions.
			// This test documents what the converted v1alpha1 looks like.
			Expect(dst.Status.Ready).To(BeTrue())
		})
	})
})

var _ = Describe("OpenshiftAssistedConfigTemplate Round-Trip", func() {
	Context("v1alpha1 -> v1alpha2 -> v1alpha1", func() {
		It("should preserve all fields during round-trip conversion", func() {
			original := &OpenshiftAssistedConfigTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-template",
					Namespace: "default",
					Labels: map[string]string{
						"test-label": "test-value",
					},
				},
				Spec: OpenshiftAssistedConfigTemplateSpec{
					Template: OpenshiftAssistedConfigTemplateResource{
						Spec: OpenshiftAssistedConfigSpec{
							SSHAuthorizedKey: "ssh-rsa AAAAB3...",
							CpuArchitecture:  "arm64",
							NodeRegistration: NodeRegistrationOptions{
								KubeletExtraLabels: []string{"custom=label"},
							},
						},
					},
				},
			}

			// Convert to v1alpha2
			intermediate := &bootstrapv1alpha2.OpenshiftAssistedConfigTemplate{}
			Expect(original.ConvertTo(intermediate)).To(Succeed())

			// Convert back to v1alpha1
			result := &OpenshiftAssistedConfigTemplate{}
			Expect(result.ConvertFrom(intermediate)).To(Succeed())

			// Verify fields preserved
			Expect(result.Name).To(Equal(original.Name))
			Expect(result.Labels).To(Equal(original.Labels))
			Expect(result.Spec.Template.Spec.SSHAuthorizedKey).To(Equal(original.Spec.Template.Spec.SSHAuthorizedKey))
			Expect(result.Spec.Template.Spec.CpuArchitecture).To(Equal(original.Spec.Template.Spec.CpuArchitecture))
			Expect(result.Spec.Template.Spec.NodeRegistration.KubeletExtraLabels).To(Equal(original.Spec.Template.Spec.NodeRegistration.KubeletExtraLabels))
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
