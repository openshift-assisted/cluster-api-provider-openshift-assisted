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

	"k8s.io/apimachinery/pkg/types"

	"github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1alpha2"
	"github.com/openshift-assisted/cluster-api-agent/test/utils"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	"github.com/openshift/assisted-service/models"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"k8s.io/client-go/tools/reference"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	openshiftAssistedControlPlaneName = "test-resource"
	clusterDeploymentName             = "test-clusterdeployment"
	namespace                         = "test"
	clusterName                       = "test-cluster"
	openShiftVersion                  = "4.16.0"
)

var _ = Describe("ClusterDeployment Controller", func() {
	ctx := context.Background()
	var controllerReconciler *ClusterDeploymentReconciler
	var k8sClient client.Client

	BeforeEach(func() {
		k8sClient = fakeclient.NewClientBuilder().WithScheme(testScheme).
			WithStatusSubresource(&hivev1.ClusterDeployment{}, &v1alpha2.OpenshiftAssistedControlPlane{}).
			Build()
		Expect(k8sClient).NotTo(BeNil())
		controllerReconciler = &ClusterDeploymentReconciler{
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
	When("A cluster deployment with no OpenshiftAssistedControlPlanes in the same namespace", func() {
		It("should not return error", func() {
			cd := utils.NewClusterDeployment(namespace, clusterDeploymentName)
			Expect(k8sClient.Create(ctx, cd)).To(Succeed())
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(cd),
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	When("A cluster deployment with OpenshiftAssistedControlPlanes in the same namespace, but none referencing it", func() {
		It("should not return error", func() {
			cd := utils.NewClusterDeployment(namespace, clusterDeploymentName)
			Expect(k8sClient.Create(ctx, cd)).To(Succeed())

			oacp := utils.NewOpenshiftAssistedControlPlane(namespace, openshiftAssistedControlPlaneName)
			oacp.Spec.DistributionVersion = openShiftVersion
			Expect(k8sClient.Create(ctx, oacp)).To(Succeed())

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

			cd := utils.NewClusterDeployment(namespace, clusterDeploymentName)
			Expect(k8sClient.Create(ctx, cd)).To(Succeed())

			enableOn := models.DiskEncryptionEnableOnAll
			mode := models.DiskEncryptionModeTang
			oacp := utils.NewOpenshiftAssistedControlPlane(namespace, openshiftAssistedControlPlaneName)
			oacp.Spec.DistributionVersion = openShiftVersion
			oacp.Spec.Config.SSHAuthorizedKey = "mykey"
			oacp.Spec.Config.DiskEncryption = &hiveext.DiskEncryption{
				EnableOn:    &enableOn,
				Mode:        &mode,
				TangServers: " [{\"url\":\"http://tang.example.com:7500\",\"thumbprint\":\"PLjNyRdGw03zlRoGjQYMahSZGu9\"}, {\"url\":\"http://tang.example.com:7501\",\"thumbprint\":\"PLjNyRdGw03zlRoGjQYMahSZGu8\"}]",
			}
			oacp.Spec.Config.Proxy = &hiveext.Proxy{
				HTTPProxy: "https://example.com",
			}
			oacp.Spec.Config.MastersSchedulable = true

			Expect(controllerutil.SetOwnerReference(cluster, oacp, testScheme)).To(Succeed())
			Expect(controllerutil.SetOwnerReference(oacp, cd, testScheme)).To(Succeed())
			ref, _ := reference.GetReference(testScheme, cd)
			oacp.Status.ClusterDeploymentRef = ref
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

			clusterImageSet := &hivev1.ClusterImageSet{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cd.Name}, clusterImageSet)).To(Succeed())
			Expect(clusterImageSet.Spec.ReleaseImage).To(Equal("quay.io/openshift-release-dev/ocp-release:4.16.0"))
		})
		When("ACP with ingressVIPs and apiVIPs", func() {
			It("should start a multinode cluster install", func() {
				cluster := utils.NewCluster(clusterName, namespace)
				Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

				cd := utils.NewClusterDeployment(namespace, clusterDeploymentName)

				acp := utils.NewOpenshiftAssistedControlPlane(namespace, openshiftAssistedControlPlaneName)
				acp.Spec.DistributionVersion = openShiftVersion
				apiVIPs := []string{"1.2.3.4", "2.3.4.5"}
				ingressVIPs := []string{"9.9.9.9", "10.10.10.10"}
				acp.Spec.Config.APIVIPs = apiVIPs
				acp.Spec.Config.IngressVIPs = ingressVIPs

				Expect(controllerutil.SetOwnerReference(cluster, acp, testScheme)).To(Succeed())
				Expect(controllerutil.SetOwnerReference(acp, cd, testScheme)).To(Succeed())
				ref, _ := reference.GetReference(testScheme, cd)
				acp.Status.ClusterDeploymentRef = ref
				Expect(k8sClient.Create(ctx, acp)).To(Succeed())
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
				Expect(aci.Annotations).To(HaveKey(InstallConfigOverrides))
				Expect(aci.Annotations[InstallConfigOverrides]).To(Equal(`{"capabilities": {"baselineCapabilitySet": "None", "additionalEnabledCapabilities": ["baremetal","Console","Insights","OperatorLifecycleManager","Ingress"]}}"`))
			})
		})
	})
	AfterEach(func() {
		k8sClient = nil
		controllerReconciler = nil
	})
})
