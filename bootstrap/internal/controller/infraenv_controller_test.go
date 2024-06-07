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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	bootstrapv1alpha1 "github.com/openshift-assisted/cluster-api-agent/bootstrap/api/v1alpha1"
	testutils "github.com/openshift-assisted/cluster-api-agent/test/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("InfraEnv Controller", func() {
	Context("When reconciling a resource", func() {
		ctx := context.Background()
		var controllerReconciler *InfraEnvReconciler
		var k8sClient client.Client

		BeforeEach(func() {
			k8sClient = fakeclient.NewClientBuilder().WithScheme(testScheme).
				WithStatusSubresource(&bootstrapv1alpha1.AgentBootstrapConfig{}).
				Build()
			Expect(k8sClient).NotTo(BeNil())

			controllerReconciler = &InfraEnvReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
				},
			}
			By("creating the test namespace")
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		})

		AfterEach(func() {
			k8sClient = nil
			controllerReconciler = nil
		})
		When("No infraenv resources exist", func() {
			It("should reconcile with no errors", func() {
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      infraEnvName,
						Namespace: namespace,
					},
				})
				Expect(err).NotTo(HaveOccurred())
			})
		})
		When("Infraenv has no cluster label", func() {
			It("should reconcile with no errors", func() {
				Expect(k8sClient.Create(ctx, testutils.NewInfraEnv(namespace, infraEnvName))).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      infraEnvName,
						Namespace: namespace,
					},
				})
				Expect(err).NotTo(HaveOccurred())
			})
		})
		When("Infraenv has no ISO URL set", func() {
			It("should reconcile with no errors", func() {
				infraEnv := testutils.NewInfraEnv(namespace, infraEnvName)
				infraEnv.Labels = map[string]string{
					clusterv1.ClusterNameLabel: clusterName,
				}
				Expect(k8sClient.Create(ctx, infraEnv)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      infraEnvName,
						Namespace: namespace,
					},
				})
				Expect(err).NotTo(HaveOccurred())
			})
		})
		When("Infraenv ISO URL set, but no ABC ", func() {
			It("should reconcile with no errors", func() {
				infraEnv := testutils.NewInfraEnv(namespace, infraEnvName)
				infraEnv.Labels = map[string]string{
					clusterv1.ClusterNameLabel: clusterName,
				}
				infraEnv.Status.ISODownloadURL = "https://example.com/my-image"
				Expect(k8sClient.Create(ctx, infraEnv)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      infraEnvName,
						Namespace: namespace,
					},
				})
				Expect(err).NotTo(HaveOccurred())
			})
		})
		When("Infraenv ISO URL set, and there is a referenced ABC ", func() {
			It("should update ABC status with the URL", func() {
				infraEnv := testutils.NewInfraEnv(namespace, infraEnvName)
				infraEnv.Labels = map[string]string{
					clusterv1.ClusterNameLabel: clusterName,
				}
				infraEnv.Status.ISODownloadURL = "https://example.com/my-image"
				Expect(k8sClient.Create(ctx, infraEnv)).To(Succeed())
				abc := NewAgentBootstrapConfig(namespace, abcName, clusterName)
				abc.Status.InfraEnvRef = &corev1.ObjectReference{
					Name: infraEnv.Name,
				}
				Expect(k8sClient.Create(ctx, abc)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      infraEnvName,
						Namespace: namespace,
					},
				})
				Expect(err).NotTo(HaveOccurred())

				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(abc), abc)).To(Succeed())
				Expect(abc.Status.ISODownloadURL).To(Equal(infraEnv.Status.ISODownloadURL))
			})
		})
	})
})
