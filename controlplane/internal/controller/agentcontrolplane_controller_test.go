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

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	hivev1 "github.com/openshift/hive/apis/hive/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	controlplanev1beta1 "github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

var _ = Describe("AgentControlPlane Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			agentControlPlaneName = "test-resource"
			clusterName           = "test-cluster"
			namespace             = "test"
		)
		var (
			ctx                  = context.Background()
			typeNamespacedName   types.NamespacedName
			agentcontrolplane    *controlplanev1beta1.AgentControlPlane
			cluster              *clusterv1.Cluster
			controllerReconciler *AgentControlPlaneReconciler
			mockCtrl             *gomock.Controller
		)

		BeforeEach(func() {
			mockCtrl = gomock.NewController(GinkgoT())
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			k8sClient.Create(ctx, ns)
			typeNamespacedName = types.NamespacedName{
				Name:      agentControlPlaneName,
				Namespace: namespace,
			}

			controllerReconciler = &AgentControlPlaneReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("creating the custom resource for the Kind AgentControlPlane")
			agentcontrolplane = &controlplanev1beta1.AgentControlPlane{}
			err := k8sClient.Get(ctx, typeNamespacedName, agentcontrolplane)
			if err != nil && errors.IsNotFound(err) {
				resource := &controlplanev1beta1.AgentControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      agentControlPlaneName,
						Namespace: namespace,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}

			By("creating a cluster")
			cluster = &clusterv1.Cluster{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace}, cluster)
			if err != nil && errors.IsNotFound(err) {
				cluster = createCluster(clusterName, namespace)
				err := k8sClient.Create(ctx, cluster)
				Expect(err).NotTo(HaveOccurred())
			}
		})

		AfterEach(func() {
			mockCtrl.Finish()
		})

		It("should successfully reconcile the resource", func() {
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should successfully create a cluster deployment when a cluster owns this agent control plane", func() {
			By("setting the cluster as the owner ref on the agent control plane")
			agentControlPlane := &controlplanev1beta1.AgentControlPlane{}
			err := k8sClient.Get(ctx, typeNamespacedName, agentControlPlane)
			Expect(err).NotTo(HaveOccurred())

			agentControlPlane.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(cluster, clusterv1.GroupVersion.WithKind(clusterv1.ClusterKind))})
			Expect(k8sClient.Update(ctx, agentControlPlane)).To(Succeed())

			By("checking if the agent control plane created the cluster deployment after reconcile")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Get(ctx, typeNamespacedName, agentControlPlane)
			Expect(err).NotTo(HaveOccurred())
			Expect(agentControlPlane.Status.ClusterDeploymentRef).NotTo(BeNil())
			Expect(agentControlPlane.Status.ClusterDeploymentRef.Name).Should(Equal(agentControlPlane.Name))

			By("checking that the cluster deployment was created")
			cd := &hivev1.ClusterDeployment{}
			err = k8sClient.Get(ctx, typeNamespacedName, cd)
			Expect(err).NotTo(HaveOccurred())
			Expect(cd).NotTo(BeNil())
		})

		It("should add a finalizer to the agent control plane if it's not being deleted", func() {
			By("setting the owner ref on the agent control plane")
			agentControlPlane := &controlplanev1beta1.AgentControlPlane{}
			err := k8sClient.Get(ctx, typeNamespacedName, agentControlPlane)
			Expect(err).NotTo(HaveOccurred())

			agentControlPlane.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(cluster, clusterv1.GroupVersion.WithKind(clusterv1.ClusterKind))})
			Expect(k8sClient.Update(ctx, agentControlPlane)).To(Succeed())

			By("checking if the agent control plane has the finalizer")
			err = k8sClient.Get(ctx, typeNamespacedName, agentControlPlane)
			Expect(err).NotTo(HaveOccurred())
			Expect(agentControlPlane.Finalizers).NotTo(BeEmpty())
			Expect(agentControlPlane.Finalizers).To(ContainElement(acpFinalizer))
		})

		It("should successfully delete the cluster deployment and agent control plane", func() {
			By("ensuring the cluster deployment exists")
			cd := &hivev1.ClusterDeployment{}
			err := k8sClient.Get(ctx, typeNamespacedName, cd)
			Expect(err).NotTo(HaveOccurred())
			Expect(cd).NotTo(BeNil())

			By("deleting the agent control plane")
			agentControlPlane := &controlplanev1beta1.AgentControlPlane{}
			err = k8sClient.Get(ctx, typeNamespacedName, agentControlPlane)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Delete(ctx, agentControlPlane)).To(Succeed())

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).ToNot(BeNil())

			By("ensuring the cluster deployment no longer exists")
			err = k8sClient.Get(ctx, typeNamespacedName, cd)
			Expect(err).To(HaveOccurred())
			Expect(errors.IsNotFound(err)).To(BeTrue())

			By("ensuring the agent control plane is deleted")
			err = k8sClient.Get(ctx, typeNamespacedName, agentControlPlane)
			Expect(err).To(HaveOccurred())
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})
})

func createCluster(clusterName, namespace string) *clusterv1.Cluster {
	By("creating the owning cluster")
	// Create cluster and have it own this agent control plane
	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: clusterv1.ClusterSpec{
			Paused: false,
			ControlPlaneEndpoint: clusterv1.APIEndpoint{
				Host: "example.com",
				Port: 8080,
			},
		},
		Status: clusterv1.ClusterStatus{
			InfrastructureReady: true,
		},
	}
	return cluster
}
