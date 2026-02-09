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

package v1alpha2

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	clusterv1beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
)

var _ = Describe("OpenshiftAssistedControlPlane", func() {
	Context("ConvertTo", func() {
		It("should convert v1alpha2 to v1alpha3 with basic fields", func() {
			src := &OpenshiftAssistedControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "default",
				},
				Spec: OpenshiftAssistedControlPlaneSpec{
					Replicas:            3,
					DistributionVersion: "4.14.0",
					MachineTemplate: OpenshiftAssistedControlPlaneMachineTemplate{
						InfrastructureRef: corev1.ObjectReference{
							APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
							Kind:       "Metal3MachineTemplate",
							Name:       "test-infra",
							Namespace:  "default",
						},
						NodeDrainTimeout:        &metav1.Duration{Duration: 60000000000},  // 60s
						NodeVolumeDetachTimeout: &metav1.Duration{Duration: 120000000000}, // 120s
						NodeDeletionTimeout:     &metav1.Duration{Duration: 30000000000},  // 30s
					},
				},
				Status: OpenshiftAssistedControlPlaneStatus{
					Ready:       true,
					Initialized: true,
					Replicas:    3,
					Version:     ptr.To("1.27.0"),
				},
			}

			dst := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
			Expect(src.ConvertTo(dst)).To(Succeed())

			// Verify ObjectMeta
			Expect(dst.Name).To(Equal("test-cp"))
			Expect(dst.Namespace).To(Equal("default"))

			// Verify Spec
			Expect(dst.Spec.Replicas).To(Equal(int32(3)))
			Expect(dst.Spec.DistributionVersion).To(Equal("4.14.0"))

			// Verify MachineTemplate conversion (Duration -> seconds)
			Expect(dst.Spec.MachineTemplate.Deletion.NodeDrainTimeoutSeconds).To(Equal(ptr.To(int32(60))))
			Expect(dst.Spec.MachineTemplate.Deletion.NodeVolumeDetachTimeoutSeconds).To(Equal(ptr.To(int32(120))))
			Expect(dst.Spec.MachineTemplate.Deletion.NodeDeletionTimeoutSeconds).To(Equal(ptr.To(int32(30))))

			// Verify Status
			Expect(dst.Status.Initialization.ControlPlaneInitialized).To(Equal(ptr.To(true)))
			Expect(dst.Status.Replicas).To(Equal(ptr.To(int32(3))))
			Expect(dst.Status.Version).To(Equal("1.27.0"))
		})

		It("should convert v1alpha2 status fields to v1alpha3 status fields", func() {
			src := &OpenshiftAssistedControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "default",
				},
				Spec: OpenshiftAssistedControlPlaneSpec{
					Replicas:            3,
					DistributionVersion: "4.14.0",
					MachineTemplate: OpenshiftAssistedControlPlaneMachineTemplate{
						InfrastructureRef: corev1.ObjectReference{
							APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
							Kind:       "Metal3MachineTemplate",
							Name:       "test-infra",
						},
					},
				},
				Status: OpenshiftAssistedControlPlaneStatus{
					ClusterDeploymentRef: &corev1.ObjectReference{
						Name:      "test-cd",
						Namespace: "default",
					},
					Selector:            "cluster.x-k8s.io/cluster-name=test",
					Ready:               true,
					Initialized:         true,
					Replicas:            3,
					ReadyReplicas:       2,
					UpdatedReplicas:     3,
					UnavailableReplicas: 1,
					Conditions: clusterv1beta1.Conditions{
						{
							Type:   "Ready",
							Status: corev1.ConditionTrue,
						},
					},
				},
			}

			dst := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
			Expect(src.ConvertTo(dst)).To(Succeed())

			// Verify v1alpha2 status fields are converted to v1alpha3 status
			Expect(dst.Status.Selector).To(Equal("cluster.x-k8s.io/cluster-name=test"))
			Expect(dst.Status.Replicas).To(Equal(ptr.To(int32(3))))
			Expect(dst.Status.ReadyReplicas).To(Equal(ptr.To(int32(2))))
			Expect(dst.Status.UpToDateReplicas).To(Equal(ptr.To(int32(3))))
			Expect(dst.Status.Initialization.ControlPlaneInitialized).To(Equal(ptr.To(true)))
			Expect(dst.Status.Conditions).To(HaveLen(1))
		})
	})

	Context("ConvertFrom", func() {
		It("should convert v1alpha3 to v1alpha2 with basic fields", func() {
			src := &controlplanev1alpha3.OpenshiftAssistedControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "default",
				},
				Spec: controlplanev1alpha3.OpenshiftAssistedControlPlaneSpec{
					Replicas:            3,
					DistributionVersion: "4.14.0",
					MachineTemplate: controlplanev1alpha3.OpenshiftAssistedControlPlaneMachineTemplate{
						InfrastructureRef: clusterv1.ContractVersionedObjectReference{
							Kind:     "Metal3MachineTemplate",
							Name:     "test-infra",
							APIGroup: "infrastructure.cluster.x-k8s.io",
						},
						Deletion: clusterv1.MachineDeletionSpec{
							NodeDrainTimeoutSeconds:        ptr.To(int32(60)),  // 60s
							NodeVolumeDetachTimeoutSeconds: ptr.To(int32(120)), // 120s
							NodeDeletionTimeoutSeconds:     ptr.To(int32(30)),  // 30s
						},
					},
				},
				Status: controlplanev1alpha3.OpenshiftAssistedControlPlaneStatus{
					Initialization: controlplanev1alpha3.OpenshiftAssistedControlPlaneInitializationStatus{
						ControlPlaneInitialized: ptr.To(true),
					},
					Replicas:            ptr.To(int32(3)),
					ReadyReplicas:       ptr.To(int32(2)),
					UpToDateReplicas:    ptr.To(int32(3)),
					Version:             "1.27.0",
					DistributionVersion: "4.14.0",
				},
			}

			dst := &OpenshiftAssistedControlPlane{}
			Expect(dst.ConvertFrom(src)).To(Succeed())

			// Verify ObjectMeta
			Expect(dst.Name).To(Equal("test-cp"))
			Expect(dst.Namespace).To(Equal("default"))

			// Verify Spec
			Expect(dst.Spec.Replicas).To(Equal(int32(3)))
			Expect(dst.Spec.DistributionVersion).To(Equal("4.14.0"))

			// Verify MachineTemplate conversion
			Expect(dst.Spec.MachineTemplate.NodeDrainTimeout).To(Equal(&metav1.Duration{Duration: 60000000000}))
			Expect(dst.Spec.MachineTemplate.NodeVolumeDetachTimeout).To(Equal(&metav1.Duration{Duration: 120000000000}))
			Expect(dst.Spec.MachineTemplate.NodeDeletionTimeout).To(Equal(&metav1.Duration{Duration: 30000000000}))

			// Verify Status
			Expect(dst.Status.Initialized).To(BeTrue())
			Expect(dst.Status.Replicas).To(Equal(int32(3)))
			Expect(dst.Status.ReadyReplicas).To(Equal(int32(2)))
			Expect(dst.Status.UpdatedReplicas).To(Equal(int32(3)))
			Expect(*dst.Status.Version).To(Equal("1.27.0"))
		})

		It("should restore v1alpha2 status fields from v1alpha3 status", func() {
			src := &controlplanev1alpha3.OpenshiftAssistedControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "default",
				},
				Spec: controlplanev1alpha3.OpenshiftAssistedControlPlaneSpec{
					Replicas:            3,
					DistributionVersion: "4.14.0",
					MachineTemplate: controlplanev1alpha3.OpenshiftAssistedControlPlaneMachineTemplate{
						InfrastructureRef: clusterv1.ContractVersionedObjectReference{
							Kind:     "Metal3MachineTemplate",
							Name:     "test-infra",
							APIGroup: "infrastructure.cluster.x-k8s.io",
						},
					},
				},
				Status: controlplanev1alpha3.OpenshiftAssistedControlPlaneStatus{
					Selector:          "cluster.x-k8s.io/cluster-name=test",
					Replicas:          ptr.To(int32(3)),
					ReadyReplicas:     ptr.To(int32(2)),
					UpToDateReplicas:  ptr.To(int32(3)),
					AvailableReplicas: ptr.To(int32(2)),
					Initialization: controlplanev1alpha3.OpenshiftAssistedControlPlaneInitializationStatus{
						ControlPlaneInitialized: ptr.To(true),
					},
					Conditions: []metav1.Condition{
						{
							Type:   string(ControlPlaneAvailableCondition),
							Status: metav1.ConditionTrue,
							Reason: "Ready",
						},
					},
				},
			}

			dst := &OpenshiftAssistedControlPlane{}
			Expect(dst.ConvertFrom(src)).To(Succeed())

			// Verify status fields are restored from v1alpha3
			Expect(dst.Status.Selector).To(Equal("cluster.x-k8s.io/cluster-name=test"))
			Expect(dst.Status.Replicas).To(Equal(int32(3)))
			Expect(dst.Status.ReadyReplicas).To(Equal(int32(2)))
			Expect(dst.Status.UpdatedReplicas).To(Equal(int32(3)))
			Expect(dst.Status.UnavailableReplicas).To(Equal(int32(1))) // computed: 3 - 2 = 1
			Expect(dst.Status.Initialized).To(BeTrue())
			Expect(dst.Status.Ready).To(BeTrue()) // derived from ControlPlaneAvailable condition
			Expect(dst.Status.Conditions).To(HaveLen(1))
		})
	})

	Context("Condition Conversion", func() {
		var testTime metav1.Time

		BeforeEach(func() {
			testTime = metav1.NewTime(time.Date(2024, 6, 15, 12, 30, 0, 0, time.UTC))
		})

		Context("ConvertTo (v1alpha2 -> v1alpha3)", func() {
			It("should convert conditions with all fields correctly", func() {
				src := &OpenshiftAssistedControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cp",
						Namespace: "default",
					},
					Spec: OpenshiftAssistedControlPlaneSpec{
						Replicas:            3,
						DistributionVersion: "4.14.0",
						MachineTemplate: OpenshiftAssistedControlPlaneMachineTemplate{
							InfrastructureRef: corev1.ObjectReference{
								APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
								Kind:       "Metal3MachineTemplate",
								Name:       "test-infra",
							},
						},
					},
					Status: OpenshiftAssistedControlPlaneStatus{
						Conditions: clusterv1beta1.Conditions{
							{
								Type:               ControlPlaneAvailableCondition,
								Status:             corev1.ConditionTrue,
								LastTransitionTime: testTime,
								Reason:             "AllComponentsReady",
								Message:            "Control plane is fully operational",
								Severity:           clusterv1beta1.ConditionSeverityInfo,
							},
							{
								Type:               "MachinesReady",
								Status:             corev1.ConditionFalse,
								LastTransitionTime: testTime,
								Reason:             "MachineNotReady",
								Message:            "1 of 3 machines are not ready",
								Severity:           clusterv1beta1.ConditionSeverityWarning,
							},
							{
								Type:               "KubeconfigAvailable",
								Status:             corev1.ConditionUnknown,
								LastTransitionTime: testTime,
								Reason:             "CheckingKubeconfig",
								Message:            "Verifying kubeconfig availability",
								Severity:           clusterv1beta1.ConditionSeverityInfo,
							},
						},
					},
				}

				dst := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
				Expect(src.ConvertTo(dst)).To(Succeed())

				// Verify new conditions (metav1.Condition format)
				Expect(dst.Status.Conditions).To(HaveLen(3))

				// Find and verify ControlPlaneAvailable condition
				var cpReady *metav1.Condition
				for i := range dst.Status.Conditions {
					if dst.Status.Conditions[i].Type == string(ControlPlaneAvailableCondition) {
						cpReady = &dst.Status.Conditions[i]
						break
					}
				}
				Expect(cpReady).ToNot(BeNil())
				Expect(cpReady.Status).To(Equal(metav1.ConditionTrue))
				Expect(cpReady.Reason).To(Equal("AllComponentsReady"))
				Expect(cpReady.Message).To(Equal("Control plane is fully operational"))
				Expect(cpReady.LastTransitionTime).To(Equal(testTime))

				// Find and verify MachinesReady condition
				var machinesReady *metav1.Condition
				for i := range dst.Status.Conditions {
					if dst.Status.Conditions[i].Type == "MachinesReady" {
						machinesReady = &dst.Status.Conditions[i]
						break
					}
				}
				Expect(machinesReady).ToNot(BeNil())
				Expect(machinesReady.Status).To(Equal(metav1.ConditionFalse))
				Expect(machinesReady.Reason).To(Equal("MachineNotReady"))
				Expect(machinesReady.Message).To(Equal("1 of 3 machines are not ready"))
			})

			It("should convert empty reason to 'Unknown'", func() {
				src := &OpenshiftAssistedControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cp",
						Namespace: "default",
					},
					Spec: OpenshiftAssistedControlPlaneSpec{
						Replicas:            1,
						DistributionVersion: "4.14.0",
						MachineTemplate: OpenshiftAssistedControlPlaneMachineTemplate{
							InfrastructureRef: corev1.ObjectReference{
								APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
								Kind:       "Metal3MachineTemplate",
								Name:       "test-infra",
							},
						},
					},
					Status: OpenshiftAssistedControlPlaneStatus{
						Conditions: clusterv1beta1.Conditions{
							{
								Type:               "TestCondition",
								Status:             corev1.ConditionTrue,
								LastTransitionTime: testTime,
								Reason:             "", // Empty reason
								Message:            "Test message",
							},
						},
					},
				}

				dst := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
				Expect(src.ConvertTo(dst)).To(Succeed())

				Expect(dst.Status.Conditions).To(HaveLen(1))
				// metav1.Condition requires non-empty reason, should be "Unknown"
				Expect(dst.Status.Conditions[0].Reason).To(Equal("Unknown"))
				Expect(dst.Status.Conditions[0].Message).To(Equal("Test message"))
			})

			It("should preserve long messages during conversion", func() {
				longMessage := "This is a very long message that describes a complex error scenario " +
					"involving multiple components and their interactions. The error occurred during " +
					"the reconciliation of the control plane resources when attempting to create " +
					"the necessary infrastructure components for the cluster deployment. Please check " +
					"the controller logs for more detailed information about the root cause."

				src := &OpenshiftAssistedControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cp",
						Namespace: "default",
					},
					Spec: OpenshiftAssistedControlPlaneSpec{
						Replicas:            1,
						DistributionVersion: "4.14.0",
						MachineTemplate: OpenshiftAssistedControlPlaneMachineTemplate{
							InfrastructureRef: corev1.ObjectReference{
								APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
								Kind:       "Metal3MachineTemplate",
								Name:       "test-infra",
							},
						},
					},
					Status: OpenshiftAssistedControlPlaneStatus{
						Conditions: clusterv1beta1.Conditions{
							{
								Type:               "ErrorCondition",
								Status:             corev1.ConditionFalse,
								LastTransitionTime: testTime,
								Reason:             "ComplexError",
								Message:            longMessage,
								Severity:           clusterv1beta1.ConditionSeverityError,
							},
						},
					},
				}

				dst := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
				Expect(src.ConvertTo(dst)).To(Succeed())

				Expect(dst.Status.Conditions).To(HaveLen(1))
				Expect(dst.Status.Conditions[0].Message).To(Equal(longMessage))
			})
		})

		Context("ConvertFrom (v1alpha3 -> v1alpha2)", func() {
			It("should convert metav1.Conditions to v1beta1.Conditions correctly", func() {
				src := &controlplanev1alpha3.OpenshiftAssistedControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cp",
						Namespace: "default",
					},
					Spec: controlplanev1alpha3.OpenshiftAssistedControlPlaneSpec{
						Replicas:            3,
						DistributionVersion: "4.14.0",
						MachineTemplate: controlplanev1alpha3.OpenshiftAssistedControlPlaneMachineTemplate{
							InfrastructureRef: clusterv1.ContractVersionedObjectReference{
								Kind:     "Metal3MachineTemplate",
								Name:     "test-infra",
								APIGroup: "infrastructure.cluster.x-k8s.io",
							},
						},
					},
					Status: controlplanev1alpha3.OpenshiftAssistedControlPlaneStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(ControlPlaneAvailableCondition),
								Status:             metav1.ConditionTrue,
								LastTransitionTime: testTime,
								Reason:             "AllReady",
								Message:            "All control plane components are ready",
							},
							{
								Type:               "UpgradeAvailable",
								Status:             metav1.ConditionTrue,
								LastTransitionTime: testTime,
								Reason:             "NewVersionAvailable",
								Message:            "Version 4.15.0 is available for upgrade",
							},
						},
					},
				}

				dst := &OpenshiftAssistedControlPlane{}
				Expect(dst.ConvertFrom(src)).To(Succeed())

				Expect(dst.Status.Conditions).To(HaveLen(2))

				// Find and verify ControlPlaneAvailable condition
				var cpReady *clusterv1beta1.Condition
				for i := range dst.Status.Conditions {
					if dst.Status.Conditions[i].Type == ControlPlaneAvailableCondition {
						cpReady = &dst.Status.Conditions[i]
						break
					}
				}
				Expect(cpReady).ToNot(BeNil())
				Expect(cpReady.Status).To(Equal(corev1.ConditionTrue))
				Expect(cpReady.Reason).To(Equal("AllReady"))
				Expect(cpReady.Message).To(Equal("All control plane components are ready"))
				Expect(cpReady.LastTransitionTime).To(Equal(testTime))
			})

			It("should convert multiple conditions", func() {
				src := &controlplanev1alpha3.OpenshiftAssistedControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cp",
						Namespace: "default",
					},
					Spec: controlplanev1alpha3.OpenshiftAssistedControlPlaneSpec{
						Replicas:            3,
						DistributionVersion: "4.14.0",
						MachineTemplate: controlplanev1alpha3.OpenshiftAssistedControlPlaneMachineTemplate{
							InfrastructureRef: clusterv1.ContractVersionedObjectReference{
								Kind:     "Metal3MachineTemplate",
								Name:     "test-infra",
								APIGroup: "infrastructure.cluster.x-k8s.io",
							},
						},
					},
					Status: controlplanev1alpha3.OpenshiftAssistedControlPlaneStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(ControlPlaneAvailableCondition),
								Status:             metav1.ConditionTrue,
								LastTransitionTime: testTime,
								Reason:             "AllReady",
								Message:            "Control plane is ready",
							},
							{
								Type:               "MachinesReady",
								Status:             metav1.ConditionTrue,
								LastTransitionTime: testTime,
								Reason:             "AllMachinesReady",
								Message:            "All machines are ready",
							},
						},
					},
				}

				dst := &OpenshiftAssistedControlPlane{}
				Expect(dst.ConvertFrom(src)).To(Succeed())

				// Should have 2 conditions
				Expect(dst.Status.Conditions).To(HaveLen(2))

				var cpReady *clusterv1beta1.Condition
				var machinesReady *clusterv1beta1.Condition
				for i := range dst.Status.Conditions {
					if dst.Status.Conditions[i].Type == ControlPlaneAvailableCondition {
						cpReady = &dst.Status.Conditions[i]
					}
					if dst.Status.Conditions[i].Type == "MachinesReady" {
						machinesReady = &dst.Status.Conditions[i]
					}
				}

				Expect(cpReady).ToNot(BeNil())
				Expect(cpReady.Status).To(Equal(corev1.ConditionTrue))
				Expect(cpReady.Reason).To(Equal("AllReady"))
				Expect(cpReady.Message).To(Equal("Control plane is ready"))

				Expect(machinesReady).ToNot(BeNil())
				Expect(machinesReady.Status).To(Equal(corev1.ConditionTrue))
				Expect(machinesReady.Reason).To(Equal("AllMachinesReady"))
				Expect(machinesReady.Message).To(Equal("All machines are ready"))
			})

			It("should set Ready status based on ControlPlaneAvailable condition", func() {
				src := &controlplanev1alpha3.OpenshiftAssistedControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cp",
						Namespace: "default",
					},
					Spec: controlplanev1alpha3.OpenshiftAssistedControlPlaneSpec{
						Replicas:            3,
						DistributionVersion: "4.14.0",
						MachineTemplate: controlplanev1alpha3.OpenshiftAssistedControlPlaneMachineTemplate{
							InfrastructureRef: clusterv1.ContractVersionedObjectReference{
								Kind:     "Metal3MachineTemplate",
								Name:     "test-infra",
								APIGroup: "infrastructure.cluster.x-k8s.io",
							},
						},
					},
					Status: controlplanev1alpha3.OpenshiftAssistedControlPlaneStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(ControlPlaneAvailableCondition),
								Status:             metav1.ConditionTrue,
								LastTransitionTime: testTime,
								Reason:             "Ready",
								Message:            "Control plane is ready",
							},
						},
					},
				}

				dst := &OpenshiftAssistedControlPlane{}
				Expect(dst.ConvertFrom(src)).To(Succeed())

				// Ready should be derived from ControlPlaneAvailable condition
				Expect(dst.Status.Ready).To(BeTrue())
			})

			It("should not set Ready when ControlPlaneAvailable is false", func() {
				src := &controlplanev1alpha3.OpenshiftAssistedControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cp",
						Namespace: "default",
					},
					Spec: controlplanev1alpha3.OpenshiftAssistedControlPlaneSpec{
						Replicas:            3,
						DistributionVersion: "4.14.0",
						MachineTemplate: controlplanev1alpha3.OpenshiftAssistedControlPlaneMachineTemplate{
							InfrastructureRef: clusterv1.ContractVersionedObjectReference{
								Kind:     "Metal3MachineTemplate",
								Name:     "test-infra",
								APIGroup: "infrastructure.cluster.x-k8s.io",
							},
						},
					},
					Status: controlplanev1alpha3.OpenshiftAssistedControlPlaneStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(ControlPlaneAvailableCondition),
								Status:             metav1.ConditionFalse,
								LastTransitionTime: testTime,
								Reason:             "NotReady",
								Message:            "Control plane is not ready",
							},
						},
					},
				}

				dst := &OpenshiftAssistedControlPlane{}
				Expect(dst.ConvertFrom(src)).To(Succeed())

				// Ready should be false
				Expect(dst.Status.Ready).To(BeFalse())
			})
		})

		Context("Round-trip conversion", func() {
			It("should preserve conditions through v1alpha2 -> v1alpha3 -> v1alpha2", func() {
				original := &OpenshiftAssistedControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cp",
						Namespace: "default",
					},
					Spec: OpenshiftAssistedControlPlaneSpec{
						Replicas:            3,
						DistributionVersion: "4.14.0",
						MachineTemplate: OpenshiftAssistedControlPlaneMachineTemplate{
							InfrastructureRef: corev1.ObjectReference{
								APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
								Kind:       "Metal3MachineTemplate",
								Name:       "test-infra",
							},
						},
					},
					Status: OpenshiftAssistedControlPlaneStatus{
						Ready:       true,
						Initialized: true,
						Replicas:    3,
						Conditions: clusterv1beta1.Conditions{
							{
								Type:               ControlPlaneAvailableCondition,
								Status:             corev1.ConditionTrue,
								LastTransitionTime: testTime,
								Reason:             "AllReady",
								Message:            "Control plane is fully operational",
								Severity:           clusterv1beta1.ConditionSeverityInfo,
							},
							{
								Type:               "MachinesReady",
								Status:             corev1.ConditionTrue,
								LastTransitionTime: testTime,
								Reason:             "AllMachinesReady",
								Message:            "All 3 machines are ready",
								Severity:           clusterv1beta1.ConditionSeverityInfo,
							},
							{
								Type:               "UpgradeCompleted",
								Status:             corev1.ConditionFalse,
								LastTransitionTime: testTime,
								Reason:             "NoUpgradeAttempted",
								Message:            "No upgrade has been attempted",
								Severity:           clusterv1beta1.ConditionSeverityInfo,
							},
						},
					},
				}

				// Convert to v1alpha3
				v1alpha3Obj := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
				Expect(original.ConvertTo(v1alpha3Obj)).To(Succeed())

				// Convert back to v1alpha2
				roundTripped := &OpenshiftAssistedControlPlane{}
				Expect(roundTripped.ConvertFrom(v1alpha3Obj)).To(Succeed())

				// Verify conditions are preserved
				Expect(roundTripped.Status.Conditions).To(HaveLen(3))

				// Verify each condition type is present with correct values
				conditionTypes := make(map[clusterv1beta1.ConditionType]clusterv1beta1.Condition)
				for _, c := range roundTripped.Status.Conditions {
					conditionTypes[c.Type] = c
				}

				Expect(conditionTypes).To(HaveKey(ControlPlaneAvailableCondition))
				Expect(conditionTypes).To(HaveKey(clusterv1beta1.ConditionType("MachinesReady")))
				Expect(conditionTypes).To(HaveKey(clusterv1beta1.ConditionType("UpgradeCompleted")))

				// Verify specific condition values
				cpReady := conditionTypes[ControlPlaneAvailableCondition]
				Expect(cpReady.Status).To(Equal(corev1.ConditionTrue))
				Expect(cpReady.Reason).To(Equal("AllReady"))
				Expect(cpReady.Message).To(Equal("Control plane is fully operational"))

				upgradeCompleted := conditionTypes["UpgradeCompleted"]
				Expect(upgradeCompleted.Status).To(Equal(corev1.ConditionFalse))
				Expect(upgradeCompleted.Reason).To(Equal("NoUpgradeAttempted"))
				Expect(upgradeCompleted.Message).To(Equal("No upgrade has been attempted"))
			})

			It("should preserve upgrade-related conditions through round-trip", func() {
				original := &OpenshiftAssistedControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cp",
						Namespace: "default",
					},
					Spec: OpenshiftAssistedControlPlaneSpec{
						Replicas:            3,
						DistributionVersion: "4.15.0",
						MachineTemplate: OpenshiftAssistedControlPlaneMachineTemplate{
							InfrastructureRef: corev1.ObjectReference{
								APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
								Kind:       "Metal3MachineTemplate",
								Name:       "test-infra",
							},
						},
					},
					Status: OpenshiftAssistedControlPlaneStatus{
						DistributionVersion: "4.14.0",
						Conditions: clusterv1beta1.Conditions{
							{
								Type:               "UpgradeAvailable",
								Status:             corev1.ConditionTrue,
								LastTransitionTime: testTime,
								Reason:             "NewVersionAvailable",
								Message:            "Version 4.15.0 is available for upgrade from 4.14.0",
								Severity:           clusterv1beta1.ConditionSeverityInfo,
							},
							{
								Type:               "UpgradeCompleted",
								Status:             corev1.ConditionFalse,
								LastTransitionTime: testTime,
								Reason:             "UpgradeInProgress",
								Message:            "Upgrade to 4.15.0 is in progress: 50% complete",
								Severity:           clusterv1beta1.ConditionSeverityInfo,
							},
						},
					},
				}

				// Convert to v1alpha3
				v1alpha3Obj := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
				Expect(original.ConvertTo(v1alpha3Obj)).To(Succeed())

				// Verify v1alpha3 has the upgrade conditions
				Expect(v1alpha3Obj.Status.Conditions).To(HaveLen(2))

				var upgradeAvailable *metav1.Condition
				var upgradeCompleted *metav1.Condition
				for i := range v1alpha3Obj.Status.Conditions {
					if v1alpha3Obj.Status.Conditions[i].Type == "UpgradeAvailable" {
						upgradeAvailable = &v1alpha3Obj.Status.Conditions[i]
					}
					if v1alpha3Obj.Status.Conditions[i].Type == "UpgradeCompleted" {
						upgradeCompleted = &v1alpha3Obj.Status.Conditions[i]
					}
				}

				Expect(upgradeAvailable).ToNot(BeNil())
				Expect(upgradeAvailable.Message).To(Equal("Version 4.15.0 is available for upgrade from 4.14.0"))

				Expect(upgradeCompleted).ToNot(BeNil())
				Expect(upgradeCompleted.Message).To(Equal("Upgrade to 4.15.0 is in progress: 50% complete"))

				// Convert back to v1alpha2
				roundTripped := &OpenshiftAssistedControlPlane{}
				Expect(roundTripped.ConvertFrom(v1alpha3Obj)).To(Succeed())

				// Verify upgrade conditions are preserved
				Expect(roundTripped.Status.Conditions).To(HaveLen(2))

				conditionTypes := make(map[clusterv1beta1.ConditionType]clusterv1beta1.Condition)
				for _, c := range roundTripped.Status.Conditions {
					conditionTypes[c.Type] = c
				}

				Expect(conditionTypes).To(HaveKey(clusterv1beta1.ConditionType("UpgradeAvailable")))
				Expect(conditionTypes).To(HaveKey(clusterv1beta1.ConditionType("UpgradeCompleted")))

				// Verify message preservation
				Expect(conditionTypes["UpgradeAvailable"].Message).To(Equal("Version 4.15.0 is available for upgrade from 4.14.0"))
				Expect(conditionTypes["UpgradeCompleted"].Message).To(Equal("Upgrade to 4.15.0 is in progress: 50% complete"))
			})
		})
	})
})
