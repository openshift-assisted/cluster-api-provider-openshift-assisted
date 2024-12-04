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

	"sigs.k8s.io/cluster-api/util/conditions"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	controlplanev1alpha1 "github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1alpha1"
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
			ctx                  = context.Background()
			typeNamespacedName   types.NamespacedName
			cluster              *clusterv1.Cluster
			controllerReconciler *OpenshiftAssistedControlPlaneReconciler
			mockCtrl             *gomock.Controller
			k8sClient            client.Client
		)

		BeforeEach(func() {
			k8sClient = fakeclient.NewClientBuilder().
				WithScheme(testScheme).
				WithStatusSubresource(&controlplanev1alpha1.OpenshiftAssistedControlPlane{}).Build()

			mockCtrl = gomock.NewController(GinkgoT())
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())
			typeNamespacedName = types.NamespacedName{
				Name:      openshiftAssistedControlPlaneName,
				Namespace: namespace,
			}

			controllerReconciler = &OpenshiftAssistedControlPlaneReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
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
			mockCtrl.Finish()
			k8sClient = nil
		})

		When("when a cluster owns this OpenshiftAssistedControlPlane", func() {
			It("should successfully create a cluster deployment", func() {
				By("setting the cluster as the owner ref on the OACP")

				openshiftAssistedControlPlane := getOpenshiftAssistedControlPlane()
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
				Expect(openshiftAssistedControlPlane.Spec.Version).To(Equal("1.30.0"))
				Expect(openshiftAssistedControlPlane.Status.ClusterDeploymentRef).NotTo(BeNil())
				Expect(openshiftAssistedControlPlane.Status.ClusterDeploymentRef.Name).Should(Equal(openshiftAssistedControlPlane.Name))

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

				openshiftAssistedControlPlane := getOpenshiftAssistedControlPlane()
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
				openshiftAssistedControlPlane := getOpenshiftAssistedControlPlane()
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

			openshiftAssistedControlPlane := getOpenshiftAssistedControlPlane()
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
		When("an invalid version for releaseImage is set on the OpenshiftAssistedControlPlane", func() {
			It("should return error", func() {
				By("setting the cluster as the owner ref on the OpenshiftAssistedControlPlane")
				openshiftAssistedControlPlane := getOpenshiftAssistedControlPlane()
				openshiftAssistedControlPlane.Spec.Config.ReleaseImage = "quay.io/openshift-release-dev/ocp-release:4.12.0-multi"
				// set version to expected version to avoid reconciliation on version conversion
				openshiftAssistedControlPlane.Spec.Version = "1.25.0"
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
					controlplanev1alpha1.MachinesCreatedCondition,
				)
				Expect(condition).NotTo(BeNil())
				Expect(condition.Message).To(Equal("version 1.25.0 is not supported, the minimum supported version is 1.27.0"))
			})
		})
		When("release image does not match expected spec version", func() {
			It("should reconcile version field", func() {
				By("setting the cluster as the owner ref on the OpenshiftAssistedControlPlane")
				openshiftAssistedControlPlane := getOpenshiftAssistedControlPlane()
				// set version to wrong version to expect reconciliation
				openshiftAssistedControlPlane.Spec.Version = "1.10.0"
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

				Expect(openshiftAssistedControlPlane.Spec.Version).To(Equal("1.30.0"))
			})
		})
	})
})

func getOpenshiftAssistedControlPlane() *controlplanev1alpha1.OpenshiftAssistedControlPlane {
	return &controlplanev1alpha1.OpenshiftAssistedControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      openshiftAssistedControlPlaneName,
			Namespace: namespace,
		},
		Spec: controlplanev1alpha1.OpenshiftAssistedControlPlaneSpec{
			Config: controlplanev1alpha1.OpenshiftAssistedControlPlaneConfigSpec{
				ReleaseImage: "quay.io/openshift-release-dev/ocp-release:4.17.0-rc.2-x86_64",
			},
			Version: "1.30.0",
		},
	}
}
