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
	"strings"

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
)

const agentInterfaceMACAddress = "00-B0-D0-63-C2-26"

var _ = Describe("Agent Controller", func() {
	Context("When reconciling a resource", func() {
		ctx := context.Background()
		var (
			controllerReconciler *AgentReconciler
			k8sClient            client.Client
		)
		BeforeEach(func() {
			k8sClient = fakeclient.NewClientBuilder().WithScheme(testScheme).
				WithStatusSubresource(&bootstrapv1alpha1.OpenshiftAssistedConfig{}).
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
		When("an Agent resource exist, but with no interfaces", func() {
			It("should reconcile with an error", func() {
				agent := testutils.NewAgent(namespace, agentName)
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("agent doesn't have inventory yet"))
			})
		})
		When("an Agent resource exists with a machine has no OAC reference", func() {
			It("should reconcile with no errors", func() {
				By("Creating the Agent")
				agent := testutils.NewAgentWithInterface(namespace, agentName, agentInterfaceMACAddress)
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				By("Creating the BMH, Metal3Machine, and Machine")
				bmh := testutils.NewBareMetalHost(namespace, bmhName)
				bmh.Spec.BootMACAddress = agentInterfaceMACAddress
				m3machine := testutils.NewMetal3Machine(namespace, metal3MachineName)
				m3machine.Annotations = map[string]string{
					baremetal.HostAnnotation: strings.Join([]string{namespace, bmhName}, "/"),
				}
				machine := testutils.NewMachine(namespace, machineName, clusterName)
				Expect(controllerutil.SetOwnerReference(m3machine, bmh, testScheme)).To(Succeed())
				Expect(controllerutil.SetOwnerReference(machine, m3machine, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, m3machine)).To(Succeed())
				Expect(k8sClient.Create(ctx, machine)).To(Succeed())
				Expect(k8sClient.Create(ctx, bmh)).To(Succeed())

				By("Reconciling the Agent")
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).NotTo(HaveOccurred())
			})
		})
		When("an Agent resource with a valid Machine reference but the OAC does not exist", func() {
			It("should reconcile with an error", func() {
				By("Creating the Agent")
				agent := testutils.NewAgentWithInterface(namespace, agentName, agentInterfaceMACAddress)
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				By("Creating the BMH, Metal3Machine and Machine")
				bmh := testutils.NewBareMetalHost(namespace, bmhName)
				bmh.Spec.BootMACAddress = agentInterfaceMACAddress
				m3machine := testutils.NewMetal3Machine(namespace, metal3MachineName)
				m3machine.Annotations = map[string]string{
					baremetal.HostAnnotation: strings.Join([]string{namespace, bmhName}, "/"),
				}
				machine := testutils.NewMachine(namespace, machineName, clusterName)
				machine.Spec.Bootstrap.ConfigRef = &corev1.ObjectReference{
					Name:      oacName,
					Namespace: namespace,
				}
				Expect(controllerutil.SetOwnerReference(machine, m3machine, testScheme)).To(Succeed())
				Expect(controllerutil.SetOwnerReference(m3machine, bmh, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, m3machine)).To(Succeed())
				Expect(k8sClient.Create(ctx, machine)).To(Succeed())
				Expect(k8sClient.Create(ctx, bmh)).To(Succeed())

				By("Reconciling the Agent")
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).To(HaveOccurred())
				Expect(
					err.Error(),
				).To(Equal("openshiftassistedconfigs.bootstrap.cluster.x-k8s.io \"test-resource\" not found"))
			})
		})
		When("an Agent resource with a valid Machine with OACs, and reported interfaces", func() {
			It("should return error if BMH is not found", func() {
				By("Creating the OpenshiftAssistedConfig")
				oac := NewOpenshiftAssistedConfig(namespace, oacName, clusterName)
				Expect(k8sClient.Create(ctx, oac)).To(Succeed())

				By("Creating the Agent")
				agent := testutils.NewAgentWithInterface(namespace, agentName, agentInterfaceMACAddress)
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				By("Creating the Metal3Machine, and Machine")
				m3machine := testutils.NewMetal3Machine(namespace, metal3MachineName)
				m3machine.Annotations = map[string]string{
					baremetal.HostAnnotation: strings.Join([]string{namespace, bmhName}, "/"),
				}
				machine := testutils.NewMachine(namespace, machineName, clusterName)
				Expect(controllerutil.SetOwnerReference(machine, m3machine, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, m3machine)).To(Succeed())
				Expect(k8sClient.Create(ctx, machine)).To(Succeed())

				By("Reconciling the Agent")
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).To(HaveOccurred())
				Expect(
					err.Error(),
				).To(Equal("found 0 BMHs, but none matched any of the MacAddresses from the agent's 1 interfaces"))

				By("Checking the results of reconciliation")
				postOAC := &bootstrapv1alpha1.OpenshiftAssistedConfig{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(oac), postOAC)).To(Succeed())
				Expect(postOAC.Status.AgentRef).To(BeNil())
			})
		})
		When("an Agent resource with a valid Machine with OACs, reported interfaces", func() {
			It("should return error if no matching BMH is not found", func() {
				oac := NewOpenshiftAssistedConfig(namespace, oacName, clusterName)
				Expect(k8sClient.Create(ctx, oac)).To(Succeed())

				By("Creating the BMH, Metal3Machine, and Machine")
				bmh := testutils.NewBareMetalHost(namespace, bmhName)
				bmh.Spec.BootMACAddress = "00-B0-D0-63-C1-11"
				m3machine := testutils.NewMetal3Machine(namespace, metal3MachineName)
				m3machine.Annotations = map[string]string{
					baremetal.HostAnnotation: strings.Join([]string{namespace, bmhName}, "/"),
				}
				machine := testutils.NewMachine(namespace, machineName, clusterName)
				Expect(controllerutil.SetOwnerReference(m3machine, bmh, testScheme)).To(Succeed())
				Expect(controllerutil.SetOwnerReference(machine, m3machine, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, m3machine)).To(Succeed())
				Expect(k8sClient.Create(ctx, machine)).To(Succeed())
				Expect(k8sClient.Create(ctx, bmh)).To(Succeed())

				By("Creating the Agent")
				agent := testutils.NewAgentWithInterface(namespace, agentName, agentInterfaceMACAddress)
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).To(HaveOccurred())
				Expect(
					err.Error(),
				).To(Equal("found 1 BMHs, but none matched any of the MacAddresses from the agent's 1 interfaces"))
			})
		})
		When("an Agent resource with a valid Machine with OACs, reported interfaces", func() {
			It("should return error if matching BMH is found, but no metal3machine is found", func() {
				By("Creating the BMH")
				bmh := testutils.NewBareMetalHost(namespace, bmhName)
				bmh.Spec.BootMACAddress = agentInterfaceMACAddress
				Expect(k8sClient.Create(ctx, bmh))

				By("Creating the Agent")
				agent := testutils.NewAgentWithInterface(namespace, agentName, agentInterfaceMACAddress)
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				By("Reconciling the Agent")
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("couldn't find *v1beta1.Metal3Machine owner for *v1alpha1.BareMetalHost"))
			})
		})

		When("an Agent resource with a valid Machine with OACs, reported interfaces", func() {
			It("should return error if matching BMH+metal3machine is found, but no machine is found", func() {
				By("Creating the OpenshiftAssistedConfig")
				oac := NewOpenshiftAssistedConfig(namespace, oacName, clusterName)
				Expect(k8sClient.Create(ctx, oac)).To(Succeed())

				By("Creating the BMH and Metal3Machine")
				bmh := testutils.NewBareMetalHost(namespace, bmhName)
				bmh.Spec.BootMACAddress = agentInterfaceMACAddress
				m3machine := testutils.NewMetal3Machine(namespace, metal3MachineName)
				m3machine.Annotations = map[string]string{
					baremetal.HostAnnotation: strings.Join([]string{namespace, bmhName}, "/"),
				}
				Expect(controllerutil.SetOwnerReference(m3machine, bmh, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, m3machine)).To(Succeed())
				Expect(k8sClient.Create(ctx, bmh)).To(Succeed())

				By("Creating the Agent")
				agent := testutils.NewAgentWithInterface(namespace, agentName, agentInterfaceMACAddress)
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				By("Reconciling the Agent")
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).To(HaveOccurred())
				Expect(
					err.Error(),
				).To(Equal("couldn't find *v1beta1.Machine owner for *v1beta1.Metal3Machine"))
			})
		})

		When("an Agent resource with matching matching BMH, metal3machine, machine (worker)", func() {
			It("should reconcile with a valid accepted worker agent", func() {
				By("Creating the OpenshiftAssistedConfig")
				oac := NewOpenshiftAssistedConfig(namespace, oacName, clusterName)
				Expect(k8sClient.Create(ctx, oac)).To(Succeed())

				By("Creating the BMH, Metal3Machine and Machine")
				bmh := testutils.NewBareMetalHost(namespace, bmhName)
				bmh.Spec.BootMACAddress = agentInterfaceMACAddress
				m3machine := testutils.NewMetal3Machine(namespace, metal3MachineName)
				m3machine.Annotations = map[string]string{
					baremetal.HostAnnotation: strings.Join([]string{namespace, bmhName}, "/"),
				}
				machine := testutils.NewMachine(namespace, machineName, clusterName)
				machine.Spec.Bootstrap.ConfigRef = &corev1.ObjectReference{
					Name:      oacName,
					Namespace: namespace,
				}
				Expect(controllerutil.SetOwnerReference(machine, m3machine, testScheme)).To(Succeed())
				Expect(controllerutil.SetOwnerReference(m3machine, bmh, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, m3machine)).To(Succeed())
				Expect(k8sClient.Create(ctx, machine)).To(Succeed())
				Expect(k8sClient.Create(ctx, bmh)).To(Succeed())

				By("Creating the Agent")
				agent := testutils.NewAgentWithInterface(namespace, agentName, agentInterfaceMACAddress)
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				By("Reconciling the Agent")
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).NotTo(HaveOccurred())

				By("Checking the result of the reconciliation")
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(agent), agent)).To(Succeed())
				assertAgentIsReadyWithRole(agent, bmh, models.HostRoleWorker)
			})
		})
		When("an Agent resource with matching matching BMH, metal3machine, machine (master)", func() {
			It("should reconcile with a valid accepted master agent", func() {
				By("Creating the OpenshiftAssistedConfig")
				oac := NewOpenshiftAssistedConfig(namespace, oacName, clusterName)
				Expect(k8sClient.Create(ctx, oac)).To(Succeed())

				By("Creating the BMH, Metal3Machine and Machine")
				bmh := testutils.NewBareMetalHost(namespace, bmhName)
				bmh.Spec.BootMACAddress = agentInterfaceMACAddress
				m3machine := testutils.NewMetal3Machine(namespace, metal3MachineName)
				m3machine.Annotations = map[string]string{
					baremetal.HostAnnotation: strings.Join([]string{namespace, bmhName}, "/"),
				}
				machine := testutils.NewMachine(namespace, machineName, clusterName)
				machine.Spec.Bootstrap.ConfigRef = &corev1.ObjectReference{
					Name:      oacName,
					Namespace: namespace,
				}
				machine.Labels[clusterv1.MachineControlPlaneLabel] = "control-plane"
				Expect(controllerutil.SetOwnerReference(machine, m3machine, testScheme)).To(Succeed())
				Expect(controllerutil.SetOwnerReference(m3machine, bmh, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, m3machine)).To(Succeed())
				Expect(k8sClient.Create(ctx, machine)).To(Succeed())
				Expect(k8sClient.Create(ctx, bmh)).To(Succeed())

				By("Creating the Agent")
				agent := testutils.NewAgentWithInterface(namespace, agentName, agentInterfaceMACAddress)
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				By("Reconciling the Agent")
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).NotTo(HaveOccurred())

				By("Checking the result of the reconciliation")
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(agent), agent)).To(Succeed())
				assertAgentIsReadyWithRole(agent, bmh, models.HostRoleMaster)
				postOAC := &bootstrapv1alpha1.OpenshiftAssistedConfig{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(oac), postOAC)).To(Succeed())
				Expect(postOAC.Status.AgentRef).NotTo(BeNil())
				Expect(postOAC.Status.AgentRef.Name).To(Equal(agent.Name))
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
