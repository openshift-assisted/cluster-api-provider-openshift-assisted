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
		When("No agent resources exists", func() {
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
		When("an Agent resource exists, but with no infraenv", func() {
			It("should reconcile with an error", func() {
				agent := testutils.NewAgent(namespace, agentName)
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("no infraenvs.agent-install.openshift.io label on Agent test-namespace/test-agent"))
			})
		})
		When("an Agent resource exists with an infraEnv, but with no machine owner", func() {
			It("should reconcile with an error", func() {
				agent := testutils.NewAgentWithInfraEnvLabel(namespace, agentName, infraEnvName)
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				By("Creating InfraEnv")
				infraEnv := testutils.NewInfraEnv(namespace, infraEnvName)
				Expect(k8sClient.Create(ctx, infraEnv)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("couldn't find *v1beta1.Machine owner for *v1beta1.InfraEnv"))
			})
		})
		When("an Agent resource exists with an infraenv, but machine has no OAC reference", func() {
			It("should reconcile with no errors", func() {
				By("Creating the Agent")
				agent := testutils.NewAgentWithInfraEnvLabel(namespace, agentName, machineName)
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())
				By("Creating InfraEnv")
				infraEnv := testutils.NewInfraEnv(namespace, machineName)

				machine := testutils.NewMachine(namespace, machineName, clusterName)
				Expect(controllerutil.SetOwnerReference(machine, infraEnv, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, machine)).To(Succeed())
				Expect(k8sClient.Create(ctx, infraEnv)).To(Succeed())

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
				agent := testutils.NewAgentWithInfraEnvLabel(namespace, agentName, machineName)
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())
				By("Creating InfraEnv")
				infraEnv := testutils.NewInfraEnv(namespace, machineName)

				machine := testutils.NewMachine(namespace, machineName, clusterName)
				machine.Spec.Bootstrap.ConfigRef = &corev1.ObjectReference{
					Name:      oacName,
					Namespace: namespace,
				}
				Expect(controllerutil.SetOwnerReference(machine, infraEnv, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, machine)).To(Succeed())
				Expect(k8sClient.Create(ctx, infraEnv)).To(Succeed())

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
		When("an Agent resource with a valid Machine with OACs, and no agent ref", func() {
			It("should add the agent ref to OAC status", func() {
				By("Creating the OpenshiftAssistedConfig")
				oac := NewOpenshiftAssistedConfig(namespace, oacName, clusterName)
				Expect(k8sClient.Create(ctx, oac)).To(Succeed())

				agent := testutils.NewAgentWithInfraEnvLabel(namespace, agentName, machineName)
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				infraEnv := testutils.NewInfraEnv(namespace, machineName)
				machine := testutils.NewMachine(namespace, machineName, clusterName)
				machine.Spec.Bootstrap.ConfigRef = &corev1.ObjectReference{
					Name:      oacName,
					Namespace: namespace,
				}
				Expect(controllerutil.SetOwnerReference(machine, infraEnv, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, machine)).To(Succeed())
				Expect(k8sClient.Create(ctx, infraEnv)).To(Succeed())

				By("Reconciling the Agent")
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).NotTo(HaveOccurred())
				expectedOAC := bootstrapv1alpha1.OpenshiftAssistedConfig{}
				Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: oacName}, &expectedOAC)).To(Succeed())
				Expect(expectedOAC.Status.AgentRef).NotTo(BeNil())
				Expect(expectedOAC.Status.AgentRef.Name).To(Equal(agentName))
			})
		})
		When("an Agent resource with a valid Machine with OACs, and agent ref", func() {
			It("should add the agent ref to OAC status", func() {

				agent := testutils.NewAgentWithInfraEnvLabel(namespace, agentName, machineName)
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				By("Creating the OpenshiftAssistedConfig")
				oac := NewOpenshiftAssistedConfig(namespace, oacName, clusterName)
				oac.Status.AgentRef = &corev1.LocalObjectReference{Name: agent.Name}
				Expect(k8sClient.Create(ctx, oac)).To(Succeed())

				infraEnv := testutils.NewInfraEnv(namespace, machineName)
				machine := testutils.NewMachine(namespace, machineName, clusterName)
				machine.Spec.Bootstrap.ConfigRef = &corev1.ObjectReference{
					Name:      oacName,
					Namespace: namespace,
				}
				Expect(controllerutil.SetOwnerReference(machine, infraEnv, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, machine)).To(Succeed())
				Expect(k8sClient.Create(ctx, infraEnv)).To(Succeed())

				By("Reconciling the Agent")
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).NotTo(HaveOccurred())
				expectedOAC := bootstrapv1alpha1.OpenshiftAssistedConfig{}
				Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: oacName}, &expectedOAC)).To(Succeed())
				Expect(expectedOAC.Status.AgentRef).NotTo(BeNil())
				Expect(expectedOAC.Status.AgentRef.Name).To(Equal(agentName))
			})
		})

		When("an Agent resource with matching InfraEnv, and machine (worker)", func() {
			It("should reconcile with a valid accepted worker agent", func() {
				By("Creating the OpenshiftAssistedConfig")
				oac := NewOpenshiftAssistedConfig(namespace, oacName, clusterName)
				Expect(k8sClient.Create(ctx, oac)).To(Succeed())

				machine := testutils.NewMachine(namespace, machineName, clusterName)
				machine.Spec.Bootstrap.ConfigRef = &corev1.ObjectReference{
					Name:      oacName,
					Namespace: namespace,
				}
				Expect(k8sClient.Create(ctx, machine)).To(Succeed())

				By("Creating the matching InfraEnv")
				infraEnv := testutils.NewInfraEnv(namespace, machineName)
				Expect(controllerutil.SetOwnerReference(machine, infraEnv, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, infraEnv)).To(Succeed())

				By("Creating the Agent")
				agent := testutils.NewAgentWithInfraEnvLabel(namespace, agentName, machineName)
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				By("Reconciling the Agent")
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).NotTo(HaveOccurred())

				By("Checking the result of the reconciliation")
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(agent), agent)).To(Succeed())
				assertAgentIsReadyWithRole(agent, models.HostRoleWorker)
			})
		})
		When("an Agent resource with matching InfraEnv, and machine (master)", func() {
			It("should reconcile with a valid accepted master agent", func() {
				By("Creating the OpenshiftAssistedConfig")
				oac := NewOpenshiftAssistedConfig(namespace, oacName, clusterName)
				Expect(k8sClient.Create(ctx, oac)).To(Succeed())

				machine := testutils.NewMachine(namespace, machineName, clusterName)
				machine.Spec.Bootstrap.ConfigRef = &corev1.ObjectReference{
					Name:      oacName,
					Namespace: namespace,
				}
				machine.Labels[clusterv1.MachineControlPlaneLabel] = "control-plane"
				Expect(k8sClient.Create(ctx, machine)).To(Succeed())

				By("Creating the matching InfraEnv")
				infraEnv := testutils.NewInfraEnv(namespace, machineName)
				Expect(controllerutil.SetOwnerReference(machine, infraEnv, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, infraEnv)).To(Succeed())

				By("Creating the Agent")
				agent := testutils.NewAgentWithInfraEnvLabel(namespace, agentName, machineName)
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				By("Reconciling the Agent")
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).NotTo(HaveOccurred())

				By("Checking the result of the reconciliation")
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(agent), agent)).To(Succeed())
				assertAgentIsReadyWithRole(agent, models.HostRoleMaster)
				postOAC := &bootstrapv1alpha1.OpenshiftAssistedConfig{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(oac), postOAC)).To(Succeed())
				Expect(postOAC.Status.AgentRef).NotTo(BeNil())
				Expect(postOAC.Status.AgentRef.Name).To(Equal(agent.Name))
			})
		})
	})
})

func assertAgentIsReadyWithRole(agent *v1beta1.Agent, role models.HostRole) {
	Expect(agent.Spec.Role).To(Equal(role))
	Expect(agent.Spec.IgnitionConfigOverrides).NotTo(BeEmpty())
	Expect(agent.Spec.Approved).To(BeTrue())
}
