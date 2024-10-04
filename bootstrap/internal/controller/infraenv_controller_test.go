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

const (
	DownloadURL = "https://example.com/my-image"
)

var _ = Describe("InfraEnv Controller", func() {
	Context("When reconciling a resource", func() {
		ctx := context.Background()
		var controllerReconciler *InfraEnvReconciler
		var k8sClient client.Client

		BeforeEach(func() {

			k8sClient = fakeclient.NewClientBuilder().WithScheme(testScheme).
				WithStatusSubresource(&bootstrapv1alpha1.OpenshiftAssistedConfig{}).
				WithIndex(
					&bootstrapv1alpha1.OpenshiftAssistedConfig{},
					oacInfraEnvRefFieldName,
					filterRefName,
				).
				WithIndex(
					&bootstrapv1alpha1.OpenshiftAssistedConfig{},
					oacInfraEnvRefFieldNamespace,
					filterRefNamespace,
				).
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
		When("Infraenv ISO URL set, but no OpenshiftAssistedConfigs reference it", func() {
			It("should reconcile with no errors", func() {
				By("creating the InfraEnv and OpenshiftAssistedConfig")
				infraEnv := testutils.NewInfraEnv(namespace, infraEnvName)
				infraEnv.Labels = map[string]string{
					clusterv1.ClusterNameLabel: clusterName,
				}
				infraEnv.Status.ISODownloadURL = DownloadURL
				Expect(k8sClient.Create(ctx, infraEnv)).To(Succeed())
				oac := NewOpenshiftAssistedConfig(namespace, oacName, clusterName)
				Expect(k8sClient.Create(ctx, oac)).To(Succeed())

				By("reconciling the InfraEnv")
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      infraEnvName,
						Namespace: namespace,
					},
				})
				Expect(err).NotTo(HaveOccurred())

				By("checking that the OpenshiftAssistedConfig does not have its ISO URL set")
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(oac), oac)).To(Succeed())
				Expect(oac.Status.ISODownloadURL).To(BeEmpty())
			})
		})
		When("Infraenv ISO URL set, and there is a referenced OAC ", func() {
			It("should update OAC status with the URL", func() {
				infraEnv := testutils.NewInfraEnv(namespace, infraEnvName)
				infraEnv.Labels = map[string]string{
					clusterv1.ClusterNameLabel: clusterName,
				}
				infraEnv.Status.ISODownloadURL = DownloadURL
				Expect(k8sClient.Create(ctx, infraEnv)).To(Succeed())

				oac := NewOpenshiftAssistedConfigWithInfraEnv(namespace, oacName, clusterName, infraEnv)
				Expect(k8sClient.Create(ctx, oac)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      infraEnvName,
						Namespace: namespace,
					},
				})
				Expect(err).NotTo(HaveOccurred())

				By("checking that the OpenshiftAssistedConfig does have its ISO URL set")
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(oac), oac)).To(Succeed())
				Expect(oac.Status.ISODownloadURL).To(Equal(infraEnv.Status.ISODownloadURL))
			})
		})
	})
	Context("InfraEnv Controller uses internal URL for the ISO", func() {

		const (
			assistedNamespace = "assisted-test-namespace"
		)
		var (
			ctx                  = context.Background()
			controllerReconciler *InfraEnvReconciler
			k8sClient            client.Client
			oac                  *bootstrapv1alpha1.OpenshiftAssistedConfig
		)

		BeforeEach(func() {
			k8sClient = fakeclient.NewClientBuilder().WithScheme(testScheme).
				WithStatusSubresource(&bootstrapv1alpha1.OpenshiftAssistedConfig{}).
				WithIndex(
					&bootstrapv1alpha1.OpenshiftAssistedConfig{},
					oacInfraEnvRefFieldName,
					filterRefName,
				).
				WithIndex(
					&bootstrapv1alpha1.OpenshiftAssistedConfig{},
					oacInfraEnvRefFieldNamespace,
					filterRefNamespace,
				).
				Build()
			Expect(k8sClient).NotTo(BeNil())

			controllerReconciler = &InfraEnvReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Config: InfraEnvControllerConfig{
					UseInternalImageURL:   true,
					ImageServiceNamespace: assistedNamespace,
					ImageServiceName:      "assisted-image-service",
				},
			}

			By("creating the test namespaces")
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
				},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())
			assistedNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: assistedNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, assistedNS)).To(Succeed())

			By("creating the InfraEnv and OpenshiftAssistedConfig")
			infraEnv := testutils.NewInfraEnv(namespace, infraEnvName)
			infraEnv.Labels = map[string]string{
				clusterv1.ClusterNameLabel: clusterName,
			}
			infraEnv.Status.ISODownloadURL = DownloadURL
			Expect(k8sClient.Create(ctx, infraEnv)).To(Succeed())
			oac = NewOpenshiftAssistedConfigWithInfraEnv(namespace, oacName, clusterName, infraEnv)
			Expect(k8sClient.Create(ctx, oac)).To(Succeed())
		})

		AfterEach(func() {
			k8sClient = nil
			controllerReconciler = nil
		})

		When("The image service service doesn't exists", func() {
			It("should return an error and OAC status should be empty", func() {
				By("reconciling the InfraEnv")
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      infraEnvName,
						Namespace: namespace,
					},
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("failed to find assisted image service service: services \"assisted-image-service\" not found"))

				By("checking that the OpenshiftAssistedConfig does not have its ISO URL set")
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(oac), oac)).To(Succeed())
				Expect(oac.Status.ISODownloadURL).To(BeEmpty())
			})
		})

		When("The image service service doesn't have a cluster IP", func() {
			It("should return an error and OAC status should be empty", func() {
				By("creating the image service service")
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "assisted-image-service",
						Namespace: assistedNamespace,
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Port: 8080,
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, svc)).To(Succeed())

				By("reconciling the InfraEnv")
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      infraEnvName,
						Namespace: namespace,
					},
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("failed to get internal image service URL, either cluster IP or Ports were missing from Service"))

				By("checking that the OpenshiftAssistedConfig does not have its ISO URL set")
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(oac), oac)).To(Succeed())
				Expect(oac.Status.ISODownloadURL).To(BeEmpty())
			})
		})

		When("The image service service doesn't have any Ports", func() {
			It("should return an error and OAC status should be empty", func() {
				By("creating the image service service")
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "assisted-image-service",
						Namespace: assistedNamespace,
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "172.0.0.1",
					},
				}
				Expect(k8sClient.Create(ctx, svc)).To(Succeed())

				By("reconciling the InfraEnv")
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      infraEnvName,
						Namespace: namespace,
					},
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("failed to get internal image service URL, either cluster IP or Ports were missing from Service"))

				By("checking that the OpenshiftAssistedConfig does not have its ISO URL set")
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(oac), oac)).To(Succeed())
				Expect(oac.Status.ISODownloadURL).To(BeEmpty())
			})
		})

		When("The image service service exists", func() {
			It("should update OAC status with the internal URL instead of the InfraEnv's URL", func() {
				By("creating the image service service")
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "assisted-image-service",
						Namespace: assistedNamespace,
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "172.0.0.1",
						Ports: []corev1.ServicePort{
							{
								Port: 8080,
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, svc)).To(Succeed())

				By("reconciling the InfraEnv")
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      infraEnvName,
						Namespace: namespace,
					},
				})
				Expect(err).NotTo(HaveOccurred())

				By("checking that the OpenshiftAssistedConfig does have its ISO URL set")
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(oac), oac)).To(Succeed())
				Expect(oac.Status.ISODownloadURL).To(Equal("http://172.0.0.1:8080/my-image"))
			})
		})
	})
})
