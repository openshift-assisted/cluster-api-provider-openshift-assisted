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
	"encoding/base64"
	"encoding/json"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	bootstrapv1alpha2 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/bootstrap/api/v1alpha2"
	testutils "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/test/utils"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/models"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
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
				WithStatusSubresource(&bootstrapv1alpha2.OpenshiftAssistedConfig{}).
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
				Expect(err.Error()).To(Equal("couldn't find *v1beta2.Machine owner for *v1beta1.InfraEnv"))
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
				machine.Spec.Bootstrap.ConfigRef = clusterv1.ContractVersionedObjectReference{
					Name: oacName,
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
				oac := testutils.NewOpenshiftAssistedConfig(namespace, oacName, clusterName)
				Expect(k8sClient.Create(ctx, oac)).To(Succeed())

				agent := testutils.NewAgentWithInfraEnvLabel(namespace, agentName, machineName)
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				infraEnv := testutils.NewInfraEnv(namespace, machineName)
				machine := testutils.NewMachine(namespace, machineName, clusterName)
				machine.Spec.Bootstrap.ConfigRef = clusterv1.ContractVersionedObjectReference{
					Name: oacName,
				}
				Expect(controllerutil.SetOwnerReference(machine, infraEnv, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, machine)).To(Succeed())
				Expect(k8sClient.Create(ctx, infraEnv)).To(Succeed())

				By("Reconciling the Agent")
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).NotTo(HaveOccurred())
				expectedOAC := bootstrapv1alpha2.OpenshiftAssistedConfig{}
				Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: oacName}, &expectedOAC)).To(Succeed())

			})
		})
		When("an Agent resource with matching InfraEnv, and machine (worker)", func() {
			It("should reconcile with a valid accepted worker agent", func() {
				By("Creating the OpenshiftAssistedConfig")
				oac := testutils.NewOpenshiftAssistedConfig(namespace, oacName, clusterName)
				Expect(k8sClient.Create(ctx, oac)).To(Succeed())

				machine := testutils.NewMachine(namespace, machineName, clusterName)
				machine.Spec.Bootstrap.ConfigRef = clusterv1.ContractVersionedObjectReference{
					Name: oacName,
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
				oac := testutils.NewOpenshiftAssistedConfig(namespace, oacName, clusterName)
				Expect(k8sClient.Create(ctx, oac)).To(Succeed())

				machine := testutils.NewMachine(namespace, machineName, clusterName)
				machine.Spec.Bootstrap.ConfigRef = clusterv1.ContractVersionedObjectReference{
					Name: oacName,
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
			})
		})

		When("a bootstrap config has configured kernel args", func() {
			It("should reconcile with a valid coreos install args", func() {
				oac := testutils.NewOpenshiftAssistedConfig(namespace, oacName, clusterName)
				oac.Spec.KernelArguments = []aiv1beta1.KernelArgument{
					{
						Operation: "append",
						Value:     "console=tty0 console=ttyS0,115200n8",
					},
					{
						Operation: "append",
						Value:     "rd.neednet=1",
					},
				}
				Expect(k8sClient.Create(ctx, oac)).To(Succeed())

				machine := testutils.NewMachine(namespace, machineName, clusterName)
				machine.Spec.Bootstrap.ConfigRef = clusterv1.ContractVersionedObjectReference{
					Name: oacName,
				}
				machine.Labels[clusterv1.MachineControlPlaneLabel] = "control-plane"
				Expect(k8sClient.Create(ctx, machine)).To(Succeed())

				infraEnv := testutils.NewInfraEnv(namespace, machineName)
				Expect(controllerutil.SetOwnerReference(machine, infraEnv, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, infraEnv)).To(Succeed())

				agent := testutils.NewAgentWithInfraEnvLabel(namespace, agentName, machineName)
				Expect(k8sClient.Create(ctx, agent)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(agent),
				})
				Expect(err).NotTo(HaveOccurred())

				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(agent), agent)).To(Succeed())
				Expect(agent.Spec.InstallerArgs).To(Equal(`["--append-karg","console=tty0 console=ttyS0,115200n8","--append-karg","rd.neednet=1"]`))
			})
		})

		When("an Agent resource exists with another approved agent using the same infraenv", func() {
			It("should not approve the new agent", func() {
				By("Creating first agent with infraenv")
				firstAgent := testutils.NewAgentWithInfraEnvLabel(namespace, "first-agent", machineName)
				firstAgent.Spec.Approved = true
				Expect(k8sClient.Create(ctx, firstAgent)).To(Succeed())

				By("Creating the OpenshiftAssistedConfig")
				oac := testutils.NewOpenshiftAssistedConfig(namespace, oacName, clusterName)
				Expect(k8sClient.Create(ctx, oac)).To(Succeed())

				machine := testutils.NewMachine(namespace, machineName, clusterName)
				machine.Spec.Bootstrap.ConfigRef = clusterv1.ContractVersionedObjectReference{
					Name: oacName,
				}
				Expect(k8sClient.Create(ctx, machine)).To(Succeed())

				By("Creating the matching InfraEnv")
				infraEnv := testutils.NewInfraEnv(namespace, machineName)
				Expect(controllerutil.SetOwnerReference(machine, infraEnv, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, infraEnv)).To(Succeed())

				By("Creating second agent with same infraenv")
				secondAgent := testutils.NewAgentWithInfraEnvLabel(namespace, "second-agent", machineName)
				Expect(k8sClient.Create(ctx, secondAgent)).To(Succeed())

				By("Reconciling the second Agent")
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(secondAgent),
				})
				Expect(err).NotTo(HaveOccurred())

				By("Checking that second agent was not approved")
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(secondAgent), secondAgent)).To(Succeed())
				Expect(secondAgent.Spec.Approved).To(BeFalse())
			})
		})

		When("multiple agents exist with the same infraenv but none are approved", func() {
			It("should approve the first reconciled agent", func() {
				By("Creating first agent with infraenv")
				firstAgent := testutils.NewAgentWithInfraEnvLabel(namespace, "first-agent", machineName)
				Expect(k8sClient.Create(ctx, firstAgent)).To(Succeed())

				By("Creating second agent with same infraenv")
				secondAgent := testutils.NewAgentWithInfraEnvLabel(namespace, "second-agent", machineName)
				Expect(k8sClient.Create(ctx, secondAgent)).To(Succeed())

				By("Creating the OpenshiftAssistedConfig")
				oac := testutils.NewOpenshiftAssistedConfig(namespace, oacName, clusterName)
				Expect(k8sClient.Create(ctx, oac)).To(Succeed())

				machine := testutils.NewMachine(namespace, machineName, clusterName)
				machine.Spec.Bootstrap.ConfigRef = clusterv1.ContractVersionedObjectReference{
					Name: oacName,
				}
				Expect(k8sClient.Create(ctx, machine)).To(Succeed())

				By("Creating the matching InfraEnv")
				infraEnv := testutils.NewInfraEnv(namespace, machineName)
				Expect(controllerutil.SetOwnerReference(machine, infraEnv, testScheme)).To(Succeed())
				Expect(k8sClient.Create(ctx, infraEnv)).To(Succeed())

				By("Reconciling the first Agent")
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(firstAgent),
				})
				Expect(err).NotTo(HaveOccurred())

				By("Checking that first agent was approved")
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(firstAgent), firstAgent)).To(Succeed())
				Expect(firstAgent.Spec.Approved).To(BeTrue())
			})
		})
	})
})

func assertAgentIsReadyWithRole(agent *aiv1beta1.Agent, role models.HostRole) {
	Expect(agent.Spec.Role).To(Equal(role))
	Expect(agent.Spec.IgnitionConfigOverrides).NotTo(BeEmpty())
	Expect(agent.Spec.Approved).To(BeTrue())
}

var _ = Describe("getIgnitionConfig", func() {
	Context("Discovery phase ignition", func() {
		It("should include set-hostname unit when Name is set", func() {
			config := &bootstrapv1alpha2.OpenshiftAssistedConfig{
				Spec: bootstrapv1alpha2.OpenshiftAssistedConfigSpec{
					NodeRegistration: bootstrapv1alpha2.NodeRegistrationOptions{
						Name:               "$METADATA_NAME",
						KubeletExtraLabels: []string{"zone=east"},
					},
				},
			}

			ignitionJSON, err := getIgnitionConfig(config)
			Expect(err).NotTo(HaveOccurred())
			Expect(ignitionJSON).NotTo(BeEmpty())

			// Verify valid ignition JSON
			Expect(ignitionJSON).To(ContainSubstring(`"version":"3.1.0"`))
			// Verify set-hostname.service unit is included in discovery phase
			Expect(ignitionJSON).To(ContainSubstring(`set-hostname.service`))
			Expect(ignitionJSON).To(ContainSubstring(`/usr/local/bin/set_hostname`))
		})

		It("should not include set-hostname unit when Name is empty", func() {
			config := &bootstrapv1alpha2.OpenshiftAssistedConfig{
				Spec: bootstrapv1alpha2.OpenshiftAssistedConfigSpec{
					NodeRegistration: bootstrapv1alpha2.NodeRegistrationOptions{
						KubeletExtraLabels: []string{"zone=east", "env=prod"},
					},
				},
			}

			ignitionJSON, err := getIgnitionConfig(config)
			Expect(err).NotTo(HaveOccurred())
			Expect(ignitionJSON).NotTo(BeEmpty())
			// Verify set-hostname.service unit is NOT included when Name is empty
			Expect(ignitionJSON).NotTo(ContainSubstring(`set-hostname.service`))
			// Verify kubelet custom labels script is included
			Expect(ignitionJSON).To(ContainSubstring(`kubelet_custom_labels`))
		})

		DescribeTable("should handle variable notation in KubeletExtraLabels",
			func(labelValue string) {
				config := &bootstrapv1alpha2.OpenshiftAssistedConfig{
					Spec: bootstrapv1alpha2.OpenshiftAssistedConfigSpec{
						NodeRegistration: bootstrapv1alpha2.NodeRegistrationOptions{
							KubeletExtraLabels: []string{"node-id=" + labelValue},
						},
					},
				}

				ignitionJSON, err := getIgnitionConfig(config)
				Expect(err).NotTo(HaveOccurred())

				var ignConfig map[string]interface{}
				Expect(json.Unmarshal([]byte(ignitionJSON), &ignConfig)).To(Succeed())

				storage := ignConfig["storage"].(map[string]interface{})
				files := storage["files"].([]interface{})

				var scriptContent string
				for _, f := range files {
					file := f.(map[string]interface{})
					if file["path"] == "/usr/local/bin/kubelet_custom_labels" {
						contents := file["contents"].(map[string]interface{})
						source := contents["source"].(string)
						b64Data := strings.TrimPrefix(source, "data:text/plain;charset=utf-8;base64,")
						decoded, err := base64.StdEncoding.DecodeString(b64Data)
						Expect(err).NotTo(HaveOccurred())
						scriptContent = string(decoded)
						break
					}
				}

				// Both $VAR and ${VAR} are normalized to ${VAR} and resolved via grep
				Expect(scriptContent).To(ContainSubstring(`METADATA_UUID=$(/usr/bin/grep "^METADATA_UUID=" /etc/metadata_env`))
				Expect(scriptContent).To(ContainSubstring("node-id=${METADATA_UUID}"))
			},
			Entry("$VAR notation", "$METADATA_UUID"),
			Entry("${VAR} notation", "${METADATA_UUID}"),
		)

		DescribeTable("should reject invalid variable names in KubeletExtraLabels",
			func(labelValue string) {
				config := &bootstrapv1alpha2.OpenshiftAssistedConfig{
					Spec: bootstrapv1alpha2.OpenshiftAssistedConfigSpec{
						NodeRegistration: bootstrapv1alpha2.NodeRegistrationOptions{
							KubeletExtraLabels: []string{"node-id=" + labelValue},
						},
					},
				}

				_, err := getIgnitionConfig(config)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid"))
			},
			Entry("command substitution", "$VAR$(whoami)"),
			Entry("backticks", "$VAR`id`"),
			Entry("starts with digit", "$1VAR"),
			Entry("contains hyphen", "$VAR-NAME"),
			Entry("contains semicolon", "$VAR;echo"),
			Entry("contains space", "$VAR NAME"),
		)

		Context("ignition-override annotation", func() {
			const validOverride = `{"ignition":{"version":"3.1.0"},"storage":{"files":[{"path":"/etc/override-file","contents":{"source":"data:,"},"mode":384}]}}`

			It("should merge valid ignition-override annotation into host ignition", func() {
				config := &bootstrapv1alpha2.OpenshiftAssistedConfig{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							bootstrapv1alpha2.IgnitionOverrideAnnotation: validOverride,
						},
					},
					Spec: bootstrapv1alpha2.OpenshiftAssistedConfigSpec{
						NodeRegistration: bootstrapv1alpha2.NodeRegistrationOptions{
							KubeletExtraLabels: []string{"zone=east"},
						},
					},
				}

				ignitionJSON, err := getIgnitionConfig(config)
				Expect(err).NotTo(HaveOccurred())
				Expect(ignitionJSON).To(ContainSubstring(`/etc/override-file`))
				Expect(ignitionJSON).To(ContainSubstring(`kubelet_custom_labels`))
			})

			It("should ignore annotation when value is not valid JSON", func() {
				config := &bootstrapv1alpha2.OpenshiftAssistedConfig{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							bootstrapv1alpha2.IgnitionOverrideAnnotation: `not-json`,
						},
					},
					Spec: bootstrapv1alpha2.OpenshiftAssistedConfigSpec{
						NodeRegistration: bootstrapv1alpha2.NodeRegistrationOptions{
							KubeletExtraLabels: []string{"zone=east"},
						},
					},
				}

				ignitionJSON, err := getIgnitionConfig(config)
				Expect(err).NotTo(HaveOccurred())
				Expect(ignitionJSON).NotTo(ContainSubstring(`/etc/override-file`))
				Expect(ignitionJSON).To(ContainSubstring(`kubelet_custom_labels`))
			})

			It("should return error when annotation is valid JSON but invalid ignition", func() {
				config := &bootstrapv1alpha2.OpenshiftAssistedConfig{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							// Unknown ignition version so parse fails
							bootstrapv1alpha2.IgnitionOverrideAnnotation: `{"ignition":{"version":"99.0.0"}}`,
						},
					},
					Spec: bootstrapv1alpha2.OpenshiftAssistedConfigSpec{
						NodeRegistration: bootstrapv1alpha2.NodeRegistrationOptions{
							KubeletExtraLabels: []string{"zone=east"},
						},
					},
				}

				_, err := getIgnitionConfig(config)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(bootstrapv1alpha2.IgnitionOverrideAnnotation))
			})
		})

		Context("ProviderID", func() {
			It("should write KUBELET_PROVIDERID with static value", func() {
				config := &bootstrapv1alpha2.OpenshiftAssistedConfig{
					Spec: bootstrapv1alpha2.OpenshiftAssistedConfigSpec{
						NodeRegistration: bootstrapv1alpha2.NodeRegistrationOptions{
							ProviderID: "metal3://my-node-uuid",
						},
					},
				}

				ignitionJSON, err := getIgnitionConfig(config)
				Expect(err).NotTo(HaveOccurred())

				scriptContent := extractKubeletCustomLabelsScript(ignitionJSON)
				Expect(scriptContent).To(ContainSubstring(`KUBELET_PROVIDERID=metal3://my-node-uuid`))
			})

			It("should write KUBELET_PROVIDERID with dynamic value from env var", func() {
				config := &bootstrapv1alpha2.OpenshiftAssistedConfig{
					Spec: bootstrapv1alpha2.OpenshiftAssistedConfigSpec{
						NodeRegistration: bootstrapv1alpha2.NodeRegistrationOptions{
							ProviderID: "$METADATA_UUID",
						},
					},
				}

				ignitionJSON, err := getIgnitionConfig(config)
				Expect(err).NotTo(HaveOccurred())

				scriptContent := extractKubeletCustomLabelsScript(ignitionJSON)
				Expect(scriptContent).To(ContainSubstring(`METADATA_UUID=$(/usr/bin/grep "^METADATA_UUID=" /etc/metadata_env`))
				Expect(scriptContent).To(ContainSubstring(`KUBELET_PROVIDERID=${METADATA_UUID}`))
			})

			It("should write KUBELET_PROVIDERID with ${VAR} notation", func() {
				config := &bootstrapv1alpha2.OpenshiftAssistedConfig{
					Spec: bootstrapv1alpha2.OpenshiftAssistedConfigSpec{
						NodeRegistration: bootstrapv1alpha2.NodeRegistrationOptions{
							ProviderID: "${METADATA_UUID}",
						},
					},
				}

				ignitionJSON, err := getIgnitionConfig(config)
				Expect(err).NotTo(HaveOccurred())

				scriptContent := extractKubeletCustomLabelsScript(ignitionJSON)
				Expect(scriptContent).To(ContainSubstring(`METADATA_UUID=$(/usr/bin/grep "^METADATA_UUID=" /etc/metadata_env`))
				Expect(scriptContent).To(ContainSubstring(`KUBELET_PROVIDERID=${METADATA_UUID}`))
			})

			It("should not write KUBELET_PROVIDERID when ProviderID is empty", func() {
				config := &bootstrapv1alpha2.OpenshiftAssistedConfig{
					Spec: bootstrapv1alpha2.OpenshiftAssistedConfigSpec{
						NodeRegistration: bootstrapv1alpha2.NodeRegistrationOptions{
							KubeletExtraLabels: []string{"zone=east"},
						},
					},
				}

				ignitionJSON, err := getIgnitionConfig(config)
				Expect(err).NotTo(HaveOccurred())

				scriptContent := extractKubeletCustomLabelsScript(ignitionJSON)
				Expect(scriptContent).NotTo(ContainSubstring(`KUBELET_PROVIDERID`))
			})

			It("should reject invalid variable names in ProviderID", func() {
				config := &bootstrapv1alpha2.OpenshiftAssistedConfig{
					Spec: bootstrapv1alpha2.OpenshiftAssistedConfigSpec{
						NodeRegistration: bootstrapv1alpha2.NodeRegistrationOptions{
							ProviderID: "$INVALID;echo",
						},
					},
				}

				_, err := getIgnitionConfig(config)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid"))
			})

			It("should handle ProviderID and KubeletExtraLabels together with shared dynamic var", func() {
				config := &bootstrapv1alpha2.OpenshiftAssistedConfig{
					Spec: bootstrapv1alpha2.OpenshiftAssistedConfigSpec{
						NodeRegistration: bootstrapv1alpha2.NodeRegistrationOptions{
							ProviderID:         "$METADATA_UUID",
							KubeletExtraLabels: []string{"node-id=$METADATA_UUID", "zone=east"},
						},
					},
				}

				ignitionJSON, err := getIgnitionConfig(config)
				Expect(err).NotTo(HaveOccurred())

				scriptContent := extractKubeletCustomLabelsScript(ignitionJSON)
				// Variable should only be resolved once
				count := strings.Count(scriptContent, `METADATA_UUID=$(/usr/bin/grep "^METADATA_UUID=" /etc/metadata_env`)
				Expect(count).To(Equal(1))
				// Both should use the same variable
				Expect(scriptContent).To(ContainSubstring(`node-id=${METADATA_UUID}`))
				Expect(scriptContent).To(ContainSubstring(`KUBELET_PROVIDERID=${METADATA_UUID}`))
			})
		})
	})
})

func extractKubeletCustomLabelsScript(ignitionJSON string) string {
	var ignConfig map[string]interface{}
	Expect(json.Unmarshal([]byte(ignitionJSON), &ignConfig)).To(Succeed())

	storage := ignConfig["storage"].(map[string]interface{})
	files := storage["files"].([]interface{})

	for _, f := range files {
		file := f.(map[string]interface{})
		if file["path"] == "/usr/local/bin/kubelet_custom_labels" {
			contents := file["contents"].(map[string]interface{})
			source := contents["source"].(string)
			b64Data := strings.TrimPrefix(source, "data:text/plain;charset=utf-8;base64,")
			decoded, err := base64.StdEncoding.DecodeString(b64Data)
			Expect(err).NotTo(HaveOccurred())
			return string(decoded)
		}
	}
	return ""
}
