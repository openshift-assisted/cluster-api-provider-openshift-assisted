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

	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/release"

	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/upgrade"

	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/version"

	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/auth"
	"sigs.k8s.io/cluster-api/util/conditions"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
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
		mockUpgrader.EXPECT().UpdateClusterVersionDesiredUpdate(gomock.Any(), desiredVersion, gomock.Any()).Return(nil)

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
		mockUpgrader.EXPECT().UpdateClusterVersionDesiredUpdate(gomock.Any(), desiredVersion, gomock.Any()).Return(expectedError)

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).To(HaveOccurred())
		Expect(err).To(Equal(expectedError))
	})

	It("should handle errors getting current version", func() {
		expectedError := fmt.Errorf("failed to get version")
		mockUpgradeFactory.EXPECT().NewUpgrader(gomock.Any()).Return(mockUpgrader, nil)
		mockUpgrader.EXPECT().IsUpgradeInProgress(gomock.Any()).Return(false, nil)
		mockUpgrader.EXPECT().GetCurrentVersion(gomock.Any()).Return("", expectedError)
		mockUpgrader.EXPECT().IsDesiredVersionUpdated(gomock.Any(), desiredVersion).Return(true, nil)

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())

		err = k8sClient.Get(ctx, typeNamespacedName, openshiftAssistedControlPlane)
		Expect(err).NotTo(HaveOccurred())
		Expect(openshiftAssistedControlPlane.Status.DistributionVersion).To(Equal(""))
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
			gomock.Eq(expectedParams),
		).Return(nil)

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())
	})
})

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
