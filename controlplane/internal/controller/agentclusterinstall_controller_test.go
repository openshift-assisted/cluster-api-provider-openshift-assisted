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
	controlplanev1alpha1 "github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1alpha1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aimodels "github.com/openshift/assisted-service/models"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("AgentClusterInstall Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			openshiftAssistedControlPlaneName = "test-controlplane"
			clusterName                       = "test-cluster"
			namespace                         = "test-namespace"
			agentClusterInstallName           = "test-aci"
			adminKubeconfigSecret             = "test-admin-kubeconfig"
			kubeconfigSecret                  = clusterName + "-kubeconfig"
		)
		var (
			ctx                           = context.Background()
			aciNamespacedName             types.NamespacedName
			openshiftAssistedControlPlane *controlplanev1alpha1.OpenshiftAssistedControlPlane
			aci                           *hiveext.AgentClusterInstall
			reconciler                    *AgentClusterInstallReconciler
			mockCtrl                      *gomock.Controller
			k8sClient                     client.Client
		)

		BeforeEach(func() {
			mockCtrl = gomock.NewController(GinkgoT())
			k8sClient = fakeclient.NewClientBuilder().
				WithScheme(testScheme).
				WithStatusSubresource(&controlplanev1alpha1.OpenshiftAssistedControlPlane{}, &hiveext.AgentClusterInstall{}).
				Build()
			Expect(k8sClient).NotTo(BeNil())
			aciNamespacedName = types.NamespacedName{
				Name:      agentClusterInstallName,
				Namespace: namespace,
			}

			reconciler = &AgentClusterInstallReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			openshiftAssistedControlPlane = &controlplanev1alpha1.OpenshiftAssistedControlPlane{
				TypeMeta: metav1.TypeMeta{
					Kind:       "OpenshiftAssistedControlPlane",
					APIVersion: controlplanev1alpha1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      openshiftAssistedControlPlaneName,
					Namespace: namespace,
					Labels: map[string]string{
						clusterv1.ClusterNameLabel: clusterName,
					},
				},
			}
			Expect(k8sClient.Create(ctx, openshiftAssistedControlPlane)).To(Succeed())

			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			aci = generateAgentClusterInstall(agentClusterInstallName, namespace)
			Expect(k8sClient.Create(ctx, aci)).To(Succeed())
		})

		AfterEach(func() {
			mockCtrl.Finish()
		})

		It("should return an error when the AgentClusterInstall doesn't have an OpenshiftAssistedControlPlane owner", func() {
			By("Reconciling the AgentClusterInstall")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: aciNamespacedName,
			})
			Expect(err).To(HaveOccurred())
		})

		It("should reconcile successfully when the AgentClusterInstall has an OpenshiftAssistedControlPlane owner", func() {
			By("Updating the AgentClusterInstall to have an OpenshiftAssistedControlPlane owner")
			Expect(controllerutil.SetOwnerReference(openshiftAssistedControlPlane, aci, k8sClient.Scheme())).To(Succeed())
			Expect(k8sClient.Update(ctx, aci)).To(Succeed())

			By("Reconciling the AgentClusterInstall")
			res, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: aciNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(Equal(ctrl.Result{}))
		})

		It(
			"should not change the status of the OpenshiftAssistedControlPlane when the AgentClusterInstall doesn't have a kubeconfig reference",
			func() {
				By("Updating the AgentClusterInstall's status")
				Expect(controllerutil.SetOwnerReference(openshiftAssistedControlPlane, aci, k8sClient.Scheme())).To(Succeed())
				Expect(k8sClient.Update(ctx, aci)).To(Succeed())
				aci.Status.DebugInfo.State = aimodels.HostStatusInstalling
				Expect(k8sClient.Status().Update(ctx, aci)).To(Succeed())

				By("Reconciling the AgentClusterInstall")
				res, err := reconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: aciNamespacedName,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(res).To(Equal(ctrl.Result{}))

				By("Checking that the OpenshiftAssistedControlPlane status was not changed")
				acp := &controlplanev1alpha1.OpenshiftAssistedControlPlane{}
				Expect(
					k8sClient.Get(ctx, types.NamespacedName{Name: openshiftAssistedControlPlaneName, Namespace: namespace}, acp),
				).To(Succeed())
				Expect(acp.Status.Initialized).NotTo(BeTrue())
				Expect(acp.Status.Ready).NotTo(BeTrue())
			},
		)

		It("should fail to reconcile when the kubeconfig reference is set but the secret doesn't exist", func() {
			By("Updating the AgentClusterInstall's kubeconfig reference")
			Expect(controllerutil.SetOwnerReference(openshiftAssistedControlPlane, aci, k8sClient.Scheme())).To(Succeed())
			aci.Spec.ClusterMetadata = &hivev1.ClusterMetadata{
				AdminKubeconfigSecretRef: corev1.LocalObjectReference{
					Name: adminKubeconfigSecret,
				},
			}
			Expect(k8sClient.Update(ctx, aci)).To(Succeed())

			By("Reconciling the AgentClusterInstall")
			res, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: aciNamespacedName,
			})
			Expect(err).To(HaveOccurred())
			Expect(res).To(Equal(ctrl.Result{}))

			By("Checking that the cluster Kubeconfig secret didn't get created")
			kubeconfig := &corev1.Secret{}
			Expect(
				k8sClient.Get(ctx, types.NamespacedName{Name: kubeconfigSecret, Namespace: namespace}, kubeconfig),
			).NotTo(Succeed())

			By("Checking that the OpenshiftAssistedControlPlane status was not changed")
			acp := &controlplanev1alpha1.OpenshiftAssistedControlPlane{}
			Expect(
				k8sClient.Get(ctx, types.NamespacedName{Name: openshiftAssistedControlPlaneName, Namespace: namespace}, acp),
			).To(Succeed())
			Expect(acp.Status.Initialized).NotTo(BeTrue())
			Expect(acp.Status.Ready).NotTo(BeTrue())
		})

		It("should successfully reconcile when the kubeconfig reference is set", func() {
			By("Updating the AgentClusterInstall's kubeconfig reference")
			Expect(controllerutil.SetOwnerReference(openshiftAssistedControlPlane, aci, k8sClient.Scheme())).To(Succeed())
			aci.Spec.ClusterMetadata = &hivev1.ClusterMetadata{
				AdminKubeconfigSecretRef: corev1.LocalObjectReference{
					Name: adminKubeconfigSecret,
				},
			}
			Expect(k8sClient.Update(ctx, aci)).To(Succeed())

			By("Creating the admin kubeconfig secret")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      adminKubeconfigSecret,
					Namespace: namespace,
				},
				Data: map[string][]byte{
					"kubeconfig": []byte("test-kubeconfig-data"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			By("Reconciling the AgentClusterInstall")
			res, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: aciNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(Equal(ctrl.Result{}))

			By("Checking that the cluster Kubeconfig secret was created")
			kubeconfig := &corev1.Secret{}
			Expect(
				k8sClient.Get(ctx, types.NamespacedName{Name: kubeconfigSecret, Namespace: namespace}, kubeconfig),
			).To(Succeed())

			By("Ensuring the cluster label exists on both kubeconfig secrets")
			updatedSecret := &corev1.Secret{}
			Expect(
				k8sClient.Get(
					ctx,
					types.NamespacedName{Name: adminKubeconfigSecret, Namespace: namespace},
					updatedSecret,
				),
			).To(Succeed())

			Expect(updatedSecret.Labels).To(HaveKeyWithValue(clusterv1.ClusterNameLabel, clusterName))
			Expect(kubeconfig.Labels).To(HaveKeyWithValue(clusterv1.ClusterNameLabel, clusterName))

			By("Checking that the OpenshiftAssistedControlPlane status is correct")
			acp := &controlplanev1alpha1.OpenshiftAssistedControlPlane{}
			Expect(
				k8sClient.Get(ctx, types.NamespacedName{Name: openshiftAssistedControlPlaneName, Namespace: namespace}, acp),
			).To(Succeed())
			Expect(acp.Status.Initialized).To(BeTrue())
			Expect(acp.Status.Ready).NotTo(BeTrue())
		})

		It("should set the OpenshiftAssistedControlPlane to ready when the AgentClusterInstall has finished installing", func() {
			By("Updating the AgentClusterInstall's kubeconfig reference")
			Expect(controllerutil.SetOwnerReference(openshiftAssistedControlPlane, aci, k8sClient.Scheme())).To(Succeed())
			aci.Spec.ClusterMetadata = &hivev1.ClusterMetadata{
				AdminKubeconfigSecretRef: corev1.LocalObjectReference{
					Name: adminKubeconfigSecret,
				},
			}
			Expect(k8sClient.Update(ctx, aci)).To(Succeed())

			By("Creating the admin kubeconfig secret")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      adminKubeconfigSecret,
					Namespace: namespace,
				},
				Data: map[string][]byte{
					"kubeconfig": []byte("test-kubeconfig-data"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			By("Updating the AgentClusterInstall's status")
			aci.Status.DebugInfo.State = aimodels.ClusterStatusAddingHosts
			Expect(k8sClient.Status().Update(ctx, aci)).To(Succeed())

			By("Reconciling the AgentClusterInstall")
			res, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: aciNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(Equal(ctrl.Result{}))

			By("Checking that the OpenshiftAssistedControlPlane status is correct")
			acp := &controlplanev1alpha1.OpenshiftAssistedControlPlane{}
			Expect(
				k8sClient.Get(ctx, types.NamespacedName{Name: openshiftAssistedControlPlaneName, Namespace: namespace}, acp),
			).To(Succeed())
			Expect(acp.Status.Initialized).To(BeTrue())
			Expect(acp.Status.Ready).To(BeTrue())
		})
	})
})

func generateAgentClusterInstall(name, namespace string) *hiveext.AgentClusterInstall {
	aci := &hiveext.AgentClusterInstall{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: hiveext.AgentClusterInstallSpec{},
	}
	return aci
}
