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
	"fmt"
	"github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/metal3-io/cluster-api-provider-metal3/baremetal"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	bootstrapv1alpha1 "github.com/openshift-assisted/cluster-api-agent/bootstrap/api/v1alpha1"
	testutils "github.com/openshift-assisted/cluster-api-agent/test/utils"
	"github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/models"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"strings"
	"time"
)

const agentInterfaceMACAddress = "00-B0-D0-63-C2-26"

var _ = Describe("InfraEnv Controller", func() {
	Context("When reconciling a resource", func() {
		ctx := context.Background()
		var controllerReconciler *AgentReconciler
		var k8sClient client.Client

		BeforeEach(func() {
			k8sClient = fakeclient.NewClientBuilder().WithScheme(testScheme).
				WithStatusSubresource(&bootstrapv1alpha1.AgentBootstrapConfig{}).
				Build()
			Expect(k8sClient).NotTo(BeNil())

			controllerReconciler = &AgentReconciler{
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
		When("No agent resources exist", func() {
			It("should reconcile with no errors", func() {
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      agentName,
						Namespace: namespace,
					},
				})
				Expect(err).NotTo(HaveOccurred())
			})
		})
		When("an Agent resource exist, but with no ClusterDeployment reference", func() {
			It("should reconcile with no errors", func() {

				agent := testutils.NewAgent(namespace, agentName)
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).NotTo(HaveOccurred())
			})
		})
		When("an Agent resource with a ClusterDeployment reference that points to nowhere", func() {
			It("should reconcile with no errors", func() {

				cd := testutils.NewClusterDeployment(namespace, clusterDeploymentName)
				// do not create ClusterDeployment

				agent := testutils.NewAgentWithClusterDeploymentReference(namespace, agentName, *cd)
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).NotTo(HaveOccurred())
			})
		})
		When("an Agent resource with a valid ClusterDeployment reference, but not belonging to any CAPI cluster", func() {
			It("should reconcile with errors", func() {
				cd := testutils.NewClusterDeployment(namespace, clusterDeploymentName)
				Expect(k8sClient.Create(ctx, cd)).To(Succeed())

				agent := testutils.NewAgentWithClusterDeploymentReference(namespace, agentName, *cd)
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("clusterdeployment test-clusterdeployment does not belong to a CAPI cluster"))
			})
		})
		When("an Agent resource with a valid CAPI-controlled ClusterDeployment reference but machine has no ABC reference", func() {
			It("should return error", func() {
				cd := testutils.NewClusterDeployment(namespace, clusterDeploymentName)
				cd.Labels = map[string]string{
					clusterv1.ClusterNameLabel: clusterName,
				}
				Expect(k8sClient.Create(ctx, cd)).To(Succeed())

				agent := testutils.NewAgentWithClusterDeploymentReference(namespace, agentName, *cd)
				agent.Status.Inventory.Interfaces = []v1beta1.HostInterface{
					{
						Name:       "dummy",
						MacAddress: agentInterfaceMACAddress,
					},
				}

				Expect(k8sClient.Create(ctx, agent)).To(Succeed())
				bmh := testutils.NewBareMetalHost(namespace, bmhName)
				bmh.Spec.BootMACAddress = agentInterfaceMACAddress
				Expect(k8sClient.Create(ctx, bmh))

				m3machine := testutils.NewMetal3Machine(namespace, metal3MachineName)
				m3machine.Annotations = map[string]string{
					baremetal.HostAnnotation: strings.Join([]string{namespace, bmhName}, "/"),
				}

				machine := testutils.NewMachine(namespace, machineName, clusterName)
				Expect(controllerutil.SetOwnerReference(machine, m3machine, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, m3machine))
				Expect(k8sClient.Create(ctx, machine))

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("machine test-namespace/test-resource does not have any bootstrap config ref"))
			})
		})
		When("an Agent resource with a valid CAPI-controlled ClusterDeployment reference but no ABCs", func() {
			It("should return error", func() {
				cd := testutils.NewClusterDeployment(namespace, clusterDeploymentName)
				cd.Labels = map[string]string{
					clusterv1.ClusterNameLabel: clusterName,
				}
				Expect(k8sClient.Create(ctx, cd)).To(Succeed())

				agent := testutils.NewAgentWithClusterDeploymentReference(namespace, agentName, *cd)
				agent.Status.Inventory.Interfaces = []v1beta1.HostInterface{
					{
						Name:       "dummy",
						MacAddress: agentInterfaceMACAddress,
					},
				}

				Expect(k8sClient.Create(ctx, agent)).To(Succeed())
				bmh := testutils.NewBareMetalHost(namespace, bmhName)
				bmh.Spec.BootMACAddress = agentInterfaceMACAddress
				Expect(k8sClient.Create(ctx, bmh))

				m3machine := testutils.NewMetal3Machine(namespace, metal3MachineName)
				m3machine.Annotations = map[string]string{
					baremetal.HostAnnotation: strings.Join([]string{namespace, bmhName}, "/"),
				}

				machine := testutils.NewMachine(namespace, machineName, clusterName)
				machine.Spec.Bootstrap.ConfigRef = &corev1.ObjectReference{
					Name:      abcName,
					Namespace: namespace,
				}
				Expect(controllerutil.SetOwnerReference(machine, m3machine, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, m3machine))
				Expect(k8sClient.Create(ctx, machine))

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("agentbootstrapconfigs.bootstrap.cluster.x-k8s.io \"test-resource\" not found"))
			})
		})
		When("an Agent resource with a valid CAPI-controlled ClusterDeployment with ABCs, but no reported interface yet", func() {
			It("should not return error", func() {
				abc := NewAgentBootstrapConfig(namespace, abcName, clusterName)
				Expect(k8sClient.Create(ctx, abc)).To(Succeed())

				cd := testutils.NewClusterDeployment(namespace, clusterDeploymentName)
				cd.Labels = map[string]string{
					clusterv1.ClusterNameLabel: clusterName,
				}
				Expect(k8sClient.Create(ctx, cd)).To(Succeed())

				agent := testutils.NewAgentWithClusterDeploymentReference(namespace, agentName, *cd)
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				var requeueNotSet time.Duration
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Requeue).To(BeFalse())
				Expect(result.RequeueAfter).To(Equal(requeueNotSet))
			})
		})
		When("an Agent resource with a valid CAPI-controlled ClusterDeployment with ABCs, and reported interfaces", func() {
			It("should return error if BMH is not found", func() {
				abc := NewAgentBootstrapConfig(namespace, abcName, clusterName)
				Expect(k8sClient.Create(ctx, abc)).To(Succeed())

				cd := testutils.NewClusterDeployment(namespace, clusterDeploymentName)
				cd.Labels = map[string]string{
					clusterv1.ClusterNameLabel: clusterName,
				}
				Expect(k8sClient.Create(ctx, cd)).To(Succeed())

				agent := testutils.NewAgentWithClusterDeploymentReference(namespace, agentName, *cd)
				agent.Status.Inventory.Interfaces = []v1beta1.HostInterface{
					{
						Name:       "dummy",
						MacAddress: agentInterfaceMACAddress,
					},
				}
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("found 0 BMHs, and none matched any MacAddress from the agent's 1 interfaces"))
			})
		})
		When("an Agent resource with a valid CAPI-controlled ClusterDeployment with ABCs, reported interfaces", func() {
			It("should return error if no matching BMH is not found", func() {
				abc := NewAgentBootstrapConfig(namespace, abcName, clusterName)
				Expect(k8sClient.Create(ctx, abc)).To(Succeed())
				bmh := testutils.NewBareMetalHost(namespace, bmhName)
				bmh.Spec.BootMACAddress = "00-B0-D0-63-C1-11"
				Expect(k8sClient.Create(ctx, bmh))
				cd := testutils.NewClusterDeployment(namespace, clusterDeploymentName)
				cd.Labels = map[string]string{
					clusterv1.ClusterNameLabel: clusterName,
				}
				Expect(k8sClient.Create(ctx, cd)).To(Succeed())

				agent := testutils.NewAgentWithClusterDeploymentReference(namespace, agentName, *cd)
				agent.Status.Inventory.Interfaces = []v1beta1.HostInterface{
					{
						Name:       "dummy",
						MacAddress: agentInterfaceMACAddress,
					},
				}
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("found 1 BMHs, and none matched any MacAddress from the agent's 1 interfaces"))
			})
		})
		When("an Agent resource with a valid CAPI-controlled ClusterDeployment with ABCs, reported interfaces", func() {
			It("should return error if matching BMH is found, but no metal3machine is found", func() {
				abc := NewAgentBootstrapConfig(namespace, abcName, clusterName)
				Expect(k8sClient.Create(ctx, abc)).To(Succeed())
				bmh := testutils.NewBareMetalHost(namespace, bmhName)
				bmh.Spec.BootMACAddress = agentInterfaceMACAddress
				Expect(k8sClient.Create(ctx, bmh))
				cd := testutils.NewClusterDeployment(namespace, clusterDeploymentName)
				cd.Labels = map[string]string{
					clusterv1.ClusterNameLabel: clusterName,
				}
				Expect(k8sClient.Create(ctx, cd)).To(Succeed())

				agent := testutils.NewAgentWithClusterDeploymentReference(namespace, agentName, *cd)
				agent.Status.Inventory.Interfaces = []v1beta1.HostInterface{
					{
						Name:       "dummy",
						MacAddress: agentInterfaceMACAddress,
					},
				}
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("found 0 metal3machines, none matching BMH test-namespace/test-bmh"))
			})
		})
		When("an Agent resource with a valid CAPI-controlled ClusterDeployment with ABCs, reported interfaces", func() {
			It("should return error if matching BMH is found, but no matching metal3machine is found", func() {
				abc := NewAgentBootstrapConfig(namespace, abcName, clusterName)
				Expect(k8sClient.Create(ctx, abc)).To(Succeed())

				bmh := testutils.NewBareMetalHost(namespace, bmhName)
				bmh.Spec.BootMACAddress = agentInterfaceMACAddress
				Expect(k8sClient.Create(ctx, bmh))

				m3machine := testutils.NewMetal3Machine(namespace, metal3MachineName)
				Expect(k8sClient.Create(ctx, m3machine))

				cd := testutils.NewClusterDeployment(namespace, clusterDeploymentName)
				cd.Labels = map[string]string{
					clusterv1.ClusterNameLabel: clusterName,
				}
				Expect(k8sClient.Create(ctx, cd)).To(Succeed())

				agent := testutils.NewAgentWithClusterDeploymentReference(namespace, agentName, *cd)
				agent.Status.Inventory.Interfaces = []v1beta1.HostInterface{
					{
						Name:       "dummy",
						MacAddress: agentInterfaceMACAddress,
					},
				}
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("found 1 metal3machines, none matching BMH test-namespace/test-bmh"))
			})
		})
		When("an Agent resource with a valid CAPI-controlled ClusterDeployment with ABCs, reported interfaces", func() {
			It("should return error if matching BMH+metal3machine is found, but no machine is found", func() {
				abc := NewAgentBootstrapConfig(namespace, abcName, clusterName)
				Expect(k8sClient.Create(ctx, abc)).To(Succeed())

				bmh := testutils.NewBareMetalHost(namespace, bmhName)
				bmh.Spec.BootMACAddress = agentInterfaceMACAddress
				Expect(k8sClient.Create(ctx, bmh))

				m3machine := testutils.NewMetal3Machine(namespace, metal3MachineName)
				m3machine.Annotations = map[string]string{
					baremetal.HostAnnotation: strings.Join([]string{namespace, bmhName}, "/"),
				}
				Expect(k8sClient.Create(ctx, m3machine))

				cd := testutils.NewClusterDeployment(namespace, clusterDeploymentName)
				cd.Labels = map[string]string{
					clusterv1.ClusterNameLabel: clusterName,
				}
				Expect(k8sClient.Create(ctx, cd)).To(Succeed())

				agent := testutils.NewAgentWithClusterDeploymentReference(namespace, agentName, *cd)
				agent.Status.Inventory.Interfaces = []v1beta1.HostInterface{
					{
						Name:       "dummy",
						MacAddress: agentInterfaceMACAddress,
					},
				}
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal(fmt.Sprintf("no machine found for metal3machine %s/%s", namespace, metal3MachineName)))
			})
		})
		When("an Agent resource with matching matching BMH, metal3machine, machine (worker)", func() {
			It("should reconcile with a valid accepted worker agent", func() {
				abc := NewAgentBootstrapConfig(namespace, abcName, clusterName)
				Expect(k8sClient.Create(ctx, abc)).To(Succeed())

				bmh := testutils.NewBareMetalHost(namespace, bmhName)
				bmh.UID = "foobar"
				bmh.Spec.BootMACAddress = agentInterfaceMACAddress
				Expect(k8sClient.Create(ctx, bmh))

				m3machine := testutils.NewMetal3Machine(namespace, metal3MachineName)
				m3machine.Annotations = map[string]string{
					baremetal.HostAnnotation: strings.Join([]string{namespace, bmhName}, "/"),
				}
				machine := testutils.NewMachine(namespace, machineName, clusterName)
				machine.Spec.Bootstrap.ConfigRef = &corev1.ObjectReference{
					Name:      abc.Name,
					Namespace: abc.Namespace,
				}
				Expect(controllerutil.SetOwnerReference(machine, m3machine, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, m3machine))
				Expect(k8sClient.Create(ctx, machine))

				cd := testutils.NewClusterDeployment(namespace, clusterDeploymentName)
				cd.Labels = map[string]string{
					clusterv1.ClusterNameLabel: clusterName,
				}
				Expect(k8sClient.Create(ctx, cd)).To(Succeed())

				agent := testutils.NewAgentWithClusterDeploymentReference(namespace, agentName, *cd)
				agent.Status.Inventory.Interfaces = []v1beta1.HostInterface{
					{
						Name:       "dummy",
						MacAddress: agentInterfaceMACAddress,
					},
				}
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).NotTo(HaveOccurred())

				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(agent), agent)).To(Succeed())
				assertAgentIsReadyWithRole(agent, bmh, models.HostRoleWorker)
			})
		})
		When("an Agent resource with matching matching BMH, metal3machine, machine (master)", func() {
			It("should reconcile with a valid accepted master agent", func() {
				abc := NewAgentBootstrapConfig(namespace, abcName, clusterName)
				Expect(k8sClient.Create(ctx, abc)).To(Succeed())

				bmh := testutils.NewBareMetalHost(namespace, bmhName)
				bmh.UID = "foobar"
				bmh.Spec.BootMACAddress = agentInterfaceMACAddress
				Expect(k8sClient.Create(ctx, bmh))

				m3machine := testutils.NewMetal3Machine(namespace, metal3MachineName)
				m3machine.Annotations = map[string]string{
					baremetal.HostAnnotation: strings.Join([]string{namespace, bmhName}, "/"),
				}
				machine := testutils.NewMachine(namespace, machineName, clusterName)
				machine.Labels[clusterv1.MachineControlPlaneLabel] = "control-plane"
				machine.Spec.Bootstrap.ConfigRef = &corev1.ObjectReference{
					Name:      abc.Name,
					Namespace: abc.Namespace,
				}
				Expect(controllerutil.SetOwnerReference(machine, m3machine, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, m3machine))
				Expect(k8sClient.Create(ctx, machine))

				cd := testutils.NewClusterDeployment(namespace, clusterDeploymentName)
				cd.Labels = map[string]string{
					clusterv1.ClusterNameLabel: clusterName,
				}
				Expect(k8sClient.Create(ctx, cd)).To(Succeed())

				agent := testutils.NewAgentWithClusterDeploymentReference(namespace, agentName, *cd)
				agent.Status.Inventory.Interfaces = []v1beta1.HostInterface{
					{
						Name:       "dummy",
						MacAddress: agentInterfaceMACAddress,
					},
				}
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).NotTo(HaveOccurred())

				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(agent), agent)).To(Succeed())
				assertAgentIsReadyWithRole(agent, bmh, models.HostRoleMaster)
			})
		})
	})
})

func assertAgentIsReadyWithRole(agent *v1beta1.Agent, bmh *v1alpha1.BareMetalHost, role models.HostRole) {
	Expect(agent.Spec.NodeLabels).To(HaveKeyWithValue(metal3ProviderIDLabelKey, string(bmh.GetUID())))
	Expect(agent.Spec.Role).To(Equal(role))
	Expect(agent.Spec.IgnitionConfigOverrides).NotTo(BeEmpty())
	Expect(agent.Spec.Approved).To(BeTrue())
}
