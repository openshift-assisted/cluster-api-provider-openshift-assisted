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

package controller_test

import (
	"context"
	"encoding/json"
	"fmt"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	"k8s.io/apimachinery/pkg/types"

	"github.com/golang/mock/gomock"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/assistedinstaller"
	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/controller"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/pkg/containers"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/test/utils"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	"github.com/openshift/assisted-service/models"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// OACP, ClusterDeployment, and Cluster share the same name
	clusterName      = "test-cluster"
	namespace        = "test"
	openShiftVersion = "4.16.0"
	testDigest       = "sha256:abc123def456789abc123def456789abc123def456789abc123def456789abcd"
	testArchX86      = "x86_64"
)

var _ = Describe("ClusterDeployment Controller", func() {
	ctx := context.Background()
	var controllerReconciler *controller.ClusterDeploymentReconciler
	var k8sClient client.Client
	var ctrl *gomock.Controller
	var mockRemoteImage *containers.MockRemoteImage

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockRemoteImage = containers.NewMockRemoteImage(ctrl)

		k8sClient = fakeclient.NewClientBuilder().WithScheme(testScheme).
			WithStatusSubresource(&hivev1.ClusterDeployment{}, &controlplanev1alpha3.OpenshiftAssistedControlPlane{}).
			Build()
		Expect(k8sClient).NotTo(BeNil())
		controllerReconciler = &controller.ClusterDeploymentReconciler{
			Client:      k8sClient,
			Scheme:      k8sClient.Scheme(),
			RemoteImage: mockRemoteImage,
		}

		// Configure mock to return test digest for any image
		mockRemoteImage.EXPECT().GetDigest(gomock.Any(), gomock.Any()).Return(testDigest, nil).AnyTimes()

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		By("creating the test namespace")
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
	})
	When("A cluster deployment without cluster name label", func() {
		It("should skip and not return error", func() {
			// ClusterDeployment without cluster name label should be skipped
			cd := utils.NewClusterDeployment(namespace, clusterName, nil)
			Expect(k8sClient.Create(ctx, cd)).To(Succeed())
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(cd),
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	When("A cluster deployment with cluster name label but no matching OpenshiftAssistedControlPlane", func() {
		It("should not return error", func() {
			// ClusterDeployment with cluster name label pointing to non-existent OACP
			cd := utils.NewClusterDeploymentWithOwnerCluster(namespace, clusterName, clusterName, nil)
			Expect(k8sClient.Create(ctx, cd)).To(Succeed())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(cd),
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	When("A cluster deployment with OpenshiftAssistedControlPlanes in the same namespace referencing it", func() {
		It("should not return error", func() {
			cluster := utils.NewCluster(clusterName, namespace)
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Create pull secret
			Expect(k8sClient.Create(ctx, newPullSecret("pull-secret", namespace))).To(Succeed())

			enableOn := models.DiskEncryptionEnableOnAll
			mode := models.DiskEncryptionModeTang
			oacp := utils.NewOpenshiftAssistedControlPlane(namespace, clusterName)
			oacp.Labels = map[string]string{
				clusterv1.ClusterNameLabel: clusterName,
			}
			oacp.Spec.DistributionVersion = openShiftVersion
			oacp.Spec.Config.SSHAuthorizedKey = "mykey"
			oacp.Spec.Config.PullSecretRef = &corev1.LocalObjectReference{
				Name: "pull-secret",
			}
			oacp.Spec.Config.DiskEncryption = &hiveext.DiskEncryption{
				EnableOn:    &enableOn,
				Mode:        &mode,
				TangServers: " [{\"url\":\"http://tang.example.com:7500\",\"thumbprint\":\"PLjNyRdGw03zlRoGjQYMahSZGu9\"}, {\"url\":\"http://tang.example.com:7501\",\"thumbprint\":\"PLjNyRdGw03zlRoGjQYMahSZGu8\"}]",
			}
			oacp.Spec.Config.Proxy = &hiveext.Proxy{
				HTTPProxy: "https://example.com",
			}
			oacp.Spec.Config.MastersSchedulable = true

			cd := utils.NewClusterDeploymentWithOwnerCluster(namespace, clusterName, clusterName, oacp)
			Expect(k8sClient.Create(ctx, cd)).To(Succeed())

			// create config associated with this cluster
			config := utils.NewOpenshiftAssistedConfig(namespace, "myconfig", clusterName)
			config.Spec.CpuArchitecture = testArchX86
			Expect(k8sClient.Create(ctx, config)).To(Succeed())

			config = utils.NewOpenshiftAssistedConfig(namespace, "myconfig-arm", clusterName)
			config.Spec.CpuArchitecture = "arm64"
			Expect(k8sClient.Create(ctx, config)).To(Succeed())

			Expect(controllerutil.SetOwnerReference(cluster, oacp, testScheme)).To(Succeed())
			Expect(controllerutil.SetOwnerReference(oacp, cd, testScheme)).To(Succeed())

			Expect(k8sClient.Create(ctx, oacp)).To(Succeed())
			Expect(k8sClient.Update(ctx, cd)).To(Succeed())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(cd),
			})
			Expect(err).NotTo(HaveOccurred())

			aci := &hiveext.AgentClusterInstall{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cd), aci)).To(Succeed())

			// Assert exposed ACI fields are derived from ACP
			Expect(aci.Spec.ManifestsConfigMapRefs).To(Equal(oacp.Spec.Config.ManifestsConfigMapRefs))
			Expect(aci.Spec.DiskEncryption).To(Equal(oacp.Spec.Config.DiskEncryption))
			Expect(aci.Spec.Proxy).To(Equal(oacp.Spec.Config.Proxy))
			Expect(aci.Spec.MastersSchedulable).To(Equal(oacp.Spec.Config.MastersSchedulable))
			Expect(aci.Spec.SSHPublicKey).To(Equal(oacp.Spec.Config.SSHAuthorizedKey))

			// Assert ACI has correct labels
			Expect(aci.Labels).NotTo(BeEmpty())
			Expect(aci.Labels).To(HaveKey(clusterv1.ClusterNameLabel))
			Expect(aci.Labels[clusterv1.ClusterNameLabel]).To(Equal(clusterName))
			Expect(aci.Labels).To(HaveKey(clusterv1.MachineControlPlaneLabel))
			Expect(aci.Labels).To(HaveKey(clusterv1.MachineControlPlaneNameLabel))
			Expect(aci.Labels[clusterv1.MachineControlPlaneNameLabel]).To(Equal(oacp.Name))
			Expect(aci.Labels).To(HaveKey(hiveext.ClusterConsumerLabel))
			Expect(aci.Labels[hiveext.ClusterConsumerLabel]).To(Equal("OpenshiftAssistedControlPlane"))

			clusterImageSet := &hivev1.ClusterImageSet{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cd.Name}, clusterImageSet)).To(Succeed())
			Expect(clusterImageSet.Spec.ReleaseImage).To(Equal("quay.io/openshift-release-dev/ocp-release@" + testDigest))
		})
		When("ACP with ingressVIPs and apiVIPs", func() {
			It("should start a multinode cluster install", func() {
				cluster := utils.NewCluster(clusterName, namespace)
				Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

				// Create pull secret for digest resolution
				pullSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pull-secret",
						Namespace: namespace,
					},
					Data: map[string][]byte{
						assistedinstaller.PullsecretDataKey: []byte(`{"auths":{"registry.example.com":{"auth":"dGVzdDp0ZXN0"}}}`),
					},
				}
				Expect(k8sClient.Create(ctx, pullSecret)).To(Succeed())

				oacp := utils.NewOpenshiftAssistedControlPlane(namespace, clusterName)
				oacp.Spec.DistributionVersion = openShiftVersion
				oacp.Spec.Config.PullSecretRef = &corev1.LocalObjectReference{
					Name: "pull-secret",
				}
				apiVIPs := []string{"1.2.3.4", "2.3.4.5"}
				ingressVIPs := []string{"9.9.9.9", "10.10.10.10"}
				oacp.Spec.Config.APIVIPs = apiVIPs
				oacp.Spec.Config.IngressVIPs = ingressVIPs

				cd := utils.NewClusterDeploymentWithOwnerCluster(namespace, clusterName, clusterName, oacp)

				Expect(controllerutil.SetOwnerReference(cluster, oacp, testScheme)).To(Succeed())
				Expect(controllerutil.SetOwnerReference(oacp, cd, testScheme)).To(Succeed())

				Expect(k8sClient.Create(ctx, oacp)).To(Succeed())
				Expect(k8sClient.Create(ctx, cd)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(cd),
				})
				Expect(err).NotTo(HaveOccurred())

				aci := &hiveext.AgentClusterInstall{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cd), aci)).To(Succeed())

				// Assert baremetal multinode platform install
				Expect(aci.Spec.PlatformType).To(Equal(hiveext.BareMetalPlatformType))
				Expect(aci.Spec.IngressVIPs).To(Equal(ingressVIPs))
				Expect(aci.Spec.APIVIPs).To(Equal(apiVIPs))
				Expect(aci.Annotations).To(HaveKey(controller.InstallConfigOverrides))

				cfgOverride := controller.InstallConfigOverride{}
				Expect(json.Unmarshal([]byte(aci.Annotations[controller.InstallConfigOverrides]), &cfgOverride)).NotTo(HaveOccurred())
				Expect(cfgOverride.Capability.AdditionalEnabledCapabilities).To(Equal([]configv1.ClusterVersionCapability{"baremetal", "Console", "Insights", "OperatorLifecycleManager", "Ingress", "marketplace", "NodeTuning", "DeploymentConfig"}))
				Expect(cfgOverride.Capability.BaselineCapabilitySet).To(Equal(configv1.ClusterVersionCapabilitySet("None")))

			})
		})
	})
	Context("ACI Capabilities", func() {
		Context("Baremetal workload cluster", func() {
			var (
				cd   *hivev1.ClusterDeployment
				oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane
			)
			BeforeEach(func() {
				cluster := utils.NewCluster(clusterName, namespace)
				Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

				// Create pull secret for digest resolution
				pullSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pull-secret",
						Namespace: namespace,
					},
					Data: map[string][]byte{
						assistedinstaller.PullsecretDataKey: []byte(`{"auths":{"registry.example.com":{"auth":"dGVzdDp0ZXN0"}}}`),
					},
				}
				Expect(k8sClient.Create(ctx, pullSecret)).To(Succeed())

				oacp = utils.NewOpenshiftAssistedControlPlane(namespace, clusterName)
				oacp.Spec.DistributionVersion = openShiftVersion
				oacp.Spec.Config.PullSecretRef = &corev1.LocalObjectReference{
					Name: "pull-secret",
				}
				apiVIPs := []string{"1.2.3.4", "2.3.4.5"}
				ingressVIPs := []string{"9.9.9.9", "10.10.10.10"}
				oacp.Spec.Config.APIVIPs = apiVIPs
				oacp.Spec.Config.IngressVIPs = ingressVIPs

				cd = utils.NewClusterDeploymentWithOwnerCluster(namespace, clusterName, clusterName, oacp)
				Expect(controllerutil.SetOwnerReference(cluster, oacp, testScheme)).To(Succeed())
				Expect(controllerutil.SetOwnerReference(oacp, cd, testScheme)).To(Succeed())

				Expect(k8sClient.Create(ctx, oacp)).To(Succeed())
				Expect(k8sClient.Create(ctx, cd)).To(Succeed())
			})
			When("no capabilities are specified", func() {
				It("ACI should have default baremetal install config override annotation", func() {
					_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
						NamespacedName: client.ObjectKeyFromObject(cd),
					})
					Expect(err).NotTo(HaveOccurred())

					aci := &hiveext.AgentClusterInstall{}
					Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cd), aci)).To(Succeed())
					By("Verifying the ACI has the default install config overrides for baremetal set in its annotations")
					Expect(aci.Annotations).To(HaveKey(controller.InstallConfigOverrides))
					Expect(aci.Annotations[controller.InstallConfigOverrides]).To(Equal(`{"capabilities":{"baselineCapabilitySet":"None","additionalEnabledCapabilities":["baremetal","Console","Insights","OperatorLifecycleManager","Ingress","marketplace","NodeTuning","DeploymentConfig"]}}`))
				})
			})
			When("additional capabilities are specified", func() {
				It("ACI should have default baremetal install config override annotation along with the additional capabilities", func() {
					oacp.Spec.Config.Capabilities = controlplanev1alpha3.Capabilities{AdditionalEnabledCapabilities: []string{"CloudControllerManager"}}
					Expect(k8sClient.Update(ctx, oacp)).To(Succeed())
					_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
						NamespacedName: client.ObjectKeyFromObject(cd),
					})
					Expect(err).NotTo(HaveOccurred())

					aci := &hiveext.AgentClusterInstall{}
					Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cd), aci)).To(Succeed())

					By("Verifying the ACI has the default install config overrides for baremetal and the additional capability set in its annotations")
					Expect(aci.Annotations).To(HaveKey(controller.InstallConfigOverrides))
					Expect(aci.Annotations[controller.InstallConfigOverrides]).To(Equal(`{"capabilities":{"baselineCapabilitySet":"None","additionalEnabledCapabilities":["baremetal","Console","Insights","OperatorLifecycleManager","Ingress","marketplace","NodeTuning","DeploymentConfig","CloudControllerManager"]}}`))
				})
			})
			When("additional capabilities are the same as the default baremetal capabilities", func() {
				It("ACI should have default baremetal install config override annotation with no duplicates", func() {
					oacp.Spec.Config.Capabilities = controlplanev1alpha3.Capabilities{AdditionalEnabledCapabilities: []string{"baremetal"}}
					Expect(k8sClient.Update(ctx, oacp)).To(Succeed())

					_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
						NamespacedName: client.ObjectKeyFromObject(cd),
					})
					Expect(err).NotTo(HaveOccurred())

					aci := &hiveext.AgentClusterInstall{}
					Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cd), aci)).To(Succeed())

					By("Verifying the ACI has the default install config overrides for baremetal without duplicates")
					Expect(aci.Annotations).To(HaveKey(controller.InstallConfigOverrides))
					Expect(aci.Annotations[controller.InstallConfigOverrides]).To(Equal(`{"capabilities":{"baselineCapabilitySet":"None","additionalEnabledCapabilities":["baremetal","Console","Insights","OperatorLifecycleManager","Ingress","marketplace","NodeTuning","DeploymentConfig"]}}`))
				})
			})
			When("additional capabilities include MAPI", func() {
				It("ACI should have default baremetal install config override annotation without MAPI", func() {
					oacp.Spec.Config.Capabilities = controlplanev1alpha3.Capabilities{AdditionalEnabledCapabilities: []string{"MachineAPI", "CloudControllerManager"}}
					Expect(k8sClient.Update(ctx, oacp)).To(Succeed())

					_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
						NamespacedName: client.ObjectKeyFromObject(cd),
					})
					Expect(err).NotTo(HaveOccurred())

					aci := &hiveext.AgentClusterInstall{}
					Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cd), aci)).To(Succeed())

					By("Verifying the ACI has the default install config overrides for baremetal without MAPI")
					Expect(aci.Annotations).To(HaveKey(controller.InstallConfigOverrides))
					Expect(aci.Annotations[controller.InstallConfigOverrides]).To(Equal(`{"capabilities":{"baselineCapabilitySet":"None","additionalEnabledCapabilities":["baremetal","Console","Insights","OperatorLifecycleManager","Ingress","marketplace","NodeTuning","DeploymentConfig","CloudControllerManager"]}}`))
				})
			})
			When("only baseline capability is specified", func() {
				It("ACI should have default baremetal install config override annotation with specified baseline capability", func() {
					oacp.Spec.Config.Capabilities = controlplanev1alpha3.Capabilities{BaselineCapability: "vCurrent"}
					Expect(k8sClient.Update(ctx, oacp)).To(Succeed())

					_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
						NamespacedName: client.ObjectKeyFromObject(cd),
					})
					Expect(err).NotTo(HaveOccurred())

					aci := &hiveext.AgentClusterInstall{}
					Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cd), aci)).To(Succeed())

					By("Verifying the ACI has the correct install config overrides for baremetal and the specified baseline capability")
					Expect(aci.Annotations).To(HaveKey(controller.InstallConfigOverrides))
					Expect(aci.Annotations[controller.InstallConfigOverrides]).To(Equal(`{"capabilities":{"baselineCapabilitySet":"vCurrent","additionalEnabledCapabilities":["baremetal","Console","Insights","OperatorLifecycleManager","Ingress","marketplace","NodeTuning","DeploymentConfig"]}}`))
				})
			})
			When("install config override annotation is set with MAPI capability", func() {
				It("should still exclude MAPI from baremetal cluster capabilities", func() {
					// Set install config override annotation
					installConfigOverride := `{"networking":{"machineNetwork":[{"cidr":"10.0.0.0/16"}]}}`
					if oacp.Annotations == nil {
						oacp.Annotations = make(map[string]string)
					}
					oacp.Annotations[controlplanev1alpha3.InstallConfigOverrideAnnotation] = installConfigOverride

					// Set MAPI in additional capabilities
					oacp.Spec.Config.Capabilities = controlplanev1alpha3.Capabilities{AdditionalEnabledCapabilities: []string{"MachineAPI", "CloudControllerManager"}}
					Expect(k8sClient.Update(ctx, oacp)).To(Succeed())

					_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
						NamespacedName: client.ObjectKeyFromObject(cd),
					})
					Expect(err).NotTo(HaveOccurred())

					aci := &hiveext.AgentClusterInstall{}
					Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cd), aci)).To(Succeed())

					By("Verifying the ACI has the install config override annotation")
					Expect(aci.Annotations).To(HaveKey(controller.InstallConfigOverrides))

					By("Verifying the ACI excludes MAPI from capabilities even with install config override")

					// The annotation should contain both the install config override and the capabilities
					// but MAPI should still be excluded for baremetal platforms
					cfgOverride := controller.InstallConfigOverride{}
					Expect(json.Unmarshal([]byte(aci.Annotations[controller.InstallConfigOverrides]), &cfgOverride)).NotTo(HaveOccurred())

					Expect(cfgOverride.Capability.AdditionalEnabledCapabilities).To(Equal([]configv1.ClusterVersionCapability{"baremetal", "Console", "Insights", "OperatorLifecycleManager", "Ingress", "marketplace", "NodeTuning", "DeploymentConfig", "CloudControllerManager"}))
					Expect(cfgOverride.Capability.BaselineCapabilitySet).To(Equal(configv1.ClusterVersionCapabilitySet("None")))

					// Verify MAPI is not in the capabilities
					Expect(aci.Annotations[controller.InstallConfigOverrides]).NotTo(ContainSubstring("MachineAPI"))
				})
			})
		})
		Context("Non-baremetal workload cluster", func() {
			var (
				cd   *hivev1.ClusterDeployment
				oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane
			)
			BeforeEach(func() {
				cluster := utils.NewCluster(clusterName, namespace)
				Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

				// Create pull secret for digest resolution
				pullSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pull-secret",
						Namespace: namespace,
					},
					Data: map[string][]byte{
						assistedinstaller.PullsecretDataKey: []byte(`{"auths":{"registry.example.com":{"auth":"dGVzdDp0ZXN0"}}}`),
					},
				}
				Expect(k8sClient.Create(ctx, pullSecret)).To(Succeed())

				oacp = utils.NewOpenshiftAssistedControlPlane(namespace, clusterName)
				oacp.Spec.DistributionVersion = openShiftVersion
				oacp.Spec.Config.PullSecretRef = &corev1.LocalObjectReference{
					Name: "pull-secret",
				}

				cd = utils.NewClusterDeploymentWithOwnerCluster(namespace, clusterName, clusterName, oacp)

				Expect(controllerutil.SetOwnerReference(cluster, oacp, testScheme)).To(Succeed())
				Expect(controllerutil.SetControllerReference(oacp, cd, testScheme)).To(Succeed())

				Expect(k8sClient.Create(ctx, oacp)).To(Succeed())
				Expect(k8sClient.Create(ctx, cd)).To(Succeed())
			})
			When("no capabilities are specified", func() {
				It("ACI should not have install config override annotation", func() {
					_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
						NamespacedName: client.ObjectKeyFromObject(cd),
					})
					Expect(err).NotTo(HaveOccurred())

					aci := &hiveext.AgentClusterInstall{}
					Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cd), aci)).To(Succeed())
					By("Verifying the ACI does not have the install config overrides set in its annotations")
					Expect(aci.Annotations).ToNot(HaveKey(controller.InstallConfigOverrides))
				})
			})
			When("additional capabilities are specified", func() {
				It("ACI should have only the specified capabilities in the install config override annotation and the baseline should be set to the default", func() {
					oacp.Spec.Config.Capabilities = controlplanev1alpha3.Capabilities{AdditionalEnabledCapabilities: []string{"NodeTuning"}}

					Expect(k8sClient.Update(ctx, oacp)).To(Succeed())
					_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
						NamespacedName: client.ObjectKeyFromObject(cd),
					})
					Expect(err).NotTo(HaveOccurred())

					aci := &hiveext.AgentClusterInstall{}
					Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cd), aci)).To(Succeed())

					By("Verifying the ACI has the correct install config overrides annotation")
					Expect(aci.Annotations).To(HaveKey(controller.InstallConfigOverrides))
					Expect(aci.Annotations[controller.InstallConfigOverrides]).To(Equal(`{"capabilities":{"baselineCapabilitySet":"vCurrent","additionalEnabledCapabilities":["NodeTuning"]}}`))
				})
			})
			When("additional capabilities include MAPI", func() {
				It("ACI should include MAPI in its install config override annotation", func() {
					oacp.Spec.Config.Capabilities = controlplanev1alpha3.Capabilities{AdditionalEnabledCapabilities: []string{"MachineAPI", "NodeTuning"}}
					Expect(k8sClient.Update(ctx, oacp)).To(Succeed())

					_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
						NamespacedName: client.ObjectKeyFromObject(cd),
					})
					Expect(err).NotTo(HaveOccurred())

					aci := &hiveext.AgentClusterInstall{}
					Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cd), aci)).To(Succeed())

					By("Verifying the ACI has the install config overrides annotation and it includes MAPI")
					Expect(aci.Annotations).To(HaveKey(controller.InstallConfigOverrides))
					Expect(aci.Annotations[controller.InstallConfigOverrides]).To(Equal(`{"capabilities":{"baselineCapabilitySet":"vCurrent","additionalEnabledCapabilities":["MachineAPI","NodeTuning"]}}`))
				})
			})
			When("only baseline capability is specified", func() {
				It("ACI should have only have the specified baseline capability", func() {
					oacp.Spec.Config.Capabilities = controlplanev1alpha3.Capabilities{BaselineCapability: "v4.17"}
					Expect(k8sClient.Update(ctx, oacp)).To(Succeed())

					_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
						NamespacedName: client.ObjectKeyFromObject(cd),
					})
					Expect(err).NotTo(HaveOccurred())

					aci := &hiveext.AgentClusterInstall{}
					Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cd), aci)).To(Succeed())

					By("Verifying the ACI has only the baseline capability in its annotation for install config override set")
					Expect(aci.Annotations).To(HaveKey(controller.InstallConfigOverrides))
					Expect(aci.Annotations[controller.InstallConfigOverrides]).To(Equal(`{"capabilities":{"baselineCapabilitySet":"v4.17"}}`))
				})
			})
			When("both baseline capability and additional capabilities are specified", func() {
				It("ACI should have the install config override annotation with both specified baseline capability and additional capabilities set", func() {
					oacp.Spec.Config.Capabilities = controlplanev1alpha3.Capabilities{BaselineCapability: "v4.8", AdditionalEnabledCapabilities: []string{"NodeTuning"}}
					Expect(k8sClient.Update(ctx, oacp)).To(Succeed())

					_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
						NamespacedName: client.ObjectKeyFromObject(cd),
					})
					Expect(err).NotTo(HaveOccurred())

					aci := &hiveext.AgentClusterInstall{}
					Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cd), aci)).To(Succeed())

					By("Verifying the ACI has the install config overrides with specified baseline and additional capabilities set")
					Expect(aci.Annotations).To(HaveKey(controller.InstallConfigOverrides))
					Expect(aci.Annotations[controller.InstallConfigOverrides]).To(Equal(`{"capabilities":{"baselineCapabilitySet":"v4.8","additionalEnabledCapabilities":["NodeTuning"]}}`))
				})
			})
			When("baseline capability is not valid", func() {
				It("should error out", func() {
					oacp.Spec.Config.Capabilities = controlplanev1alpha3.Capabilities{BaselineCapability: "abcd"}
					Expect(k8sClient.Update(ctx, oacp)).To(Succeed())

					_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
						NamespacedName: client.ObjectKeyFromObject(cd),
					})
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(Equal("invalid baseline capability set, must be one of: None, vCurrent, or v4.x. Got: [abcd]"))

					aci := &hiveext.AgentClusterInstall{}
					Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cd), aci)).NotTo(Succeed())
				})
			})
		})

	})

	Context("Install Config Override Annotation", func() {
		When("OpenshiftAssistedControlPlane has install config override annotation", func() {
			It("should propagate the annotation to the AgentClusterInstall", func() {
				cluster := utils.NewCluster(clusterName, namespace)
				Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

				// Create pull secret for digest resolution
				pullSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pull-secret",
						Namespace: namespace,
					},
					Data: map[string][]byte{
						assistedinstaller.PullsecretDataKey: []byte(`{"auths":{"registry.example.com":{"auth":"dGVzdDp0ZXN0"}}}`),
					},
				}
				Expect(k8sClient.Create(ctx, pullSecret)).To(Succeed())

				oacp := utils.NewOpenshiftAssistedControlPlane(namespace, clusterName)
				oacp.Spec.DistributionVersion = openShiftVersion
				oacp.Spec.Config.PullSecretRef = &corev1.LocalObjectReference{
					Name: "pull-secret",
				}

				// Set the install config override annotation
				installConfigOverride := `{"networking":{"machineNetwork":[{"cidr":"10.0.0.0/16"}]}}`
				if oacp.Annotations == nil {
					oacp.Annotations = make(map[string]string)
				}
				oacp.Annotations[controlplanev1alpha3.InstallConfigOverrideAnnotation] = installConfigOverride

				cd := utils.NewClusterDeploymentWithOwnerCluster(namespace, clusterName, clusterName, oacp)
				Expect(controllerutil.SetOwnerReference(cluster, oacp, testScheme)).To(Succeed())
				Expect(controllerutil.SetOwnerReference(oacp, cd, testScheme)).To(Succeed())

				Expect(k8sClient.Create(ctx, oacp)).To(Succeed())
				Expect(k8sClient.Create(ctx, cd)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(cd),
				})
				Expect(err).NotTo(HaveOccurred())

				aci := &hiveext.AgentClusterInstall{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cd), aci)).To(Succeed())

				By("Verifying the ACI has the install config override annotation propagated from the control plane")
				Expect(aci.Annotations).To(HaveKey(controller.InstallConfigOverrides))
				Expect(aci.Annotations[controller.InstallConfigOverrides]).To(Equal(installConfigOverride))
			})
		})
		When("OpenshiftAssistedControlPlane does not have install config override annotation", func() {
			It("should not set the annotation on the AgentClusterInstall", func() {
				cluster := utils.NewCluster(clusterName, namespace)
				Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

				// Create pull secret for digest resolution
				pullSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pull-secret",
						Namespace: namespace,
					},
					Data: map[string][]byte{
						assistedinstaller.PullsecretDataKey: []byte(`{"auths":{"registry.example.com":{"auth":"dGVzdDp0ZXN0"}}}`),
					},
				}
				Expect(k8sClient.Create(ctx, pullSecret)).To(Succeed())

				oacp := utils.NewOpenshiftAssistedControlPlane(namespace, clusterName)
				oacp.Spec.DistributionVersion = openShiftVersion
				oacp.Spec.Config.PullSecretRef = &corev1.LocalObjectReference{
					Name: "pull-secret",
				}

				cd := utils.NewClusterDeploymentWithOwnerCluster(namespace, clusterName, clusterName, oacp)

				Expect(controllerutil.SetOwnerReference(cluster, oacp, testScheme)).To(Succeed())
				Expect(controllerutil.SetControllerReference(oacp, cd, testScheme)).To(Succeed())

				Expect(k8sClient.Create(ctx, oacp)).To(Succeed())
				Expect(k8sClient.Create(ctx, cd)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(cd),
				})
				Expect(err).NotTo(HaveOccurred())

				aci := &hiveext.AgentClusterInstall{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cd), aci)).To(Succeed())

				By("Verifying the ACI does not have the install config override annotation when not set on the control plane")
				Expect(aci.Annotations).NotTo(HaveKey(controller.InstallConfigOverrides))
			})
		})
	})
	Context("ClusterDeployment ClusterInstallRef update", func() {
		When("ClusterInstallRef is already correctly set", func() {
			It("should skip the update and not error", func() {
				cluster := utils.NewCluster(clusterName, namespace)
				Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

				// Create pull secret for digest resolution
				pullSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pull-secret",
						Namespace: namespace,
					},
					Data: map[string][]byte{
						assistedinstaller.PullsecretDataKey: []byte(`{"auths":{"registry.example.com":{"auth":"dGVzdDp0ZXN0"}}}`),
					},
				}
				Expect(k8sClient.Create(ctx, pullSecret)).To(Succeed())

				oacp := utils.NewOpenshiftAssistedControlPlane(namespace, clusterName)
				oacp.Spec.DistributionVersion = openShiftVersion
				oacp.Spec.Config.PullSecretRef = &corev1.LocalObjectReference{
					Name: "pull-secret",
				}
				Expect(controllerutil.SetOwnerReference(cluster, oacp, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, oacp)).To(Succeed())

				cd := utils.NewClusterDeploymentWithOwnerCluster(namespace, clusterName, clusterName, oacp)
				cd.Spec.ClusterInstallRef = &hivev1.ClusterInstallLocalReference{
					Group:   hiveext.Group,
					Version: hiveext.Version,
					Kind:    "AgentClusterInstall",
					Name:    cd.Name,
				}
				Expect(controllerutil.SetOwnerReference(oacp, cd, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, cd)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(cd),
				})
				Expect(err).NotTo(HaveOccurred())

				By("Verifying the ClusterInstallRef is still set correctly")
				updatedCD := &hivev1.ClusterDeployment{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cd), updatedCD)).To(Succeed())
				Expect(updatedCD.Spec.ClusterInstallRef).NotTo(BeNil())
				Expect(updatedCD.Spec.ClusterInstallRef.Group).To(Equal(hiveext.Group))
				Expect(updatedCD.Spec.ClusterInstallRef.Version).To(Equal(hiveext.Version))
				Expect(updatedCD.Spec.ClusterInstallRef.Kind).To(Equal("AgentClusterInstall"))
				Expect(updatedCD.Spec.ClusterInstallRef.Name).To(Equal(cd.Name))
			})
		})
		When("ClusterInstallRef is not set", func() {
			It("should set the ClusterInstallRef", func() {
				cluster := utils.NewCluster(clusterName, namespace)
				Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

				// Create pull secret for digest resolution
				pullSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pull-secret",
						Namespace: namespace,
					},
					Data: map[string][]byte{
						assistedinstaller.PullsecretDataKey: []byte(`{"auths":{"registry.example.com":{"auth":"dGVzdDp0ZXN0"}}}`),
					},
				}
				Expect(k8sClient.Create(ctx, pullSecret)).To(Succeed())

				oacp := utils.NewOpenshiftAssistedControlPlane(namespace, clusterName)
				oacp.Spec.DistributionVersion = openShiftVersion
				oacp.Spec.Config.PullSecretRef = &corev1.LocalObjectReference{
					Name: "pull-secret",
				}
				Expect(controllerutil.SetOwnerReference(cluster, oacp, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, oacp)).To(Succeed())

				cd := utils.NewClusterDeploymentWithOwnerCluster(namespace, clusterName, clusterName, oacp)
				cd.Spec.ClusterInstallRef = nil
				Expect(controllerutil.SetOwnerReference(oacp, cd, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, cd)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(cd),
				})
				Expect(err).NotTo(HaveOccurred())

				By("Verifying the ClusterInstallRef is now set")
				updatedCD := &hivev1.ClusterDeployment{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cd), updatedCD)).To(Succeed())
				Expect(updatedCD.Spec.ClusterInstallRef).NotTo(BeNil())
				Expect(updatedCD.Spec.ClusterInstallRef.Group).To(Equal(hiveext.Group))
				Expect(updatedCD.Spec.ClusterInstallRef.Version).To(Equal(hiveext.Version))
				Expect(updatedCD.Spec.ClusterInstallRef.Kind).To(Equal("AgentClusterInstall"))
				Expect(updatedCD.Spec.ClusterInstallRef.Name).To(Equal(cd.Name))
			})
		})
		When("ClusterInstallRef has incorrect values", func() {
			It("should update the ClusterInstallRef to the correct values", func() {
				cluster := utils.NewCluster(clusterName, namespace)
				Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

				// Create pull secret for digest resolution
				pullSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pull-secret",
						Namespace: namespace,
					},
					Data: map[string][]byte{
						assistedinstaller.PullsecretDataKey: []byte(`{"auths":{"registry.example.com":{"auth":"dGVzdDp0ZXN0"}}}`),
					},
				}
				Expect(k8sClient.Create(ctx, pullSecret)).To(Succeed())

				oacp := utils.NewOpenshiftAssistedControlPlane(namespace, clusterName)
				oacp.Spec.DistributionVersion = openShiftVersion
				oacp.Spec.Config.PullSecretRef = &corev1.LocalObjectReference{
					Name: "pull-secret",
				}
				Expect(controllerutil.SetOwnerReference(cluster, oacp, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, oacp)).To(Succeed())

				cd := utils.NewClusterDeploymentWithOwnerCluster(namespace, clusterName, clusterName, oacp)
				cd.Spec.ClusterInstallRef = &hivev1.ClusterInstallLocalReference{
					Group:   "wrong.group",
					Version: "wrong-version",
					Kind:    "WrongKind",
					Name:    "wrong-name",
				}
				Expect(controllerutil.SetOwnerReference(oacp, cd, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, cd)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(cd),
				})
				Expect(err).NotTo(HaveOccurred())

				By("Verifying the ClusterInstallRef has been corrected")
				updatedCD := &hivev1.ClusterDeployment{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cd), updatedCD)).To(Succeed())
				Expect(updatedCD.Spec.ClusterInstallRef).NotTo(BeNil())
				Expect(updatedCD.Spec.ClusterInstallRef.Group).To(Equal(hiveext.Group))
				Expect(updatedCD.Spec.ClusterInstallRef.Version).To(Equal(hiveext.Version))
				Expect(updatedCD.Spec.ClusterInstallRef.Kind).To(Equal("AgentClusterInstall"))
				Expect(updatedCD.Spec.ClusterInstallRef.Name).To(Equal(cd.Name))
			})
		})
	})

	When("digest resolution fails", func() {
		It("should return error and requeue", func() {
			// Create a local mock for this specific test
			localCtrl := gomock.NewController(GinkgoT())
			localMockRemoteImage := containers.NewMockRemoteImage(localCtrl)

			// Configure mock to return error on GetDigest
			localMockRemoteImage.EXPECT().GetDigest(gomock.Any(), gomock.Any()).
				Return("", fmt.Errorf("failed to contact registry: connection timeout"))

			// Create reconciler with the failing mock
			localReconciler := &controller.ClusterDeploymentReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				RemoteImage: localMockRemoteImage,
			}

			cluster := utils.NewCluster(clusterName, namespace)
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Create pull secret
			pullSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pull-secret-fail",
					Namespace: namespace,
				},
				Data: map[string][]byte{
					assistedinstaller.PullsecretDataKey: []byte(`{"auths":{"registry.example.com":{"auth":"dGVzdDp0ZXN0"}}}`),
				},
			}
			Expect(k8sClient.Create(ctx, pullSecret)).To(Succeed())

			oacp := utils.NewOpenshiftAssistedControlPlane(namespace, "test-digest-fail")
			oacp.Labels = map[string]string{
				clusterv1.ClusterNameLabel: clusterName,
			}
			oacp.Spec.DistributionVersion = openShiftVersion
			oacp.Spec.Config.PullSecretRef = &corev1.LocalObjectReference{
				Name: "pull-secret-fail",
			}

			cd := utils.NewClusterDeploymentWithOwnerCluster(namespace, "test-digest-fail", clusterName, oacp)
			Expect(k8sClient.Create(ctx, cd)).To(Succeed())

			// create config associated with this cluster
			config := utils.NewOpenshiftAssistedConfig(namespace, "myconfig-fail", clusterName)
			config.Spec.CpuArchitecture = testArchX86
			Expect(k8sClient.Create(ctx, config)).To(Succeed())

			Expect(controllerutil.SetOwnerReference(cluster, oacp, testScheme)).To(Succeed())
			Expect(controllerutil.SetOwnerReference(oacp, cd, testScheme)).To(Succeed())

			Expect(k8sClient.Create(ctx, oacp)).To(Succeed())
			Expect(k8sClient.Update(ctx, cd)).To(Succeed())

			// Reconcile should return error
			_, err := localReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(cd),
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to resolve release image digest"))
			Expect(err.Error()).To(ContainSubstring("4.16.0"))
		})
	})

	When("pull secret is missing", func() {
		It("should return error", func() {
			cluster := utils.NewCluster(clusterName, namespace)
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Create OACP without pull secret
			oacp := utils.NewOpenshiftAssistedControlPlane(namespace, "test-no-pullsecret")
			oacp.Labels = map[string]string{
				clusterv1.ClusterNameLabel: clusterName,
			}
			oacp.Spec.DistributionVersion = openShiftVersion
			oacp.Spec.Config.PullSecretRef = &corev1.LocalObjectReference{
				Name: "nonexistent-secret",
			}

			cd := utils.NewClusterDeploymentWithOwnerCluster(namespace, "test-no-pullsecret", clusterName, oacp)
			Expect(k8sClient.Create(ctx, cd)).To(Succeed())

			// create config associated with this cluster
			config := utils.NewOpenshiftAssistedConfig(namespace, "myconfig-no-ps", clusterName)
			config.Spec.CpuArchitecture = testArchX86
			Expect(k8sClient.Create(ctx, config)).To(Succeed())

			Expect(controllerutil.SetOwnerReference(cluster, oacp, testScheme)).To(Succeed())
			Expect(controllerutil.SetOwnerReference(oacp, cd, testScheme)).To(Succeed())

			Expect(k8sClient.Create(ctx, oacp)).To(Succeed())
			Expect(k8sClient.Update(ctx, cd)).To(Succeed())

			// Reconcile should return error about missing pull secret
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(cd),
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get pull secret"))
		})
	})

	When("repository override annotation is set", func() {
		It("should resolve digest from overridden repository", func() {
			// Create a local mock to verify the image ref passed to GetDigest
			localCtrl := gomock.NewController(GinkgoT())
			localMockRemoteImage := containers.NewMockRemoteImage(localCtrl)

			overrideRepo := "quay.io/custom/custom-release"
			expectedImage := overrideRepo + ":4.16.0-" + testArchX86

			// Configure mock to expect the overridden repository
			localMockRemoteImage.EXPECT().GetDigest(expectedImage, gomock.Any()).
				Return(testDigest, nil).Times(1)

			// Create reconciler with the local mock
			localReconciler := &controller.ClusterDeploymentReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				RemoteImage: localMockRemoteImage,
			}

			cluster := utils.NewCluster(clusterName, namespace)
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Create pull secret
			pullSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pull-secret-override",
					Namespace: namespace,
				},
				Data: map[string][]byte{
					assistedinstaller.PullsecretDataKey: []byte(`{"auths":{"registry.example.com":{"auth":"dGVzdDp0ZXN0"}}}`),
				},
			}
			Expect(k8sClient.Create(ctx, pullSecret)).To(Succeed())

			oacp := utils.NewOpenshiftAssistedControlPlane(namespace, "test-override")
			oacp.Labels = map[string]string{
				clusterv1.ClusterNameLabel: clusterName,
			}
			oacp.Annotations = map[string]string{
				"cluster.x-k8s.io/release-image-repository-override": overrideRepo,
			}
			oacp.Spec.DistributionVersion = openShiftVersion
			oacp.Spec.Config.PullSecretRef = &corev1.LocalObjectReference{
				Name: "pull-secret-override",
			}

			cd := utils.NewClusterDeploymentWithOwnerCluster(namespace, "test-override", clusterName, oacp)
			Expect(k8sClient.Create(ctx, cd)).To(Succeed())

			// create config associated with this cluster
			config := utils.NewOpenshiftAssistedConfig(namespace, "myconfig-override", clusterName)
			config.Spec.CpuArchitecture = testArchX86
			Expect(k8sClient.Create(ctx, config)).To(Succeed())

			Expect(controllerutil.SetOwnerReference(cluster, oacp, testScheme)).To(Succeed())
			Expect(controllerutil.SetOwnerReference(oacp, cd, testScheme)).To(Succeed())

			Expect(k8sClient.Create(ctx, oacp)).To(Succeed())
			Expect(k8sClient.Update(ctx, cd)).To(Succeed())

			_, err := localReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(cd),
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify ClusterImageSet has digest-based image with overridden repo
			clusterImageSet := &hivev1.ClusterImageSet{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cd.Name}, clusterImageSet)).To(Succeed())
			Expect(clusterImageSet.Spec.ReleaseImage).To(Equal(overrideRepo + "@" + testDigest))
		})
	})

	When("updating existing tag-based ClusterImageSet", func() {
		It("should update to digest-based reference", func() {
			cluster := utils.NewCluster(clusterName, namespace)
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Create pull secret
			pullSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pull-secret-update",
					Namespace: namespace,
				},
				Data: map[string][]byte{
					assistedinstaller.PullsecretDataKey: []byte(`{"auths":{"registry.example.com":{"auth":"dGVzdDp0ZXN0"}}}`),
				},
			}
			Expect(k8sClient.Create(ctx, pullSecret)).To(Succeed())

			// Pre-create ClusterImageSet with tag-based image
			tagBasedImage := "quay.io/openshift-release-dev/ocp-release:4.16.0-multi"
			existingImageSet := &hivev1.ClusterImageSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-update-cis",
				},
				Spec: hivev1.ClusterImageSetSpec{
					ReleaseImage: tagBasedImage,
				},
			}
			Expect(k8sClient.Create(ctx, existingImageSet)).To(Succeed())

			oacp := utils.NewOpenshiftAssistedControlPlane(namespace, "test-update-cis")
			oacp.Labels = map[string]string{
				clusterv1.ClusterNameLabel: clusterName,
			}
			oacp.Spec.DistributionVersion = openShiftVersion
			oacp.Spec.Config.PullSecretRef = &corev1.LocalObjectReference{
				Name: "pull-secret-update",
			}

			cd := utils.NewClusterDeploymentWithOwnerCluster(namespace, "test-update-cis", clusterName, oacp)
			Expect(k8sClient.Create(ctx, cd)).To(Succeed())

			// create config associated with this cluster
			config := utils.NewOpenshiftAssistedConfig(namespace, "myconfig-update", clusterName)
			config.Spec.CpuArchitecture = testArchX86
			Expect(k8sClient.Create(ctx, config)).To(Succeed())

			Expect(controllerutil.SetOwnerReference(cluster, oacp, testScheme)).To(Succeed())
			Expect(controllerutil.SetOwnerReference(oacp, cd, testScheme)).To(Succeed())

			Expect(k8sClient.Create(ctx, oacp)).To(Succeed())
			Expect(k8sClient.Update(ctx, cd)).To(Succeed())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(cd),
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify ClusterImageSet was updated to digest-based
			updatedImageSet := &hivev1.ClusterImageSet{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cd.Name}, updatedImageSet)).To(Succeed())
			Expect(updatedImageSet.Spec.ReleaseImage).To(Equal("quay.io/openshift-release-dev/ocp-release@" + testDigest))
			Expect(updatedImageSet.Spec.ReleaseImage).NotTo(Equal(tagBasedImage))
		})
	})

	When("using OKD release image", func() {
		It("should resolve digest correctly", func() {
			cluster := utils.NewCluster(clusterName, namespace)
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Create pull secret
			pullSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pull-secret-okd",
					Namespace: namespace,
				},
				Data: map[string][]byte{
					assistedinstaller.PullsecretDataKey: []byte(`{"auths":{"registry.example.com":{"auth":"dGVzdDp0ZXN0"}}}`),
				},
			}
			Expect(k8sClient.Create(ctx, pullSecret)).To(Succeed())

			okdVersion := "4.18.0-okd-scos.ec.1"
			oacp := utils.NewOpenshiftAssistedControlPlane(namespace, "test-okd")
			oacp.Labels = map[string]string{
				clusterv1.ClusterNameLabel: clusterName,
			}
			oacp.Spec.DistributionVersion = okdVersion
			oacp.Spec.Config.PullSecretRef = &corev1.LocalObjectReference{
				Name: "pull-secret-okd",
			}

			cd := utils.NewClusterDeploymentWithOwnerCluster(namespace, "test-okd", clusterName, oacp)
			Expect(k8sClient.Create(ctx, cd)).To(Succeed())

			// create config associated with this cluster
			config := utils.NewOpenshiftAssistedConfig(namespace, "myconfig-okd", clusterName)
			config.Spec.CpuArchitecture = testArchX86
			Expect(k8sClient.Create(ctx, config)).To(Succeed())

			Expect(controllerutil.SetOwnerReference(cluster, oacp, testScheme)).To(Succeed())
			Expect(controllerutil.SetOwnerReference(oacp, cd, testScheme)).To(Succeed())

			Expect(k8sClient.Create(ctx, oacp)).To(Succeed())
			Expect(k8sClient.Update(ctx, cd)).To(Succeed())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(cd),
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify ClusterImageSet has digest-based OKD image
			clusterImageSet := &hivev1.ClusterImageSet{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cd.Name}, clusterImageSet)).To(Succeed())
			// OKD uses quay.io/okd/scos-release repository
			Expect(clusterImageSet.Spec.ReleaseImage).To(ContainSubstring("quay.io/okd/scos-release@" + testDigest))
		})
	})

	AfterEach(func() {
		k8sClient = nil
		controllerReconciler = nil
	})
})
