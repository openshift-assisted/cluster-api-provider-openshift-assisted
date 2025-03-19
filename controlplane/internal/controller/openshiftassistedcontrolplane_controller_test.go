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

package controller

import (
	"context"
	"fmt"
	"time"

	metal3v1beta1 "github.com/metal3-io/cluster-api-provider-metal3/api/v1beta1"
	"github.com/openshift/assisted-service/api/v1beta1"

	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/auth"
	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/release"
	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/upgrade"
	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/version"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/cluster-api/util/conditions"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	controlplanev1alpha2 "github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1alpha2"
	testutils "github.com/openshift-assisted/cluster-api-agent/test/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

var _ = Describe("OpenshiftAssistedControlPlane Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			openshiftAssistedControlPlaneName = "test-resource"
			clusterName                       = "test-cluster"
			namespace                         = "test"
		)
		var (
			k8sVersion                    = "1.30.0"
			ctx                           = context.Background()
			typeNamespacedName            types.NamespacedName
			cluster                       *clusterv1.Cluster
			controllerReconciler          *OpenshiftAssistedControlPlaneReconciler
			k8sClient                     client.Client
			ctrl                          *gomock.Controller
			mockKubernetesVersionDetector *version.MockKubernetesVersionDetector
			mockUpgradeFactory            *upgrade.MockClusterUpgradeFactory
			mockUpgrader                  *upgrade.MockClusterUpgrade
		)

		BeforeEach(func() {
			ctrl = gomock.NewController(GinkgoT())
			k8sClient = fakeclient.NewClientBuilder().
				WithScheme(testScheme).
				WithStatusSubresource(&controlplanev1alpha2.OpenshiftAssistedControlPlane{}).Build()

			mockKubernetesVersionDetector = version.NewMockKubernetesVersionDetector(ctrl)
			mockKubernetesVersionDetector.EXPECT().GetKubernetesVersion(gomock.Any(), gomock.Any()).Return(&k8sVersion, nil).AnyTimes()

			mockUpgrader = upgrade.NewMockClusterUpgrade(ctrl)
			mockUpgrader.EXPECT().IsUpgradeInProgress(gomock.Any()).Return(false, nil).AnyTimes()
			mockUpgrader.EXPECT().GetCurrentVersion(gomock.Any()).Return("4.18.0", nil).AnyTimes()
			mockUpgrader.EXPECT().IsDesiredVersionUpdated(gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()

			mockUpgradeFactory = upgrade.NewMockClusterUpgradeFactory(ctrl)
			mockUpgradeFactory.EXPECT().NewUpgrader(gomock.Any()).Return(mockUpgrader, nil).AnyTimes()

			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())
			typeNamespacedName = types.NamespacedName{
				Name:      openshiftAssistedControlPlaneName,
				Namespace: namespace,
			}
			controllerReconciler = &OpenshiftAssistedControlPlaneReconciler{
				Client:             k8sClient,
				Scheme:             k8sClient.Scheme(),
				K8sVersionDetector: mockKubernetesVersionDetector,
				UpgradeFactory:     mockUpgradeFactory,
			}

			By("creating a cluster")
			cluster = &clusterv1.Cluster{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace}, cluster)
			if err != nil && errors.IsNotFound(err) {
				cluster = testutils.NewCluster(clusterName, namespace)
				err := k8sClient.Create(ctx, cluster)
				Expect(err).NotTo(HaveOccurred())
			}
		})

		AfterEach(func() {
			ctrl.Finish()
			k8sClient = nil
		})

		When("when a cluster owns this OpenshiftAssistedControlPlane", func() {
			It("should successfully create a cluster deployment", func() {
				By("setting the cluster as the owner ref on the OACP")

				openshiftAssistedControlPlane := testutils.NewOpenshiftAssistedControlPlane(namespace, openshiftAssistedControlPlaneName)
				openshiftAssistedControlPlane.SetOwnerReferences(
					[]metav1.OwnerReference{
						*metav1.NewControllerRef(cluster, clusterv1.GroupVersion.WithKind(clusterv1.ClusterKind)),
					},
				)
				Expect(k8sClient.Create(ctx, openshiftAssistedControlPlane)).To(Succeed())

				By("checking if the OpenshiftAssistedControlPlane created the cluster deployment after reconcile")
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				expectedVersion := "1.30.0"
				Expect(err).NotTo(HaveOccurred())
				err = k8sClient.Get(ctx, typeNamespacedName, openshiftAssistedControlPlane)
				Expect(err).NotTo(HaveOccurred())
				Expect(openshiftAssistedControlPlane.Status.ClusterDeploymentRef).NotTo(BeNil())
				Expect(openshiftAssistedControlPlane.Status.ClusterDeploymentRef.Name).Should(Equal(openshiftAssistedControlPlane.Name))
				Expect(openshiftAssistedControlPlane.Status.Version).To(Equal(&expectedVersion))
				By("checking that the cluster deployment was created")
				cd := &hivev1.ClusterDeployment{}
				err = k8sClient.Get(ctx, typeNamespacedName, cd)
				Expect(err).NotTo(HaveOccurred())
				Expect(cd).NotTo(BeNil())

				// assert ClusterDeployment properties
				Expect(cd.Spec.ClusterName).To(Equal(clusterName))
				Expect(cd.Spec.BaseDomain).To(Equal(openshiftAssistedControlPlane.Spec.Config.BaseDomain))
				Expect(cd.Spec.PullSecretRef).To(Equal(openshiftAssistedControlPlane.Spec.Config.PullSecretRef))
			})
		})

		When("a cluster owns this OpenshiftAssistedControlPlane", func() {
			It("should successfully create a cluster deployment and match OpenshiftAssistedControlPlane properties", func() {
				By("setting the cluster as the owner ref on the OpenshiftAssistedControlPlane")

				pullSecret := auth.GenerateFakePullSecret("my-pullsecret", namespace)
				Expect(k8sClient.Create(ctx, pullSecret)).To(Succeed())

				kubeconfigSecret := &corev1.Secret{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster-kubeconfig",
						Namespace: namespace,
					},
					Data: map[string][]byte{"value": []byte("fake-kubeconfig")},
				}
				Expect(k8sClient.Create(ctx, kubeconfigSecret)).To(Succeed())

				openshiftAssistedControlPlane := testutils.NewOpenshiftAssistedControlPlane(namespace, openshiftAssistedControlPlaneName)
				openshiftAssistedControlPlane.Spec.Config.ClusterName = "my-cluster"
				openshiftAssistedControlPlane.Spec.Config.PullSecretRef = &corev1.LocalObjectReference{
					Name: "my-pullsecret",
				}
				openshiftAssistedControlPlane.Spec.Config.BaseDomain = "example.com"
				openshiftAssistedControlPlane.SetOwnerReferences(
					[]metav1.OwnerReference{
						*metav1.NewControllerRef(cluster, clusterv1.GroupVersion.WithKind(clusterv1.ClusterKind)),
					},
				)
				// Simulate AgentClusterInstall marking KubeconfigAvailableCondition and ControlPlaneReadyCondition
				conditions.MarkTrue(openshiftAssistedControlPlane, controlplanev1alpha2.KubeconfigAvailableCondition)
				conditions.MarkTrue(openshiftAssistedControlPlane, controlplanev1alpha2.ControlPlaneReadyCondition)

				Expect(k8sClient.Create(ctx, openshiftAssistedControlPlane)).To(Succeed())

				By("checking if the OpenshiftAssistedControlPlane created the cluster deployment after reconcile")
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				Expect(err).NotTo(HaveOccurred())
				err = k8sClient.Get(ctx, typeNamespacedName, openshiftAssistedControlPlane)
				Expect(err).NotTo(HaveOccurred())
				Expect(openshiftAssistedControlPlane.Status.ClusterDeploymentRef).NotTo(BeNil())
				Expect(openshiftAssistedControlPlane.Status.ClusterDeploymentRef.Name).Should(Equal(openshiftAssistedControlPlane.Name))
				By("checking conditions ready")
				expectedReadyConditions := []clusterv1.ConditionType{
					controlplanev1alpha2.MachinesCreatedCondition,
					controlplanev1alpha2.KubeconfigAvailableCondition,
					controlplanev1alpha2.KubernetesVersionAvailableCondition,
				}
				checkReadyConditions(expectedReadyConditions, openshiftAssistedControlPlane)

				By("checking that the cluster deployment was created")
				cd := &hivev1.ClusterDeployment{}
				err = k8sClient.Get(ctx, typeNamespacedName, cd)
				Expect(err).NotTo(HaveOccurred())
				Expect(cd).NotTo(BeNil())

				// assert ClusterDeployment properties
				Expect(cd.Spec.ClusterName).To(Equal(openshiftAssistedControlPlane.Spec.Config.ClusterName))
				Expect(cd.Spec.BaseDomain).To(Equal(openshiftAssistedControlPlane.Spec.Config.BaseDomain))
				Expect(cd.Spec.PullSecretRef).To(Equal(openshiftAssistedControlPlane.Spec.Config.PullSecretRef))
			})
		})

		When("a pull secret isn't set on the OpenshiftAssistedControlPlane", func() {
			It("should successfully create the ClusterDeployment with the default fake pull secret", func() {
				By("setting the cluster as the owner ref on the OpenshiftAssistedControlPlane")
				openshiftAssistedControlPlane := testutils.NewOpenshiftAssistedControlPlane(namespace, openshiftAssistedControlPlaneName)
				openshiftAssistedControlPlane.SetOwnerReferences(
					[]metav1.OwnerReference{
						*metav1.NewControllerRef(cluster, clusterv1.GroupVersion.WithKind(clusterv1.ClusterKind)),
					},
				)
				Expect(k8sClient.Create(ctx, openshiftAssistedControlPlane)).To(Succeed())

				By("checking if the OpenshiftAssistedControlPlane created the cluster deployment after reconcile")
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				Expect(err).NotTo(HaveOccurred())
				err = k8sClient.Get(ctx, typeNamespacedName, openshiftAssistedControlPlane)
				Expect(err).NotTo(HaveOccurred())
				Expect(openshiftAssistedControlPlane.Status.ClusterDeploymentRef).NotTo(BeNil())
				Expect(openshiftAssistedControlPlane.Status.ClusterDeploymentRef.Name).Should(Equal(openshiftAssistedControlPlane.Name))

				By("checking that the fake pull secret was created")
				pullSecret := &corev1.Secret{}
				err = k8sClient.Get(ctx, types.NamespacedName{Name: placeholderPullSecretName, Namespace: namespace}, pullSecret)
				Expect(err).NotTo(HaveOccurred())
				Expect(pullSecret).NotTo(BeNil())
				Expect(pullSecret.Data).NotTo(BeNil())
				Expect(pullSecret.Data).To(HaveKey(".dockerconfigjson"))

				By("checking that the cluster deployment was created and references the fake pull secret")
				cd := &hivev1.ClusterDeployment{}
				err = k8sClient.Get(ctx, typeNamespacedName, cd)
				Expect(err).NotTo(HaveOccurred())
				Expect(cd).NotTo(BeNil())
				Expect(cd.Spec.PullSecretRef).NotTo(BeNil())
				Expect(cd.Spec.PullSecretRef.Name).To(Equal(placeholderPullSecretName))

			})
		})
		It("should add a finalizer to the OpenshiftAssistedControlPlane if it's not being deleted", func() {
			By("setting the owner ref on the OpenshiftAssistedControlPlane")

			openshiftAssistedControlPlane := testutils.NewOpenshiftAssistedControlPlane(namespace, openshiftAssistedControlPlaneName)
			openshiftAssistedControlPlane.SetOwnerReferences(
				[]metav1.OwnerReference{
					*metav1.NewControllerRef(cluster, clusterv1.GroupVersion.WithKind(clusterv1.ClusterKind)),
				},
			)
			Expect(k8sClient.Create(ctx, openshiftAssistedControlPlane)).To(Succeed())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("checking if the OpenshiftAssistedControlPlane has the finalizer")
			err = k8sClient.Get(ctx, typeNamespacedName, openshiftAssistedControlPlane)
			Expect(err).NotTo(HaveOccurred())
			Expect(openshiftAssistedControlPlane.Finalizers).NotTo(BeEmpty())
			Expect(openshiftAssistedControlPlane.Finalizers).To(ContainElement(acpFinalizer))
		})
		When("an invalid version is set on the OpenshiftAssistedControlPlane", func() {
			It("should return error", func() {
				By("setting the cluster as the owner ref on the OpenshiftAssistedControlPlane")
				openshiftAssistedControlPlane := testutils.NewOpenshiftAssistedControlPlane(namespace, openshiftAssistedControlPlaneName)
				openshiftAssistedControlPlane.Spec.DistributionVersion = "4.12.0"
				openshiftAssistedControlPlane.SetOwnerReferences(
					[]metav1.OwnerReference{
						*metav1.NewControllerRef(cluster, clusterv1.GroupVersion.WithKind(clusterv1.ClusterKind)),
					},
				)
				Expect(k8sClient.Create(ctx, openshiftAssistedControlPlane)).To(Succeed())
				By("checking if the condition of invalid version")
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				Expect(err).NotTo(HaveOccurred())

				Expect(k8sClient.Get(ctx, typeNamespacedName, openshiftAssistedControlPlane)).To(Succeed())

				condition := conditions.Get(openshiftAssistedControlPlane,
					controlplanev1alpha2.MachinesCreatedCondition,
				)
				Expect(condition).NotTo(BeNil())
				Expect(condition.Message).To(Equal("version 4.12.0 is not supported, the minimum supported version is 4.14.0"))

			})
		})
	})
})

var _ = Describe("Upgrade scenarios", func() {
	const (
		openshiftAssistedControlPlaneName = "test-resource"
		clusterName                       = "test-cluster"
		namespace                         = "test"
		currentVersion                    = "4.14.0"
		desiredVersion                    = "4.15.0"
	)

	var (
		k8sVersion                    = "1.30.0"
		ctx                           context.Context
		typeNamespacedName            types.NamespacedName
		cluster                       *clusterv1.Cluster
		controllerReconciler          *OpenshiftAssistedControlPlaneReconciler
		k8sClient                     client.Client
		ctrl                          *gomock.Controller
		mockKubernetesVersionDetector *version.MockKubernetesVersionDetector
		mockUpgradeFactory            *upgrade.MockClusterUpgradeFactory
		mockUpgrader                  *upgrade.MockClusterUpgrade
		openshiftAssistedControlPlane *controlplanev1alpha2.OpenshiftAssistedControlPlane
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		k8sClient = fakeclient.NewClientBuilder().
			WithScheme(testScheme).
			WithStatusSubresource(&controlplanev1alpha2.OpenshiftAssistedControlPlane{}).Build()

		mockKubernetesVersionDetector = version.NewMockKubernetesVersionDetector(ctrl)
		mockKubernetesVersionDetector.EXPECT().GetKubernetesVersion(gomock.Any(), gomock.Any()).Return(&k8sVersion, nil).AnyTimes()

		mockUpgrader = upgrade.NewMockClusterUpgrade(ctrl)
		mockUpgradeFactory = upgrade.NewMockClusterUpgradeFactory(ctrl)

		typeNamespacedName = types.NamespacedName{
			Name:      openshiftAssistedControlPlaneName,
			Namespace: namespace,
		}

		controllerReconciler = &OpenshiftAssistedControlPlaneReconciler{
			Client:             k8sClient,
			Scheme:             k8sClient.Scheme(),
			K8sVersionDetector: mockKubernetesVersionDetector,
			UpgradeFactory:     mockUpgradeFactory,
		}

		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		cluster = testutils.NewCluster(clusterName, namespace)
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

		// Create kubeconfig secret
		kubeconfigSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster-kubeconfig",
				Namespace: namespace,
			},
			Data: map[string][]byte{"value": []byte("fake-kubeconfig")},
		}
		Expect(k8sClient.Create(ctx, kubeconfigSecret)).To(Succeed())

		openshiftAssistedControlPlane = testutils.NewOpenshiftAssistedControlPlane(namespace, openshiftAssistedControlPlaneName)
		openshiftAssistedControlPlane.SetOwnerReferences([]metav1.OwnerReference{
			*metav1.NewControllerRef(cluster, clusterv1.GroupVersion.WithKind(clusterv1.ClusterKind)),
		})
		openshiftAssistedControlPlane.Spec.DistributionVersion = desiredVersion
		conditions.MarkTrue(openshiftAssistedControlPlane, controlplanev1alpha2.KubeconfigAvailableCondition)
		conditions.MarkTrue(openshiftAssistedControlPlane, controlplanev1alpha2.ControlPlaneReadyCondition)
		Expect(k8sClient.Create(ctx, openshiftAssistedControlPlane)).To(Succeed())
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("should handle upgrade when no upgrade is in progress", func() {
		mockUpgradeFactory.EXPECT().NewUpgrader(gomock.Any()).Return(mockUpgrader, nil)
		mockUpgrader.EXPECT().IsUpgradeInProgress(gomock.Any()).Return(false, nil)
		mockUpgrader.EXPECT().GetCurrentVersion(gomock.Any()).Return(currentVersion, nil)
		mockUpgrader.EXPECT().IsDesiredVersionUpdated(gomock.Any(), desiredVersion).Return(false, nil)
		mockUpgrader.EXPECT().UpdateClusterVersionDesiredUpdate(gomock.Any(), desiredVersion, gomock.Any(), gomock.Any()).Return(nil)

		result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ctrlruntime.Result{
			Requeue:      true,
			RequeueAfter: 1 * time.Minute,
		}))

		err = k8sClient.Get(ctx, typeNamespacedName, openshiftAssistedControlPlane)
		Expect(err).NotTo(HaveOccurred())
		Expect(openshiftAssistedControlPlane.Status.DistributionVersion).To(Equal(currentVersion))
	})

	It("should handle upgrade in progress", func() {
		mockUpgradeFactory.EXPECT().NewUpgrader(gomock.Any()).Return(mockUpgrader, nil)
		mockUpgrader.EXPECT().IsUpgradeInProgress(gomock.Any()).Return(true, nil)
		mockUpgrader.EXPECT().GetCurrentVersion(gomock.Any()).Return(currentVersion, nil)
		mockUpgrader.EXPECT().IsDesiredVersionUpdated(gomock.Any(), desiredVersion).Return(true, nil)
		conditions.MarkFalse(openshiftAssistedControlPlane, controlplanev1alpha2.UpgradeCompletedCondition, controlplanev1alpha2.UpgradeInProgressReason, clusterv1.ConditionSeverityInfo, "upgrade in progress")

		result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ctrlruntime.Result{Requeue: true, RequeueAfter: time.Minute}))

		err = k8sClient.Get(ctx, typeNamespacedName, openshiftAssistedControlPlane)
		Expect(err).NotTo(HaveOccurred())
		condition := conditions.Get(openshiftAssistedControlPlane, controlplanev1alpha2.UpgradeCompletedCondition)
		// we expect the upgrade to be in progress
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(corev1.ConditionFalse))
		Expect(condition.Reason).To(Equal(controlplanev1alpha2.UpgradeInProgressReason))
	})

	It("should handle completed upgrade", func() {
		mockUpgradeFactory.EXPECT().NewUpgrader(gomock.Any()).Return(mockUpgrader, nil)
		mockUpgrader.EXPECT().IsUpgradeInProgress(gomock.Any()).Return(false, nil)
		mockUpgrader.EXPECT().GetCurrentVersion(gomock.Any()).Return(desiredVersion, nil)
		mockUpgrader.EXPECT().IsDesiredVersionUpdated(gomock.Any(), desiredVersion).Return(true, nil)

		// Make sure upgrade is in progress: once we reconcile it should be marked as complete
		conditions.MarkFalse(
			openshiftAssistedControlPlane,
			controlplanev1alpha2.UpgradeCompletedCondition,
			controlplanev1alpha2.UpgradeInProgressReason,
			clusterv1.ConditionSeverityWarning,
			"upgrade in progress",
		)
		Expect(k8sClient.Status().Update(ctx, openshiftAssistedControlPlane)).To(Succeed())

		result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ctrlruntime.Result{}))

		err = k8sClient.Get(ctx, typeNamespacedName, openshiftAssistedControlPlane)
		Expect(err).NotTo(HaveOccurred())
		Expect(openshiftAssistedControlPlane.Status.DistributionVersion).To(Equal(desiredVersion))

		condition := conditions.Get(openshiftAssistedControlPlane, controlplanev1alpha2.UpgradeCompletedCondition)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(corev1.ConditionTrue))
	})

	It("should handle upgrade errors", func() {
		expectedError := fmt.Errorf("upgrade failed")
		mockUpgradeFactory.EXPECT().NewUpgrader(gomock.Any()).Return(mockUpgrader, nil)
		mockUpgrader.EXPECT().IsUpgradeInProgress(gomock.Any()).Return(false, nil)
		mockUpgrader.EXPECT().GetCurrentVersion(gomock.Any()).Return(currentVersion, nil)
		mockUpgrader.EXPECT().IsDesiredVersionUpdated(gomock.Any(), desiredVersion).Return(false, nil)
		mockUpgrader.EXPECT().UpdateClusterVersionDesiredUpdate(gomock.Any(), desiredVersion, gomock.Any(), gomock.Any()).Return(expectedError)

		// Make sure upgrade is in progress(not completed): even if we get no current version, now the upgrade is over
		conditions.MarkFalse(
			openshiftAssistedControlPlane,
			controlplanev1alpha2.UpgradeCompletedCondition,
			controlplanev1alpha2.UpgradeInProgressReason,
			clusterv1.ConditionSeverityWarning,
			"upgrade in progress",
		)

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).To(HaveOccurred())
		Expect(err).To(Equal(expectedError))

		// Upgrade is in progres: spec.distributionVersion is 4.15 and status.distributionVersion is 4.14
		condition := conditions.Get(openshiftAssistedControlPlane, controlplanev1alpha2.UpgradeCompletedCondition)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(corev1.ConditionFalse))
	})

	It("should handle errors getting current version", func() {
		expectedError := fmt.Errorf("failed to get version")
		mockUpgradeFactory.EXPECT().NewUpgrader(gomock.Any()).Return(mockUpgrader, nil)
		mockUpgrader.EXPECT().IsUpgradeInProgress(gomock.Any()).Return(false, nil)
		mockUpgrader.EXPECT().GetCurrentVersion(gomock.Any()).Return("", expectedError)
		mockUpgrader.EXPECT().IsDesiredVersionUpdated(gomock.Any(), desiredVersion).Return(true, nil)
		mockUpgrader.EXPECT().UpdateClusterVersionDesiredUpdate(ctx, desiredVersion, gomock.Any(), gomock.Any()).Return(nil)

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())

		err = k8sClient.Get(ctx, typeNamespacedName, openshiftAssistedControlPlane)
		Expect(err).NotTo(HaveOccurred())
		Expect(openshiftAssistedControlPlane.Status.DistributionVersion).To(Equal(""))

		// upgrade should start now
		condition := conditions.Get(openshiftAssistedControlPlane, controlplanev1alpha2.UpgradeCompletedCondition)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(corev1.ConditionFalse))
		Expect(condition.Reason).To(Equal(controlplanev1alpha2.UpgradeInProgressReason))
		Expect(condition.Message).To(Equal("upgrade to version 4.15.0 in progress"))
	})

	It("should handle upgrade with repository override", func() {
		repoOverride := "custom.registry.io/openshift-release-dev/ocp-release"
		openshiftAssistedControlPlane.Annotations = map[string]string{
			release.ReleaseImageRepositoryOverrideAnnotation: repoOverride,
		}
		Expect(k8sClient.Update(ctx, openshiftAssistedControlPlane)).To(Succeed())

		mockUpgradeFactory.EXPECT().NewUpgrader(gomock.Any()).Return(mockUpgrader, nil)
		mockUpgrader.EXPECT().IsUpgradeInProgress(gomock.Any()).Return(false, nil)
		mockUpgrader.EXPECT().GetCurrentVersion(gomock.Any()).Return(currentVersion, nil)
		mockUpgrader.EXPECT().IsDesiredVersionUpdated(gomock.Any(), desiredVersion).Return(false, nil)
		expectedParams := []upgrade.ClusterUpgradeOption{
			{Name: upgrade.ReleaseImagePullSecretOption, Value: "{\"auths\":{\"fake-pull-secret\":{\"auth\":\"cGxhY2Vob2xkZXI6c2VjcmV0Cg==\"}}}"},
			{Name: upgrade.ReleaseImageRepositoryOverrideOption, Value: repoOverride},
		}
		mockUpgrader.EXPECT().UpdateClusterVersionDesiredUpdate(
			gomock.Any(),
			desiredVersion,
			gomock.Any(),
			gomock.Eq(expectedParams),
		).Return(nil)

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())

		err = k8sClient.Get(ctx, typeNamespacedName, openshiftAssistedControlPlane)
		Expect(err).NotTo(HaveOccurred())
		condition := conditions.Get(openshiftAssistedControlPlane, controlplanev1alpha2.UpgradeCompletedCondition)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(corev1.ConditionFalse))
	})
})

var _ = Describe("Scale operations and machine updates", func() {
	const (
		openshiftAssistedControlPlaneName = "test-resource"
		clusterName                       = "test-cluster"
		namespace                         = "test"
	)

	var (
		ctx                           context.Context
		typeNamespacedName            types.NamespacedName
		cluster                       *clusterv1.Cluster
		controllerReconciler          *OpenshiftAssistedControlPlaneReconciler
		k8sClient                     client.Client
		ctrl                          *gomock.Controller
		mockKubernetesVersionDetector *version.MockKubernetesVersionDetector
		oacp                          *controlplanev1alpha2.OpenshiftAssistedControlPlane
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		k8sClient = fakeclient.NewClientBuilder().
			WithScheme(testScheme).
			WithStatusSubresource(&clusterv1.Cluster{}, &controlplanev1alpha2.OpenshiftAssistedControlPlane{}, &clusterv1.Machine{}).
			Build()

		mockKubernetesVersionDetector = version.NewMockKubernetesVersionDetector(ctrl)
		k8sVersion := "1.30.0"
		mockKubernetesVersionDetector.EXPECT().GetKubernetesVersion(gomock.Any(), gomock.Any()).Return(&k8sVersion, nil).AnyTimes()

		machineTemplate := getMachineTemplate("infratemplate", namespace)
		Expect(k8sClient.Create(ctx, &machineTemplate)).To(Succeed())

		typeNamespacedName = types.NamespacedName{
			Name:      openshiftAssistedControlPlaneName,
			Namespace: namespace,
		}

		controllerReconciler = &OpenshiftAssistedControlPlaneReconciler{
			Client:             k8sClient,
			Scheme:             k8sClient.Scheme(),
			K8sVersionDetector: mockKubernetesVersionDetector,
		}

		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		cluster = testutils.NewCluster(clusterName, namespace)
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

		oacp = testutils.NewOpenshiftAssistedControlPlane(namespace, openshiftAssistedControlPlaneName)
		oacp.SetOwnerReferences([]metav1.OwnerReference{
			*metav1.NewControllerRef(cluster, clusterv1.GroupVersion.WithKind(clusterv1.ClusterKind)),
		})
		oacp.Spec.MachineTemplate.InfrastructureRef = corev1.ObjectReference{
			Kind:       "Metal3MachineTemplate",
			Namespace:  namespace,
			Name:       "infratemplate",
			APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("Scale up operations", func() {
		It("should create a new machine when scaling up", func() {
			expectedMachineNumber := 3

			oacp.Spec.Replicas = int32(expectedMachineNumber)
			Expect(k8sClient.Create(ctx, oacp)).To(Succeed())

			// Each reconcile will only scale up one machine
			for i := 0; i < expectedMachineNumber; i++ {
				result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(ctrlruntime.Result{}))
			}

			// the fourth reconcile should have no effect on scaling up machines
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrlruntime.Result{}))

			machineList := &clusterv1.MachineList{}
			Expect(k8sClient.List(ctx, machineList, client.InNamespace(namespace))).To(Succeed())
			Expect(machineList.Items).To(HaveLen(expectedMachineNumber))

			// Verify machine properties
			for _, machine := range machineList.Items {
				Expect(machine.Labels).To(HaveKeyWithValue(clusterv1.ClusterNameLabel, clusterName))
				Expect(machine.Labels).To(HaveKeyWithValue(clusterv1.MachineControlPlaneLabel, ""))
				Expect(machine.Annotations).To(HaveKeyWithValue("bmac.agent-install.openshift.io/role", "master"))
			}
		})

		It("should distribute machines across failure domains", func() {
			expectedMachineNumber := 3

			oacp.Spec.Replicas = int32(expectedMachineNumber)
			Expect(k8sClient.Create(ctx, oacp)).To(Succeed())

			cluster.Status.FailureDomains = map[string]clusterv1.FailureDomainSpec{
				"zone-1": {ControlPlane: true},
				"zone-2": {ControlPlane: true},
				"zone-3": {ControlPlane: true},
			}
			Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())

			// Each reconcile will only scale up one machine
			for i := 0; i < expectedMachineNumber; i++ {
				result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(ctrlruntime.Result{}))
			}

			machineList := &clusterv1.MachineList{}
			Expect(k8sClient.List(ctx, machineList, client.InNamespace(namespace))).To(Succeed())
			Expect(machineList.Items).To(HaveLen(3))

			// Count machines per failure domain
			fdCount := make(map[string]int)
			for _, machine := range machineList.Items {
				if machine.Spec.FailureDomain != nil {
					fdCount[*machine.Spec.FailureDomain]++
				}
			}

			// Verify distribution
			Expect(fdCount).To(HaveLen(3))
			for _, count := range fdCount {
				Expect(count).To(Equal(1))
			}
		})
	})

	Context("ReadyReplicas status", func() {
		var (
			machineList *clusterv1.MachineList
		)

		BeforeEach(func() {
			oacp.Spec.Replicas = 3
			Expect(k8sClient.Create(ctx, oacp)).To(Succeed())
			for i := 0; i < 3; i++ {
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
				Expect(err).NotTo(HaveOccurred())
			}
			machineList = &clusterv1.MachineList{}
			Expect(k8sClient.List(ctx, machineList, client.InNamespace(namespace))).To(Succeed())
		})

		It("should have ReadyReplicas count correct when all machines are ready", func() {
			for i := range machineList.Items {
				machine := &machineList.Items[i]
				conditions.MarkTrue(machine, clusterv1.ReadyCondition)
				Expect(k8sClient.Status().Update(ctx, machine)).To(Succeed())
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, typeNamespacedName, oacp)).To(Succeed())
			Expect(oacp.Status.ReadyReplicas).To(Equal(int32(3)))
		})

		It("should have ReadyReplicas count correct when no machines are ready", func() {
			for i := range machineList.Items {
				machine := &machineList.Items[i]
				conditions.MarkFalse(machine, clusterv1.ReadyCondition, "NotReady", clusterv1.ConditionSeverityError, "Machine is not ready")
				Expect(k8sClient.Status().Update(ctx, machine)).To(Succeed())
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, typeNamespacedName, oacp)).To(Succeed())
			Expect(oacp.Status.ReadyReplicas).To(Equal(int32(0)))
		})

		It("should have ReadyReplicas count correct when some machines are ready", func() {
			for i := range machineList.Items {
				machine := &machineList.Items[i]
				if i%2 == 0 {
					conditions.MarkTrue(machine, clusterv1.ReadyCondition)
				} else {
					conditions.MarkFalse(machine, clusterv1.ReadyCondition, "NotReady", clusterv1.ConditionSeverityError, "Machine is not ready")
				}
				Expect(k8sClient.Status().Update(ctx, machine)).To(Succeed())
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, typeNamespacedName, oacp)).To(Succeed())
			Expect(oacp.Status.ReadyReplicas).To(Equal(int32(2)))
		})
	})

	Context("Scale down operations", func() {
		BeforeEach(func() {
			// Ensures Cluster has Failure Domains for the initial machines
			cluster.Status.FailureDomains = map[string]clusterv1.FailureDomainSpec{
				"zone-1": {ControlPlane: true},
				"zone-2": {ControlPlane: true},
				"zone-3": {ControlPlane: true},
			}
			Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())

			// Create initial machines
			oacp.Spec.Replicas = 3
			Expect(k8sClient.Create(ctx, oacp)).To(Succeed())
			// reconcile 3 times to scale up all replicas
			for i := 0; i < 3; i++ {
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
				Expect(err).NotTo(HaveOccurred())
			}
			Expect(k8sClient.Get(ctx, typeNamespacedName, oacp)).To(Succeed())
		})

		It("should remove machines when scaling down", func() {
			// Update replicas to scale down
			oacp.Spec.Replicas = 1
			Expect(k8sClient.Update(ctx, oacp)).To(Succeed())

			// we need to reconcile 3 times: first two loops will downscale a replica, third will do nothing
			for i := 0; i < 3; i++ {
				result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(ctrlruntime.Result{}))
			}

			machineList := &clusterv1.MachineList{}
			Expect(k8sClient.List(ctx, machineList, client.InNamespace(namespace))).To(Succeed())
			Expect(machineList.Items).To(HaveLen(1))
		})

		It("should maintain failure domain balance when scaling down", func() {

			// Scale down to 2 replicas
			oacp.Spec.Replicas = 2
			Expect(k8sClient.Update(ctx, oacp)).To(Succeed())

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrlruntime.Result{}))

			machineList := &clusterv1.MachineList{}
			Expect(k8sClient.List(ctx, machineList, client.InNamespace(namespace))).To(Succeed())
			Expect(machineList.Items).To(HaveLen(2))

			// Verify remaining machines are in different failure domains
			domains := make(map[string]bool)
			for _, machine := range machineList.Items {
				Expect(machine.Spec.FailureDomain).NotTo(BeNil())
				domains[*machine.Spec.FailureDomain] = true
			}
			Expect(domains).To(HaveLen(2))
		})
	})

	Context("Machine updates", func() {
		BeforeEach(func() {
			oacp.Spec.Replicas = 3
			Expect(k8sClient.Create(ctx, oacp)).To(Succeed())
			for i := 0; i < 3; i++ {
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
				Expect(err).NotTo(HaveOccurred())
			}
		})

		It("should correctly track updated machines", func() {
			// Modify machine template
			Expect(k8sClient.Get(ctx, typeNamespacedName, oacp)).To(Succeed())
			oacp.Spec.MachineTemplate.NodeDrainTimeout = &metav1.Duration{Duration: 10 * time.Minute}
			Expect(k8sClient.Update(ctx, oacp)).To(Succeed())

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrlruntime.Result{}))

			// Verify status reflects machines needing updates
			Expect(k8sClient.Get(ctx, typeNamespacedName, oacp)).To(Succeed())
			Expect(oacp.Status.UpdatedReplicas).To(Equal(int32(0)))
			Expect(oacp.Status.Replicas).To(Equal(int32(3)))
		})

		It("should mark machines as updated when matching desired state", func() {
			machineList := &clusterv1.MachineList{}
			Expect(k8sClient.List(ctx, machineList, client.InNamespace(namespace))).To(Succeed())

			// Update all machines to match desired state
			for i := range machineList.Items {
				machine := &machineList.Items[i]
				machine.Spec.NodeDrainTimeout = oacp.Spec.MachineTemplate.NodeDrainTimeout
				Expect(k8sClient.Update(ctx, machine)).To(Succeed())
			}

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrlruntime.Result{}))

			// Verify status shows all machines updated
			Expect(k8sClient.Get(ctx, typeNamespacedName, oacp)).To(Succeed())
			Expect(oacp.Status.UpdatedReplicas).To(Equal(oacp.Status.Replicas))
		})

		It("should handle bootstrap config updates", func() {
			// Modify bootstrap config spec
			Expect(k8sClient.Get(ctx, typeNamespacedName, oacp)).To(Succeed())
			oacp.Spec.OpenshiftAssistedConfigSpec.KernelArguments = []v1beta1.KernelArgument{{
				Operation: "foobar",
				Value:     "barfoo",
			}}
			Expect(k8sClient.Update(ctx, oacp)).To(Succeed())

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrlruntime.Result{}))

			// Verify machines are marked for update
			Expect(k8sClient.Get(ctx, typeNamespacedName, oacp)).To(Succeed())
			Expect(oacp.Status.UpdatedReplicas).To(Equal(int32(0)))
		})
	})
})

// Create dummy machine template
func getMachineTemplate(name string, namespace string) metal3v1beta1.Metal3MachineTemplate {
	return metal3v1beta1.Metal3MachineTemplate{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Metal3MachineTemplate",
			APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: metal3v1beta1.Metal3MachineTemplateSpec{},
	}
}

func checkReadyConditions(expectedReadyConditions []clusterv1.ConditionType, openshiftAssistedControlPlane *controlplanev1alpha2.OpenshiftAssistedControlPlane) {
	for _, conditionType := range expectedReadyConditions {
		By(fmt.Sprintf("checking condition %s ready", conditionType))
		condition := conditions.Get(openshiftAssistedControlPlane,
			conditionType,
		)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(corev1.ConditionTrue))
	}
}
