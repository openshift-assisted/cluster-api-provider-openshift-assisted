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
	"github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1alpha1"
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
	agentControlPlaneName = "test-resource"
	clusterDeploymentName = "test-clusterdeployment"
	namespace             = "test"
	clusterName           = "test-cluster"
)

var _ = Describe("AgentClusterInstall Controller", func() {
	ctx := context.Background()
	var controllerReconciler *ClusterDeploymentReconciler
	var k8sClient client.Client

	BeforeEach(func() {
		k8sClient = fakeclient.NewClientBuilder().WithScheme(testScheme).
			WithStatusSubresource(&hivev1.ClusterDeployment{}, &v1alpha1.AgentControlPlane{}).
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
	When("A cluster deployment with no AgentControlPlanes in the same namespace", func() {
		It("should not return error", func() {
			cd := utils.NewClusterDeployment(namespace, clusterDeploymentName)
			Expect(k8sClient.Create(ctx, cd)).To(Succeed())
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(cd),
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	When("A cluster deployment with AgentControlPlanes in the same namespace, but none referecing it", func() {
		It("should not return error", func() {
			cd := utils.NewClusterDeployment(namespace, clusterDeploymentName)
			Expect(k8sClient.Create(ctx, cd)).To(Succeed())

			acp := utils.NewAgentControlPlane(namespace, agentControlPlaneName)
			Expect(k8sClient.Create(ctx, acp)).To(Succeed())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(cd),
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	When("A cluster deployment with AgentControlPlanes in the same namespace referecing it", func() {
		It("should not return error", func() {
			cluster := utils.NewCluster(clusterName, namespace)
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			cd := utils.NewClusterDeployment(namespace, clusterDeploymentName)
			Expect(k8sClient.Create(ctx, cd)).To(Succeed())

			enableOn := models.DiskEncryptionEnableOnAll
			mode := models.DiskEncryptionModeTang
			acp := utils.NewAgentControlPlane(namespace, agentControlPlaneName)
			acp.Spec.AgentConfigSpec.SSHAuthorizedKey = "mykey"
			acp.Spec.AgentConfigSpec.DiskEncryption = &hiveext.DiskEncryption{
				EnableOn:    &enableOn,
				Mode:        &mode,
				TangServers: " [{\"url\":\"http://tang.example.com:7500\",\"thumbprint\":\"PLjNyRdGw03zlRoGjQYMahSZGu9\"}, {\"url\":\"http://tang.example.com:7501\",\"thumbprint\":\"PLjNyRdGw03zlRoGjQYMahSZGu8\"}]",
			}
			acp.Spec.AgentConfigSpec.Proxy = &hiveext.Proxy{
				HTTPProxy: "https://example.com",
			}
			acp.Spec.AgentConfigSpec.MastersSchedulable = true

			Expect(controllerutil.SetOwnerReference(cluster, acp, testScheme)).To(Succeed())
			ref, _ := reference.GetReference(testScheme, cd)
			acp.Status.ClusterDeploymentRef = ref
			Expect(k8sClient.Create(ctx, acp)).To(Succeed())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(cd),
			})
			Expect(err).NotTo(HaveOccurred())

			aci := &hiveext.AgentClusterInstall{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cd), aci)).To(Succeed())

			// Assert exposed ACI fields are derived from ACP
			Expect(aci.Spec.ManifestsConfigMapRefs).To(Equal(acp.Spec.AgentConfigSpec.ManifestsConfigMapRefs))
			Expect(aci.Spec.DiskEncryption).To(Equal(acp.Spec.AgentConfigSpec.DiskEncryption))
			Expect(aci.Spec.Proxy).To(Equal(acp.Spec.AgentConfigSpec.Proxy))
			Expect(aci.Spec.MastersSchedulable).To(Equal(acp.Spec.AgentConfigSpec.MastersSchedulable))
			Expect(aci.Spec.SSHPublicKey).To(Equal(acp.Spec.AgentConfigSpec.SSHAuthorizedKey))
		})
	})
	AfterEach(func() {
		k8sClient = nil
		controllerReconciler = nil
	})
})
