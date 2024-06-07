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
	testutils "github.com/openshift-assisted/cluster-api-agent/test/utils"
	v1beta12 "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	"github.com/openshift/assisted-service/api/v1beta1"
	v1 "github.com/openshift/hive/apis/hive/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	bootstrapv1alpha1 "github.com/openshift-assisted/cluster-api-agent/bootstrap/api/v1alpha1"
)

const (
	agentName                 = "test-agent"
	abcName                   = "test-resource"
	bmhName                   = "test-bmh"
	namespace                 = "test-namespace"
	clusterName               = "test-cluster"
	clusterDeploymentName     = "test-clusterdeployment"
	machineName               = "test-resource"
	metal3MachineName         = "test-m3machine"
	acpName                   = "test-controlplane"
	metal3MachineTemplateName = "test-m3machinetemplate"
	infraEnvName              = "test-infraenv"
)

var _ = Describe("AgentBootstrapConfigSpec Controller", func() {
	Context("When reconciling a resource", func() {
		ctx := context.Background()
		var controllerReconciler *AgentBootstrapConfigReconciler
		var k8sClient client.Client

		BeforeEach(func() {
			k8sClient = fakeclient.NewClientBuilder().WithScheme(testScheme).
				WithStatusSubresource(&bootstrapv1alpha1.AgentBootstrapConfig{}).
				Build()
			Expect(k8sClient).NotTo(BeNil())

			controllerReconciler = &AgentBootstrapConfigReconciler{
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
		When("AgentBootstrapConfig has no owner", func() {
			It("should successfully reconcile with NOOP", func() {
				abc := NewAgentBootstrapConfig(namespace, abcName, clusterName)
				Expect(k8sClient.Create(ctx, abc)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(abc),
				})
				Expect(err).NotTo(HaveOccurred())

				// This config has no owner, should exit before setting conditions
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(abc), abc)).To(Succeed())
				condition := conditions.Get(abc,
					bootstrapv1alpha1.DataSecretAvailableCondition,
				)
				Expect(condition).To(BeNil())
			})
		})
		When("AgentBootstrapConfig has a non-relevant owner", func() {
			It("should successfully reconcile", func() {
				abc := NewAgentBootstrapConfig(namespace, abcName, clusterName)
				abc.OwnerReferences = []metav1.OwnerReference{
					{
						APIVersion: "madeup-version",
						Kind:       "madeup-kind",
						Name:       "madeup-name",
						UID:        "madeup-uid",
					},
				}
				Expect(k8sClient.Create(ctx, abc)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(abc),
				})
				Expect(err).NotTo(HaveOccurred())

				// This config has no relevant owner, should exit before setting conditions
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(abc), abc)).To(Succeed())
				condition := conditions.Get(abc,
					bootstrapv1alpha1.DataSecretAvailableCondition,
				)
				Expect(condition).To(BeNil())
			})
		})
		When("AgentBootstrapConfig has no cluster label", func() {
			It("should error when AgentBootstrapConfig has no cluster label", func() {
				abc := setupControlPlaneAgentBootstrapConfig(ctx, k8sClient)
				mockControlPlaneInitialization(ctx, k8sClient)
				// remove cluster label
				delete(abc.Labels, clusterv1.ClusterNameLabel)
				Expect(k8sClient.Update(ctx, abc)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(abc),
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("cluster name label not found in config"))

				// This config has no relevant owner, should exit before setting conditions
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(abc), abc)).To(Succeed())
				condition := conditions.Get(abc,
					bootstrapv1alpha1.DataSecretAvailableCondition,
				)
				Expect(condition).To(BeNil())
			})
		})
		When("ClusterDeployment and AgentClusterInstall are not created yet", func() {
			It("should requeue the request without errors", func() {
				// Given
				abc := setupControlPlaneAgentBootstrapConfig(ctx, k8sClient)
				// and AgentControlPlane provider did not create CD and ACI

				result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(abc),
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Requeue).To(BeTrue())
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(abc), abc)).To(Succeed())
				Expect(conditions.Get(abc,
					bootstrapv1alpha1.DataSecretAvailableCondition,
				)).To(BeNil())
			})
		})
		When("ClusterDeployment is created but AgentClusterInstall is not", func() {
			It("should requeue the request without errors", func() {
				abc := setupControlPlaneAgentBootstrapConfig(ctx, k8sClient)
				cd := testutils.NewClusterDeploymentWithOwnerCluster(namespace, clusterName, clusterName)
				Expect(k8sClient.Create(ctx, cd)).To(Succeed())
				// but not ACI

				result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(abc),
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Requeue).To(BeTrue())
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(abc), abc)).To(Succeed())
				Expect(conditions.Get(abc,
					bootstrapv1alpha1.DataSecretAvailableCondition,
				)).To(BeNil())
			})
		})
		When("ClusterDeployment and AgentClusterInstall are already created", func() {
			It("should create infraenv with an empty ISO URL", func() {
				abc := setupControlPlaneAgentBootstrapConfig(ctx, k8sClient)
				mockControlPlaneInitialization(ctx, k8sClient)

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(abc),
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(abc), abc)).To(Succeed())
				Expect(conditions.Get(abc,
					bootstrapv1alpha1.DataSecretAvailableCondition,
				)).To(BeNil())
				assertInfraEnvWithEmptyISOURL(ctx, k8sClient, abc)
			})
		})
		When("InfraEnv, ClusterDeployment and AgentClusterInstall are already created but no Metal3Machine is running", func() {
			It("should update metal3MachineTemplate", func() {
				abc := setupControlPlaneAgentBootstrapConfig(ctx, k8sClient)
				mockControlPlaneInitialization(ctx, k8sClient)

				// InfraEnv and AgentBootstrapConfig is already updated by InfraEnv controller
				Expect(k8sClient.Create(ctx, testutils.NewInfraEnv(namespace, infraEnvName))).To(Succeed())
				expectedDiskFormat := "live-iso"
				expectedISODownloadURL := "https://example.com/download-my-iso"
				abc.Status.ISODownloadURL = expectedISODownloadURL
				Expect(k8sClient.Status().Update(ctx, abc)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(abc),
				})
				Expect(err).NotTo(HaveOccurred())
				assertISOURLOnM3Template(ctx, k8sClient, expectedISODownloadURL, expectedDiskFormat)
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(abc), abc)).To(Succeed())
				assertBootstrapReady(abc)
			})
		})
		When("InfraEnv, ClusterDeployment and AgentClusterInstall are already created and Metal3Machine is running", func() {
			It("should update metal3MachineTemplate", func() {
				abc := setupControlPlaneAgentBootstrapConfigWithMetal3Machine(ctx, k8sClient)
				mockControlPlaneInitialization(ctx, k8sClient)
				// InfraEnv and AgentBootstrapConfig is already updated by InfraEnv controller
				Expect(k8sClient.Create(ctx, testutils.NewInfraEnv(namespace, infraEnvName))).To(Succeed())
				expectedDiskFormat := "live-iso"
				expectedISODownloadURL := "https://example.com/download-my-iso"
				// Simulate InfraEnv controller updating ISO to config
				abc.Status.ISODownloadURL = expectedISODownloadURL
				Expect(k8sClient.Status().Update(ctx, abc)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(abc),
				})
				Expect(err).NotTo(HaveOccurred())
				assertISOURLOnM3Template(ctx, k8sClient, expectedISODownloadURL, expectedDiskFormat)
				assertISOURLOnM3Machine(ctx, k8sClient, expectedISODownloadURL, expectedDiskFormat)
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(abc), abc)).To(Succeed())
				assertBootstrapReady(abc)
			})
		})
	})
})

func assertISOURLOnM3Template(ctx context.Context, k8sClient client.Client, expectedISODownloadURL, expectedDiskFormat string) {
	m3Template := testutils.NewM3MachineTemplate(namespace, metal3MachineTemplateName)
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(m3Template), m3Template)).To(Succeed())
	Expect(m3Template.Spec.Template.Spec.Image.URL).To(Equal(expectedISODownloadURL))
	Expect(m3Template.Spec.Template.Spec.Image.DiskFormat).To(Equal(&expectedDiskFormat))
}

func assertISOURLOnM3Machine(ctx context.Context, k8sClient client.Client, expectedISODownloadURL, expectedDiskFormat string) {
	m3Machine := testutils.NewMetal3Machine(namespace, metal3MachineName)
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(m3Machine), m3Machine)).To(Succeed())
	Expect(m3Machine.Spec.Image.URL).To(Equal(expectedISODownloadURL))
	Expect(m3Machine.Spec.Image.DiskFormat).To(Equal(&expectedDiskFormat))
}
func assertBootstrapReady(abc *bootstrapv1alpha1.AgentBootstrapConfig) {
	Expect(conditions.IsTrue(abc, bootstrapv1alpha1.DataSecretAvailableCondition)).To(BeTrue())
	Expect(abc.Status.Ready).To(BeTrue())
	Expect(*abc.Status.DataSecretName).NotTo(BeNil())
}

func assertInfraEnvWithEmptyISOURL(ctx context.Context, k8sClient client.Client, abc *bootstrapv1alpha1.AgentBootstrapConfig) {
	infraEnvList := &v1beta1.InfraEnvList{}
	Expect(k8sClient.List(ctx, infraEnvList, client.MatchingLabels{bootstrapv1alpha1.AgentBootstrapConfigLabel: abcName})).To(Succeed())
	Expect(len(infraEnvList.Items)).To(Equal(1))
	infraEnv := infraEnvList.Items[0]
	Expect(abc.Status.InfraEnvRef).ToNot(BeNil())
	Expect(infraEnv.Name).To(Equal(abc.Status.InfraEnvRef.Name))
	Expect(infraEnv.Status.ISODownloadURL).To(Equal(""))
	Expect(abc.Status.ISODownloadURL).To(Equal(""))
}

// mock controlplane provider generating ACI and CD
func mockControlPlaneInitialization(ctx context.Context, k8sClient client.Client) {
	cd := testutils.NewClusterDeploymentWithOwnerCluster(namespace, clusterName, clusterName)
	Expect(k8sClient.Create(ctx, cd)).To(Succeed())

	aci := testutils.NewAgentClusterInstall(clusterName, namespace, clusterName)
	Expect(k8sClient.Create(ctx, aci)).To(Succeed())

	crossReferenceACIAndCD(ctx, k8sClient, aci, cd)
}

func setupControlPlaneAgentBootstrapConfigWithMetal3Machine(ctx context.Context, k8sClient client.Client) *bootstrapv1alpha1.AgentBootstrapConfig {
	abc := setupControlPlaneAgentBootstrapConfig(ctx, k8sClient)
	m3Machine := testutils.NewMetal3Machine(namespace, metal3MachineName)
	Expect(k8sClient.Create(ctx, m3Machine)).To(Succeed())
	machine := &clusterv1.Machine{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      machineName,
	}, machine)).To(Succeed())
	gvk := m3Machine.GroupVersionKind()
	machine.Spec.InfrastructureRef = corev1.ObjectReference{
		Kind:       gvk.Kind,
		Namespace:  m3Machine.Namespace,
		Name:       m3Machine.Name,
		UID:        m3Machine.UID,
		APIVersion: gvk.GroupVersion().String(),
	}
	Expect(k8sClient.Update(ctx, machine)).To(Succeed())
	return abc
}

func setupControlPlaneAgentBootstrapConfig(ctx context.Context, k8sClient client.Client) *bootstrapv1alpha1.AgentBootstrapConfig {
	cluster := testutils.NewCluster(clusterName, namespace)
	Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

	m3Template := testutils.NewM3MachineTemplateWithImage(namespace, metal3MachineTemplateName, "https://example.com/abcd", "qcow2")
	Expect(k8sClient.Create(ctx, m3Template)).To(Succeed())
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(m3Template), m3Template)).To(Succeed())

	acp := testutils.NewAgentControlPlane(namespace, acpName, m3Template)
	Expect(k8sClient.Create(ctx, acp)).Should(Succeed())
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(acp), acp)).To(Succeed())

	machine := testutils.NewMachineWithOwner(namespace, machineName, clusterName, acp)
	Expect(k8sClient.Create(ctx, machine)).To(Succeed())
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(machine), machine)).To(Succeed())

	abc := NewAgentBootstrapConfigWithOwner(namespace, abcName, clusterName, machine)
	Expect(k8sClient.Create(ctx, abc)).To(Succeed())
	return abc
}

func NewAgentBootstrapConfigWithOwner(namespace, name, clusterName string, owner client.Object) *bootstrapv1alpha1.AgentBootstrapConfig {
	ownerGVK := owner.GetObjectKind().GroupVersionKind()
	ownerRefs := []metav1.OwnerReference{
		{
			APIVersion: ownerGVK.GroupVersion().String(),
			Kind:       ownerGVK.Kind,
			Name:       owner.GetName(),
			UID:        owner.GetUID(),
		},
	}
	abc := NewAgentBootstrapConfig(namespace, name, clusterName)
	abc.OwnerReferences = ownerRefs
	return abc
}

func NewAgentBootstrapConfig(namespace, name, clusterName string) *bootstrapv1alpha1.AgentBootstrapConfig {
	return &bootstrapv1alpha1.AgentBootstrapConfig{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:         clusterName,
				clusterv1.MachineControlPlaneLabel: "control-plane",
			},
			Name:      name,
			Namespace: namespace,
		},
	}
}

func crossReferenceACIAndCD(ctx context.Context, k8sClient client.Client, aci *v1beta12.AgentClusterInstall, cd *v1.ClusterDeployment) {
	cd.Spec.ClusterInstallRef = &v1.ClusterInstallLocalReference{
		Group:   aci.GroupVersionKind().Group,
		Version: aci.GroupVersionKind().Version,
		Kind:    aci.Kind,
		Name:    aci.Name,
	}
	Expect(k8sClient.Update(ctx, cd)).To(Succeed())
	aci.Spec.ClusterDeploymentRef.Name = cd.Name
	Expect(k8sClient.Update(ctx, aci)).To(Succeed())
}
