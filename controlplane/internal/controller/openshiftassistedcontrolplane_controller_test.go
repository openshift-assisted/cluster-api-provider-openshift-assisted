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

	controlplanev1alpha2 "github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1alpha2"
	testutils "github.com/openshift-assisted/cluster-api-agent/test/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

const exampleDomain = "example.com"

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
				WithIndex(
					&clusterv1.MachineDeployment{},
					clusterNameField,
					filterClusterName,
				).
				WithIndex(
					&clusterv1.MachineSet{},
					clusterNameField,
					filterClusterName,
				).
				WithStatusSubresource(&controlplanev1alpha2.OpenshiftAssistedControlPlane{}).Build()

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
				openshiftAssistedControlPlane.Spec.Config.BaseDomain = exampleDomain
				openshiftAssistedControlPlane.SetOwnerReferences(
					[]metav1.OwnerReference{
						*metav1.NewControllerRef(cluster, clusterv1.GroupVersion.WithKind(clusterv1.ClusterKind)),
					},
				)
				Expect(k8sClient.Create(ctx, openshiftAssistedControlPlane)).To(Succeed())

				mdVersion := "v4.16.0"
				md := clusterv1.MachineDeployment{
					TypeMeta: metav1.TypeMeta{
						Kind: "MachineDeployment",
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "mynamespace",
						Name:      "my-machine-deployment",
					},
					Spec: clusterv1.MachineDeploymentSpec{
						ClusterName: "test-cluster",
						Template: clusterv1.MachineTemplateSpec{
							Spec: clusterv1.MachineSpec{
								Version: &mdVersion,
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, &md)).To(Succeed())

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

				Expect(k8sClient.Get(ctx, typeNamespacedName, openshiftAssistedControlPlane)).To(Succeed())
				condition := conditions.Get(openshiftAssistedControlPlane,
					controlplanev1alpha2.MachinesCreatedCondition,
				)
				Expect(condition).To(BeNil())
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
			Expect(openshiftAssistedControlPlane.Finalizers).To(ContainElement(oacpFinalizer))
		})
		When("an invalid version is set on the OpenshiftAssistedControlPlane", func() {
			It("should return error", func() {
				By("setting the cluster as the owner ref on the OpenshiftAssistedControlPlane")
				openshiftAssistedControlPlane := getOpenshiftAssistedControlPlane()
				openshiftAssistedControlPlane.Spec.Version = "4.12.0"
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
		When("a cluster owns this OpenshiftAssistedControlPlane and MachineDeployment with different version", func() {
			It("should fail because of the different versions", func() {
				By("setting the cluster as the owner ref on the OpenshiftAssistedControlPlane")

				openshiftAssistedControlPlane := getOpenshiftAssistedControlPlane()
				openshiftAssistedControlPlane.Spec.Config.ClusterName = "test-cluster"
				openshiftAssistedControlPlane.Spec.Config.PullSecretRef = &corev1.LocalObjectReference{
					Name: "my-pullsecret",
				}
				openshiftAssistedControlPlane.Spec.Config.BaseDomain = exampleDomain
				openshiftAssistedControlPlane.SetOwnerReferences(
					[]metav1.OwnerReference{
						*metav1.NewControllerRef(cluster, clusterv1.GroupVersion.WithKind(clusterv1.ClusterKind)),
					},
				)
				Expect(k8sClient.Create(ctx, openshiftAssistedControlPlane)).To(Succeed())

				mdVersion := "4.17.0"
				md := clusterv1.MachineDeployment{
					TypeMeta: metav1.TypeMeta{
						Kind: "MachineDeployment",
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "mynamespace",
						Name:      "my-machine-deployment",
					},
					Spec: clusterv1.MachineDeploymentSpec{
						ClusterName: "test-cluster",
						Template: clusterv1.MachineTemplateSpec{
							Spec: clusterv1.MachineSpec{
								Version: &mdVersion,
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, &md)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				expectedErrorMessage := "controlplane and workers should be all on desired version 4.16.0. The following resources are not on the same version: MachineDeployment mynamespace/my-machine-deployment (4.17.0)"
				Expect(err).To(MatchError(expectedErrorMessage))

				Expect(k8sClient.Get(ctx, typeNamespacedName, openshiftAssistedControlPlane)).To(Succeed())
				condition := conditions.Get(openshiftAssistedControlPlane,
					controlplanev1alpha2.MachinesCreatedCondition,
				)
				Expect(condition).NotTo(BeNil())
				Expect(condition.Reason).To(Equal(controlplanev1alpha2.VersionCheckFailedReason))
				Expect(condition.Message).To(Equal(expectedErrorMessage))
			})
		})
		When("a cluster owns this OpenshiftAssistedControlPlane, MachineDeployment with same version but MachineSet with a different one", func() {
			It("should fail because of the different versions", func() {
				By("setting the cluster as the owner ref on the OpenshiftAssistedControlPlane")

				openshiftAssistedControlPlane := getOpenshiftAssistedControlPlane()
				openshiftAssistedControlPlane.Spec.Config.ClusterName = "test-cluster"
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

				mdVersion := "4.16.0"
				md := clusterv1.MachineDeployment{
					TypeMeta: metav1.TypeMeta{
						Kind: "MachineDeployment",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-machine-deployment",
					},
					Spec: clusterv1.MachineDeploymentSpec{
						ClusterName: "test-cluster",
						Template: clusterv1.MachineTemplateSpec{
							Spec: clusterv1.MachineSpec{
								Version: &mdVersion,
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, &md)).To(Succeed())

				msVersion := "4.17.0"
				ms := clusterv1.MachineSet{
					TypeMeta: metav1.TypeMeta{
						Kind: "MachineSet",
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "mynamespace",
						Name:      "my-machine-set",
					},
					Spec: clusterv1.MachineSetSpec{
						ClusterName: "test-cluster",
						Template: clusterv1.MachineTemplateSpec{
							Spec: clusterv1.MachineSpec{
								Version: &msVersion,
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, &ms)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				expectedErrorMessage := "controlplane and workers should be all on desired version 4.16.0. The following resources are not on the same version: MachineSet mynamespace/my-machine-set (4.17.0)"
				Expect(err).To(MatchError(expectedErrorMessage))

				Expect(k8sClient.Get(ctx, typeNamespacedName, openshiftAssistedControlPlane)).To(Succeed())
				condition := conditions.Get(openshiftAssistedControlPlane,
					controlplanev1alpha2.MachinesCreatedCondition,
				)
				Expect(condition).NotTo(BeNil())
				Expect(condition.Reason).To(Equal(controlplanev1alpha2.VersionCheckFailedReason))
				Expect(condition.Message).To(Equal(expectedErrorMessage))
			})
		})
	})
})

func getOpenshiftAssistedControlPlane() *controlplanev1alpha2.OpenshiftAssistedControlPlane {
	return &controlplanev1alpha2.OpenshiftAssistedControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      openshiftAssistedControlPlaneName,
			Namespace: namespace,
		},
		Spec: controlplanev1alpha2.OpenshiftAssistedControlPlaneSpec{
			Version: "4.16.0",
		},
	}
}
