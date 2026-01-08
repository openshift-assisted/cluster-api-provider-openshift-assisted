package util

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("FindStatusCondition", func() {
	var (
		testTime metav1.Time
	)

	BeforeEach(func() {
		testTime = metav1.NewTime(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	})

	Context("when conditions slice is nil", func() {
		It("returns nil", func() {
			var conditions clusterv1.Conditions
			result := FindStatusCondition(conditions, "SomeCondition")
			Expect(result).To(BeNil())
		})
	})

	Context("when conditions slice is empty", func() {
		It("returns nil", func() {
			conditions := clusterv1.Conditions{}
			result := FindStatusCondition(conditions, "SomeCondition")
			Expect(result).To(BeNil())
		})
	})

	Context("when condition is not found", func() {
		It("returns nil when no matching condition exists", func() {
			conditions := clusterv1.Conditions{
				{
					Type:               "OtherCondition",
					Status:             corev1.ConditionTrue,
					LastTransitionTime: testTime,
					Reason:             "SomeReason",
					Message:            "Some message",
				},
			}
			result := FindStatusCondition(conditions, "NonExistentCondition")
			Expect(result).To(BeNil())
		})
	})

	Context("when condition is found", func() {
		It("returns the condition with matching type", func() {
			conditions := clusterv1.Conditions{
				{
					Type:               "ReadyCondition",
					Status:             corev1.ConditionTrue,
					LastTransitionTime: testTime,
					Reason:             "AllSystemsGo",
					Message:            "Everything is ready",
				},
			}
			result := FindStatusCondition(conditions, "ReadyCondition")
			Expect(result).NotTo(BeNil())
			Expect(result.Type).To(Equal(clusterv1.ConditionType("ReadyCondition")))
		})

		It("returns the correct condition when multiple conditions exist", func() {
			conditions := clusterv1.Conditions{
				{
					Type:               "FirstCondition",
					Status:             corev1.ConditionFalse,
					LastTransitionTime: testTime,
					Reason:             "FirstReason",
					Message:            "First message",
				},
				{
					Type:               "SecondCondition",
					Status:             corev1.ConditionTrue,
					LastTransitionTime: testTime,
					Reason:             "SecondReason",
					Message:            "Second message",
				},
				{
					Type:               "ThirdCondition",
					Status:             corev1.ConditionUnknown,
					LastTransitionTime: testTime,
					Reason:             "ThirdReason",
					Message:            "Third message",
				},
			}
			result := FindStatusCondition(conditions, "SecondCondition")
			Expect(result).NotTo(BeNil())
			Expect(result.Type).To(Equal(clusterv1.ConditionType("SecondCondition")))
			Expect(result.Reason).To(Equal("SecondReason"))
		})
	})

	Context("condition field preservation", func() {
		It("preserves Status field correctly for ConditionTrue", func() {
			conditions := clusterv1.Conditions{
				{
					Type:               "TestCondition",
					Status:             corev1.ConditionTrue,
					LastTransitionTime: testTime,
				},
			}
			result := FindStatusCondition(conditions, "TestCondition")
			Expect(result).NotTo(BeNil())
			Expect(result.Status).To(Equal(corev1.ConditionTrue))
		})

		It("preserves Status field correctly for ConditionFalse", func() {
			conditions := clusterv1.Conditions{
				{
					Type:               "TestCondition",
					Status:             corev1.ConditionFalse,
					LastTransitionTime: testTime,
				},
			}
			result := FindStatusCondition(conditions, "TestCondition")
			Expect(result).NotTo(BeNil())
			Expect(result.Status).To(Equal(corev1.ConditionFalse))
		})

		It("preserves Status field correctly for ConditionUnknown", func() {
			conditions := clusterv1.Conditions{
				{
					Type:               "TestCondition",
					Status:             corev1.ConditionUnknown,
					LastTransitionTime: testTime,
				},
			}
			result := FindStatusCondition(conditions, "TestCondition")
			Expect(result).NotTo(BeNil())
			Expect(result.Status).To(Equal(corev1.ConditionUnknown))
		})

		It("preserves Severity field correctly", func() {
			conditions := clusterv1.Conditions{
				{
					Type:               "ErrorCondition",
					Status:             corev1.ConditionFalse,
					Severity:           clusterv1.ConditionSeverityError,
					LastTransitionTime: testTime,
				},
				{
					Type:               "WarningCondition",
					Status:             corev1.ConditionFalse,
					Severity:           clusterv1.ConditionSeverityWarning,
					LastTransitionTime: testTime,
				},
				{
					Type:               "InfoCondition",
					Status:             corev1.ConditionFalse,
					Severity:           clusterv1.ConditionSeverityInfo,
					LastTransitionTime: testTime,
				},
			}

			errorResult := FindStatusCondition(conditions, "ErrorCondition")
			Expect(errorResult).NotTo(BeNil())
			Expect(errorResult.Severity).To(Equal(clusterv1.ConditionSeverityError))

			warningResult := FindStatusCondition(conditions, "WarningCondition")
			Expect(warningResult).NotTo(BeNil())
			Expect(warningResult.Severity).To(Equal(clusterv1.ConditionSeverityWarning))

			infoResult := FindStatusCondition(conditions, "InfoCondition")
			Expect(infoResult).NotTo(BeNil())
			Expect(infoResult.Severity).To(Equal(clusterv1.ConditionSeverityInfo))
		})

		It("preserves LastTransitionTime field correctly", func() {
			expectedTime := metav1.NewTime(time.Date(2023, 6, 15, 12, 30, 45, 0, time.UTC))
			conditions := clusterv1.Conditions{
				{
					Type:               "TestCondition",
					Status:             corev1.ConditionTrue,
					LastTransitionTime: expectedTime,
				},
			}
			result := FindStatusCondition(conditions, "TestCondition")
			Expect(result).NotTo(BeNil())
			Expect(result.LastTransitionTime).To(Equal(expectedTime))
		})

		It("preserves Reason field correctly", func() {
			conditions := clusterv1.Conditions{
				{
					Type:               "TestCondition",
					Status:             corev1.ConditionFalse,
					LastTransitionTime: testTime,
					Reason:             "ResourceNotFound",
				},
			}
			result := FindStatusCondition(conditions, "TestCondition")
			Expect(result).NotTo(BeNil())
			Expect(result.Reason).To(Equal("ResourceNotFound"))
		})

		It("preserves Message field correctly", func() {
			conditions := clusterv1.Conditions{
				{
					Type:               "TestCondition",
					Status:             corev1.ConditionFalse,
					LastTransitionTime: testTime,
					Message:            "The resource was not found in the cluster",
				},
			}
			result := FindStatusCondition(conditions, "TestCondition")
			Expect(result).NotTo(BeNil())
			Expect(result.Message).To(Equal("The resource was not found in the cluster"))
		})

		It("preserves all fields correctly in a complete condition", func() {
			expectedTime := metav1.NewTime(time.Date(2024, 3, 20, 8, 15, 30, 0, time.UTC))
			conditions := clusterv1.Conditions{
				{
					Type:               "CompleteCondition",
					Status:             corev1.ConditionFalse,
					Severity:           clusterv1.ConditionSeverityWarning,
					LastTransitionTime: expectedTime,
					Reason:             "DependencyUnavailable",
					Message:            "Dependency service is temporarily unavailable, retrying in 30 seconds",
				},
			}
			result := FindStatusCondition(conditions, "CompleteCondition")
			Expect(result).NotTo(BeNil())
			Expect(result.Type).To(Equal(clusterv1.ConditionType("CompleteCondition")))
			Expect(result.Status).To(Equal(corev1.ConditionFalse))
			Expect(result.Severity).To(Equal(clusterv1.ConditionSeverityWarning))
			Expect(result.LastTransitionTime).To(Equal(expectedTime))
			Expect(result.Reason).To(Equal("DependencyUnavailable"))
			Expect(result.Message).To(Equal("Dependency service is temporarily unavailable, retrying in 30 seconds"))
		})
	})

	Context("with real-world condition types from this project", func() {
		It("finds DataSecretAvailable condition", func() {
			conditions := clusterv1.Conditions{
				{
					Type:               "DataSecretAvailable",
					Status:             corev1.ConditionTrue,
					LastTransitionTime: testTime,
					Reason:             "SecretCreated",
					Message:            "Data secret has been created successfully",
				},
			}
			result := FindStatusCondition(conditions, "DataSecretAvailable")
			Expect(result).NotTo(BeNil())
			Expect(result.Type).To(Equal(clusterv1.ConditionType("DataSecretAvailable")))
			Expect(result.Status).To(Equal(corev1.ConditionTrue))
			Expect(result.Reason).To(Equal("SecretCreated"))
			Expect(result.Message).To(Equal("Data secret has been created successfully"))
		})

		It("finds ControlPlaneReady condition with error severity", func() {
			conditions := clusterv1.Conditions{
				{
					Type:               "ControlPlaneReady",
					Status:             corev1.ConditionFalse,
					Severity:           clusterv1.ConditionSeverityError,
					LastTransitionTime: testTime,
					Reason:             "ControlPlaneInstalling",
					Message:            "Control plane installation is in progress",
				},
			}
			result := FindStatusCondition(conditions, "ControlPlaneReady")
			Expect(result).NotTo(BeNil())
			Expect(result.Type).To(Equal(clusterv1.ConditionType("ControlPlaneReady")))
			Expect(result.Status).To(Equal(corev1.ConditionFalse))
			Expect(result.Severity).To(Equal(clusterv1.ConditionSeverityError))
			Expect(result.Reason).To(Equal("ControlPlaneInstalling"))
		})

		It("finds KubeconfigAvailable condition", func() {
			conditions := clusterv1.Conditions{
				{
					Type:               "KubeconfigAvailable",
					Status:             corev1.ConditionFalse,
					Severity:           clusterv1.ConditionSeverityInfo,
					LastTransitionTime: testTime,
					Reason:             "KubeconfigUnavailable",
					Message:            "Kubeconfig is not yet available",
				},
			}
			result := FindStatusCondition(conditions, "KubeconfigAvailable")
			Expect(result).NotTo(BeNil())
			Expect(result.Type).To(Equal(clusterv1.ConditionType("KubeconfigAvailable")))
			Expect(result.Status).To(Equal(corev1.ConditionFalse))
			Expect(result.Reason).To(Equal("KubeconfigUnavailable"))
			Expect(result.Message).To(Equal("Kubeconfig is not yet available"))
		})

		It("finds MachinesCreated condition", func() {
			conditions := clusterv1.Conditions{
				{
					Type:               "MachinesCreated",
					Status:             corev1.ConditionTrue,
					LastTransitionTime: testTime,
					Reason:             "MachinesReady",
					Message:            "All machines have been created",
				},
			}
			result := FindStatusCondition(conditions, "MachinesCreated")
			Expect(result).NotTo(BeNil())
			Expect(result.Type).To(Equal(clusterv1.ConditionType("MachinesCreated")))
			Expect(result.Status).To(Equal(corev1.ConditionTrue))
			Expect(result.Reason).To(Equal("MachinesReady"))
		})
	})

	Context("edge cases", func() {
		It("returns the first matching condition when duplicates exist", func() {
			conditions := clusterv1.Conditions{
				{
					Type:               "DuplicateCondition",
					Status:             corev1.ConditionTrue,
					LastTransitionTime: testTime,
					Reason:             "FirstOccurrence",
					Message:            "This is the first occurrence",
				},
				{
					Type:               "DuplicateCondition",
					Status:             corev1.ConditionFalse,
					LastTransitionTime: testTime,
					Reason:             "SecondOccurrence",
					Message:            "This is the second occurrence",
				},
			}
			result := FindStatusCondition(conditions, "DuplicateCondition")
			Expect(result).NotTo(BeNil())
			Expect(result.Reason).To(Equal("FirstOccurrence"))
			Expect(result.Message).To(Equal("This is the first occurrence"))
		})

		It("handles condition with empty optional fields", func() {
			conditions := clusterv1.Conditions{
				{
					Type:               "MinimalCondition",
					Status:             corev1.ConditionTrue,
					LastTransitionTime: testTime,
					Reason:             "",
					Message:            "",
				},
			}
			result := FindStatusCondition(conditions, "MinimalCondition")
			Expect(result).NotTo(BeNil())
			Expect(result.Type).To(Equal(clusterv1.ConditionType("MinimalCondition")))
			Expect(result.Status).To(Equal(corev1.ConditionTrue))
			Expect(result.Reason).To(BeEmpty())
			Expect(result.Message).To(BeEmpty())
		})

		It("handles condition with long message", func() {
			longMessage := "This is a very long message that describes a complex error scenario " +
				"involving multiple components and their interactions. The error occurred during " +
				"the reconciliation of the control plane resources when attempting to create " +
				"the necessary infrastructure components for the cluster deployment."
			conditions := clusterv1.Conditions{
				{
					Type:               "LongMessageCondition",
					Status:             corev1.ConditionFalse,
					Severity:           clusterv1.ConditionSeverityError,
					LastTransitionTime: testTime,
					Reason:             "ComplexError",
					Message:            longMessage,
				},
			}
			result := FindStatusCondition(conditions, "LongMessageCondition")
			Expect(result).NotTo(BeNil())
			Expect(result.Message).To(Equal(longMessage))
		})
	})
})

var _ = Describe("GetTypedOwner", func() {
	var (
		ctx       context.Context
		scheme    *runtime.Scheme
		k8sClient client.Client
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(clusterv1.AddToScheme(scheme)).To(Succeed())
	})

	createFakeClient := func(objs ...client.Object) client.Client {
		return fakeclient.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(objs...).
			Build()
	}

	Context("when owner reference has a different API version than the typed owner", func() {
		It("finds the owner when group and kind match but version differs", func() {
			// Create a Machine that will be the owner (registered as v1beta2 in the scheme)
			machine := &clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-machine",
					Namespace: "default",
					UID:       "machine-uid",
				},
			}

			// Create a Secret with an owner reference using the OLD API version (v1beta1)
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "cluster.x-k8s.io/v1beta1", // Old version
							Kind:       "Machine",
							Name:       "test-machine",
							UID:        "machine-uid",
						},
					},
				},
			}

			k8sClient = createFakeClient(machine, secret)

			// Try to find the owner using the v1beta2 Machine type
			foundMachine := &clusterv1.Machine{}
			err := GetTypedOwner(ctx, k8sClient, secret, foundMachine)

			Expect(err).NotTo(HaveOccurred())
			Expect(foundMachine.Name).To(Equal("test-machine"))
		})
	})

	Context("when owner reference matches exactly", func() {
		It("finds the owner when group, kind, and version all match", func() {
			machine := &clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-machine",
					Namespace: "default",
					UID:       "machine-uid",
				},
			}

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: clusterv1.GroupVersion.String(), // Exact match
							Kind:       "Machine",
							Name:       "test-machine",
							UID:        "machine-uid",
						},
					},
				},
			}

			k8sClient = createFakeClient(machine, secret)

			foundMachine := &clusterv1.Machine{}
			err := GetTypedOwner(ctx, k8sClient, secret, foundMachine)

			Expect(err).NotTo(HaveOccurred())
			Expect(foundMachine.Name).To(Equal("test-machine"))
		})
	})

	Context("when no matching owner exists", func() {
		It("returns error when kind doesn't match", func() {
			cluster := &clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
					UID:       "cluster-uid",
				},
			}

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: clusterv1.GroupVersion.String(),
							Kind:       "Cluster",
							Name:       "test-cluster",
							UID:        "cluster-uid",
						},
					},
				},
			}

			k8sClient = createFakeClient(cluster, secret)

			foundMachine := &clusterv1.Machine{}
			err := GetTypedOwner(ctx, k8sClient, secret, foundMachine)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("couldn't find"))
		})

		It("returns error when group doesn't match", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "different.group.io/v1beta2",
							Kind:       "Machine",
							Name:       "test-machine",
							UID:        "machine-uid",
						},
					},
				},
			}

			k8sClient = createFakeClient(secret)

			foundMachine := &clusterv1.Machine{}
			err := GetTypedOwner(ctx, k8sClient, secret, foundMachine)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("couldn't find"))
		})

		It("returns error when object has no owner references", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
				},
			}

			k8sClient = createFakeClient(secret)

			foundMachine := &clusterv1.Machine{}
			err := GetTypedOwner(ctx, k8sClient, secret, foundMachine)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("couldn't find"))
		})
	})
})
