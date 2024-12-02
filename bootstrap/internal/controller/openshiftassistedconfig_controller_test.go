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
	agentName                        = "test-agent"
	oacName                          = "test-resource"
	bmhName                          = "test-bmh"
	namespace                        = "test-namespace"
	clusterName                      = "test-cluster"
	clusterDeploymentName            = "test-clusterdeployment"
	machineName                      = "test-resource"
	metal3MachineName                = "test-m3machine"
	acpName                          = "test-controlplane"
	metal3MachineTemplateName        = "test-m3machinetemplate"
	infraEnvName                     = "test-infraenv"
	testCert                  string = `-----BEGIN CERTIFICATE-----
MIIFPjCCAyagAwIBAgIUBCE1YX2zJ0R/3NURq2XQaciEuVQwDQYJKoZIhvcNAQEL
BQAwFjEUMBIGA1UEAwwLZXhhbXBsZS5jb20wHhcNMjIxMTI3MjM0MjAyWhcNMzIx
MTI0MjM0MjAyWjAWMRQwEgYDVQQDDAtleGFtcGxlLmNvbTCCAiIwDQYJKoZIhvcN
AQEBBQADggIPADCCAgoCggIBAKY589W+Xifs9SfxofBI1r1/NKsMUVPvg3ZtDIPQ
EeNKf5OgtSOVFcoEmkS7ZWNTIu4Kd1WBf/rG+F5lm/aTTa3j720Q+fS+gsveGQPz
7taUpU/TjHHzoCqjjhaYMr4gIJ3jkpTXUWG5/vka/oNykSxkGCuZw1gyXHNujA8L
DJYY8VNUHPl5MmXGaT++6yEN4WdB2f7R/MmEaH6KnGo/LjhMeiVmDsIxHZ/xW9OR
izPklnUi78NfZJSxiknoV6CnQShNijLEq6nQowYQ1lQuNWs6sTM28I0BYWk+gDUz
NOWkVqSHFRMzGmpqYJs7JQiv0g33VN/92dwdP/kZc9sAYRqDaI6hplOZrD/OEsbG
lmN90x/o42wotJeBDN1hHlJ1JeRjR1Vk8XUfOmaTuOPzooKIM0h9K6Ah6u3lRQtE
n68yxn0sGD8yw6EydS5FD9zzvA6rgXBSsvpMFjk/N/FmnIzD4YinLEiflfub1O0M
9thEOX9IaOh00U2eGsRa/MOJcCZ5TUOgxVlv15ATUPHo1MW8QkmYOVx4BoM/Bw0J
0HibIU8VUw2AV1tupRdQma7Qg5gyjdx2doth78IG5+LkX95fSyz60Kf9l1xBQHNA
kVyzkXlx8jmdm53CeFvHVOrVrLuA2Dk+t21TNL1uFGgQ0iLxItCf1O6F6B78QqhI
YLOdAgMBAAGjgYMwgYAwHQYDVR0OBBYEFE6DFh3+wGzA8dOYBTL9Z0CyxLJ/MB8G
A1UdIwQYMBaAFE6DFh3+wGzA8dOYBTL9Z0CyxLJ/MA8GA1UdEwEB/wQFMAMBAf8w
LQYDVR0RBCYwJIILZXhhbXBsZS5jb22CD3d3dy5leGFtcGxlLm5ldIcECgAAATAN
BgkqhkiG9w0BAQsFAAOCAgEAoj+elkYHrek6DoqOvEFZZtRp6bPvof61/VJ3kP7x
HZXp5yVxvGOHt61YRziGLpsFbuiDczk0V61ZdozHUOtZ0sWB4VeyO1pAjfd/JwDI
CK6olfkSO78WFQfdG4lNoSM9dQJyEIEZ1sbvuUL3RHDBd9oEKue+vsstlM9ahdoq
fpTTFq4ENGCAIDvaqKIlpjKsAMrsTO47CKPVh2HUpugfVGKeBRsW1KAXFoC2INS5
7BY3h60jFFW6bz0v+FnzW96Mt2VNW+i/REX6fBaR4m/QfG81rA2EEmhxCGrany+N
6DUkwiJxcqBMH9jA2yVnF7BgwG2C3geBqXTTlvVQJD8GOktkvgLjlHcYqO1pI7B3
wP9F9ZF+w39jXwGMGBg8+/aQz1RjP2bOb18n7d0bc4/pbbkVAmE4sq4qMneFZAVE
uj9S2Jna3ut08ZP05Ych5vCGX4VJ8gNNgrJju2PJVBl8NNyDfHKeHfWSOR9uOMjT
vqK6iRD9xqu/oLJyrlAuOL8ZxRpeqjxF/g8NYYV/fvv8apaX58ua9qYAFQVGf590
mmjOozzn9VBqKenVmfwzen5v78CBSgS4Hd72Qp42rLCNgqI8gyQa2qZzaNjLP/wI
pBpFC21fkybGYPkislPQ3EI69ZGRafWDBjlFFTS3YkDM98tqTZD+JG4STY+ivHhK
gmY=
-----END CERTIFICATE-----`
)

var _ = Describe("OpenshiftAssistedConfig Controller", func() {
	Context("When reconciling a resource", func() {
		ctx := context.Background()
		var controllerReconciler *OpenshiftAssistedConfigReconciler
		var k8sClient client.Client

		BeforeEach(func() {
			By("Resetting fakeclient state")
			k8sClient = fakeclient.NewClientBuilder().WithScheme(testScheme).
				WithStatusSubresource(&bootstrapv1alpha1.OpenshiftAssistedConfig{}).
				Build()
			Expect(k8sClient).NotTo(BeNil())

			controllerReconciler = &OpenshiftAssistedConfigReconciler{
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
		When("OpenshiftAssistedConfig has no owner", func() {
			It("should successfully reconcile with NOOP", func() {
				oac := NewOpenshiftAssistedConfig(namespace, oacName, clusterName)
				Expect(k8sClient.Create(ctx, oac)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(oac),
				})
				Expect(err).NotTo(HaveOccurred())

				// This config has no owner, should exit before setting conditions
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(oac), oac)).To(Succeed())
				condition := conditions.Get(oac,
					bootstrapv1alpha1.DataSecretAvailableCondition,
				)
				Expect(condition).To(BeNil())
			})
		})
		When("OpenshiftAssistedConfig has a non-relevant owner", func() {
			It("should successfully reconcile", func() {
				oac := NewOpenshiftAssistedConfig(namespace, oacName, clusterName)
				oac.OwnerReferences = []metav1.OwnerReference{
					{
						APIVersion: "madeup-version",
						Kind:       "madeup-kind",
						Name:       "madeup-name",
						UID:        "madeup-uid",
					},
				}
				Expect(k8sClient.Create(ctx, oac)).To(Succeed())

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(oac),
				})
				Expect(err).NotTo(HaveOccurred())

				// This config has no relevant owner, should exit before setting conditions
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(oac), oac)).To(Succeed())
				condition := conditions.Get(oac,
					bootstrapv1alpha1.DataSecretAvailableCondition,
				)
				Expect(condition).To(BeNil())
			})
		})
		When("ClusterDeployment and AgentClusterInstall are not created yet", func() {
			It("should wait with no error", func() {
				// Given
				oac := setupControlPlaneOpenshiftAssistedConfig(ctx, k8sClient)
				// and OpenshiftAssistedControlPlane provider did not create CD and ACI

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(oac),
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(oac), oac)).To(Succeed())
				dataSecretReadyCondition := conditions.Get(oac,
					bootstrapv1alpha1.DataSecretAvailableCondition,
				)
				Expect(dataSecretReadyCondition).NotTo(BeNil())
				Expect(dataSecretReadyCondition.Reason).To(Equal(bootstrapv1alpha1.WaitingForAssistedInstallerReason))
			})
		})
		When("ClusterDeployment is created but AgentClusterInstall is not", func() {
			It("should requeue the request without errors", func() {
				oac := setupControlPlaneOpenshiftAssistedConfig(ctx, k8sClient)
				cd := testutils.NewClusterDeploymentWithOwnerCluster(namespace, clusterName, clusterName)
				Expect(k8sClient.Create(ctx, cd)).To(Succeed())
				// but not ACI

				result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(oac),
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Requeue).To(BeTrue())
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(oac), oac)).To(Succeed())
				dataSecretReadyCondition := conditions.Get(oac,
					bootstrapv1alpha1.DataSecretAvailableCondition,
				)
				Expect(dataSecretReadyCondition).NotTo(BeNil())
				Expect(dataSecretReadyCondition.Reason).To(Equal(bootstrapv1alpha1.WaitingForAssistedInstallerReason))
			})
		})
		When("ClusterDeployment and AgentClusterInstall are already created", func() {
			It("should create infraenv with an empty ISO URL", func() {
				oac := setupControlPlaneOpenshiftAssistedConfig(ctx, k8sClient)
				mockControlPlaneInitialization(ctx, k8sClient)

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(oac),
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(oac), oac)).To(Succeed())
				dataSecretReadyCondition := conditions.Get(oac,
					bootstrapv1alpha1.DataSecretAvailableCondition,
				)
				Expect(dataSecretReadyCondition).NotTo(BeNil())
				Expect(dataSecretReadyCondition.Reason).To(Equal(bootstrapv1alpha1.WaitingForLiveISOURLReason))

				assertInfraEnvWithEmptyISOURL(ctx, k8sClient, oac)
			})
		})
		When(
			"InfraEnv, ClusterDeployment and AgentClusterInstall are already created but no Metal3Machine is running",
			func() {
				It("should update metal3MachineTemplate", func() {
					oac := setupControlPlaneOpenshiftAssistedConfig(ctx, k8sClient)
					mockControlPlaneInitialization(ctx, k8sClient)

					// InfraEnv and OpenshiftAssistedConfig is already updated by InfraEnv controller
					Expect(k8sClient.Create(ctx, testutils.NewInfraEnv(namespace, infraEnvName))).To(Succeed())
					expectedDiskFormat := "live-iso"
					expectedISODownloadURL := "https://example.com/download-my-iso"
					oac.Status.ISODownloadURL = expectedISODownloadURL
					Expect(k8sClient.Status().Update(ctx, oac)).To(Succeed())

					_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
						NamespacedName: client.ObjectKeyFromObject(oac),
					})
					Expect(err).NotTo(HaveOccurred())
					assertISOURLOnM3Template(ctx, k8sClient, expectedISODownloadURL, expectedDiskFormat)
					Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(oac), oac)).To(Succeed())
					assertBootstrapReady(oac)
				})
			},
		)
		When(
			"InfraEnv, ClusterDeployment and AgentClusterInstall are already created and Metal3Machine is running",
			func() {
				It("should update metal3MachineTemplate", func() {
					oac := setupControlPlaneOpenshiftAssistedConfigWithMetal3Machine(ctx, k8sClient)
					mockControlPlaneInitialization(ctx, k8sClient)
					// InfraEnv and OpenshiftAssistedConfig is already updated by InfraEnv controller
					Expect(k8sClient.Create(ctx, testutils.NewInfraEnv(namespace, infraEnvName))).To(Succeed())
					expectedDiskFormat := "live-iso"
					expectedISODownloadURL := "https://example.com/download-my-iso"
					// Simulate InfraEnv controller updating ISO to config
					oac.Status.ISODownloadURL = expectedISODownloadURL
					Expect(k8sClient.Status().Update(ctx, oac)).To(Succeed())

					_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
						NamespacedName: client.ObjectKeyFromObject(oac),
					})
					Expect(err).NotTo(HaveOccurred())

					assertISOURLOnM3Template(ctx, k8sClient, expectedISODownloadURL, expectedDiskFormat)
					assertISOURLOnM3Machine(ctx, k8sClient, expectedISODownloadURL, expectedDiskFormat)

					Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(oac), oac)).To(Succeed())
					assertBootstrapReady(oac)

				})
			},
		)
	})
})

func assertISOURLOnM3Template(
	ctx context.Context,
	k8sClient client.Client,
	expectedISODownloadURL, expectedDiskFormat string,
) {
	m3Template := testutils.NewM3MachineTemplate(namespace, metal3MachineTemplateName)
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(m3Template), m3Template)).To(Succeed())
	Expect(m3Template.Spec.Template.Spec.Image.URL).To(Equal(expectedISODownloadURL))
	Expect(m3Template.Spec.Template.Spec.Image.DiskFormat).To(Equal(&expectedDiskFormat))
}

func assertISOURLOnM3Machine(
	ctx context.Context,
	k8sClient client.Client,
	expectedISODownloadURL, expectedDiskFormat string,
) {
	m3Machine := testutils.NewMetal3Machine(namespace, metal3MachineName)
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(m3Machine), m3Machine)).To(Succeed())
	Expect(m3Machine.Spec.Image.URL).To(Equal(expectedISODownloadURL))
	Expect(m3Machine.Spec.Image.DiskFormat).To(Equal(&expectedDiskFormat))
}
func assertBootstrapReady(oac *bootstrapv1alpha1.OpenshiftAssistedConfig) {
	Expect(conditions.IsTrue(oac, bootstrapv1alpha1.DataSecretAvailableCondition)).To(BeTrue())
	Expect(oac.Status.Ready).To(BeTrue())
	Expect(*oac.Status.DataSecretName).NotTo(BeNil())
}

func assertInfraEnvWithEmptyISOURL(
	ctx context.Context,
	k8sClient client.Client,
	oac *bootstrapv1alpha1.OpenshiftAssistedConfig,
) {
	infraEnvList := &v1beta1.InfraEnvList{}
	Expect(
		k8sClient.List(ctx, infraEnvList, client.MatchingLabels{bootstrapv1alpha1.OpenshiftAssistedConfigLabel: oacName}),
	).To(Succeed())
	Expect(len(infraEnvList.Items)).To(Equal(1))
	infraEnv := infraEnvList.Items[0]
	Expect(oac.Status.InfraEnvRef).ToNot(BeNil())

	assertInfraEnvSpecs(infraEnv, oac)

	Expect(infraEnv.Status.ISODownloadURL).To(Equal(""))
	Expect(oac.Status.ISODownloadURL).To(Equal(""))
}

func assertInfraEnvSpecs(infraEnv v1beta1.InfraEnv, oac *bootstrapv1alpha1.OpenshiftAssistedConfig) {
	Expect(infraEnv.Name).To(Equal(oac.Status.InfraEnvRef.Name))
	Expect(infraEnv.Spec.PullSecretRef).To(Equal(oac.Spec.PullSecretRef))
	Expect(infraEnv.Spec.Proxy).To(Equal(oac.Spec.Proxy))
	Expect(infraEnv.Spec.AdditionalNTPSources).To(Equal(oac.Spec.AdditionalNTPSources))
	Expect(infraEnv.Spec.NMStateConfigLabelSelector).To(Equal(oac.Spec.NMStateConfigLabelSelector))
	Expect(infraEnv.Spec.CpuArchitecture).To(Equal(oac.Spec.CpuArchitecture))
	Expect(infraEnv.Spec.KernelArguments).To(Equal(oac.Spec.KernelArguments))
	Expect(infraEnv.Spec.AdditionalTrustBundle).To(Equal(oac.Spec.AdditionalTrustBundle))
	Expect(infraEnv.Spec.OSImageVersion).To(Equal(oac.Spec.OSImageVersion))
}

// mock controlplane provider generating ACI and CD
func mockControlPlaneInitialization(ctx context.Context, k8sClient client.Client) {
	cd := testutils.NewClusterDeploymentWithOwnerCluster(namespace, clusterName, clusterName)
	Expect(k8sClient.Create(ctx, cd)).To(Succeed())

	aci := testutils.NewAgentClusterInstall(clusterName, namespace, clusterName)
	Expect(k8sClient.Create(ctx, aci)).To(Succeed())

	crossReferenceACIAndCD(ctx, k8sClient, aci, cd)
}

func setupControlPlaneOpenshiftAssistedConfigWithMetal3Machine(
	ctx context.Context,
	k8sClient client.Client,
) *bootstrapv1alpha1.OpenshiftAssistedConfig {
	oac := setupControlPlaneOpenshiftAssistedConfig(ctx, k8sClient)
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
	return oac
}

func setupControlPlaneOpenshiftAssistedConfig(
	ctx context.Context,
	k8sClient client.Client,
) *bootstrapv1alpha1.OpenshiftAssistedConfig {
	cluster := testutils.NewCluster(clusterName, namespace)
	Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

	m3Template := testutils.NewM3MachineTemplateWithImage(
		namespace,
		metal3MachineTemplateName,
		"https://example.com/abcd",
		"qcow2",
	)
	Expect(k8sClient.Create(ctx, m3Template)).To(Succeed())
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(m3Template), m3Template)).To(Succeed())

	acp := testutils.NewOpenshiftAssistedControlPlaneWithMachineTemplate(namespace, acpName, m3Template)
	Expect(k8sClient.Create(ctx, acp)).Should(Succeed())
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(acp), acp)).To(Succeed())

	machine := testutils.NewMachineWithOwner(namespace, machineName, clusterName, acp)
	Expect(k8sClient.Create(ctx, machine)).To(Succeed())
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(machine), machine)).To(Succeed())

	oac := NewOpenshiftAssistedConfigWithOwner(namespace, oacName, clusterName, machine)
	oac.Spec.PullSecretRef = &corev1.LocalObjectReference{Name: "my-pullsecret"}
	oac.Spec.Proxy = &v1beta1.Proxy{
		HTTPProxy:  "http://myproxy.com",
		HTTPSProxy: "https://myproxy.com",
		NoProxy:    "example.com,redhat.com",
	}
	oac.Spec.AdditionalNTPSources = []string{
		"192.168.1.3",
		"myntpservice.com",
	}
	oac.Spec.NMStateConfigLabelSelector = metav1.LabelSelector{
		MatchLabels: map[string]string{
			"mylabel": "myvalue",
		},
	}
	oac.Spec.CpuArchitecture = "x86"
	oac.Spec.KernelArguments = []v1beta1.KernelArgument{
		{
			Operation: "append",
			Value:     "p1",
		},
		{
			Operation: "append",
			Value:     `p2="this is an argument"`,
		},
	}
	oac.Spec.AdditionalTrustBundle = testCert
	oac.Spec.OSImageVersion = "4.14.0"
	Expect(k8sClient.Create(ctx, oac)).To(Succeed())
	return oac
}

func NewOpenshiftAssistedConfigWithOwner(
	namespace, name, clusterName string,
	owner client.Object,
) *bootstrapv1alpha1.OpenshiftAssistedConfig {
	ownerGVK := owner.GetObjectKind().GroupVersionKind()
	ownerRefs := []metav1.OwnerReference{
		{
			APIVersion: ownerGVK.GroupVersion().String(),
			Kind:       ownerGVK.Kind,
			Name:       owner.GetName(),
			UID:        owner.GetUID(),
		},
	}
	oac := NewOpenshiftAssistedConfig(namespace, name, clusterName)
	oac.OwnerReferences = ownerRefs
	return oac
}

func NewOpenshiftAssistedConfig(namespace, name, clusterName string) *bootstrapv1alpha1.OpenshiftAssistedConfig {
	return NewOpenshiftAssistedConfigWithInfraEnv(namespace, name, clusterName, nil)
}

func NewOpenshiftAssistedConfigWithInfraEnv(namespace, name, clusterName string, infraEnv *v1beta1.InfraEnv) *bootstrapv1alpha1.OpenshiftAssistedConfig {
	var ref *corev1.ObjectReference
	if infraEnv != nil {
		ref = &corev1.ObjectReference{
			Namespace: infraEnv.GetNamespace(),
			Name:      infraEnv.GetName(),
		}
	}
	return &bootstrapv1alpha1.OpenshiftAssistedConfig{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:         clusterName,
				clusterv1.MachineControlPlaneLabel: "control-plane",
			},
			Name:      name,
			Namespace: namespace,
		},
		Status: bootstrapv1alpha1.OpenshiftAssistedConfigStatus{
			InfraEnvRef: ref,
		},
	}
}

func crossReferenceACIAndCD(
	ctx context.Context,
	k8sClient client.Client,
	aci *v1beta12.AgentClusterInstall,
	cd *v1.ClusterDeployment,
) {
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
