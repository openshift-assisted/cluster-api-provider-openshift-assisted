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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/openshift-assisted/cluster-api-agent/assistedinstaller"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	testutils "github.com/openshift-assisted/cluster-api-agent/test/utils"
	v1beta12 "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	"github.com/openshift/assisted-service/api/v1beta1"
	v1 "github.com/openshift/hive/apis/hive/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	bootstrapv1alpha1 "github.com/openshift-assisted/cluster-api-agent/bootstrap/api/v1alpha1"
)

const (
	isoExampleURL        = "https://example.com/download-my-iso"
	agentName            = "test-agent"
	oacName              = "test-resource"
	namespace            = "test-namespace"
	clusterName          = "test-cluster"
	machineName          = "test-resource"
	acpName              = "test-controlplane"
	infraEnvName         = "test-infraenv"
	testCert      string = `-----BEGIN CERTIFICATE-----
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

type mockTransport struct {
	mockHandler func(req *http.Request) (*http.Response, error)
}

func (t *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.mockHandler(req)
}

var _ = Describe("OpenshiftAssistedConfig Controller", func() {
	Context("When reconciling a resource", func() {
		ctx := context.Background()
		var controllerReconciler *OpenshiftAssistedConfigReconciler
		var k8sClient client.Client

		BeforeEach(func() {
			By("Resetting fakeclient state")
			k8sClient = fakeclient.NewClientBuilder().WithScheme(testScheme).
				WithStatusSubresource(&bootstrapv1alpha1.OpenshiftAssistedConfig{}, &v1beta1.InfraEnv{}).
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
			"InfraEnv, ClusterDeployment and AgentClusterInstall are already created but no eventsURL has been generated",
			func() {
				It("fail reconciliation", func() {
					oac := setupControlPlaneOpenshiftAssistedConfig(ctx, k8sClient)
					mockControlPlaneInitialization(ctx, k8sClient)

					// InfraEnv with no eventsURL
					Expect(k8sClient.Create(ctx, testutils.NewInfraEnv(namespace, machineName))).To(Succeed())
					oac.Status.ISODownloadURL = isoExampleURL
					Expect(k8sClient.Status().Update(ctx, oac)).To(Succeed())

					_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
						NamespacedName: client.ObjectKeyFromObject(oac),
					})
					Expect(err).To(MatchError("error while retrieving ignitionURL: cannot generate ignition url if events URL is not generated"))
				})
			},
		)
		When(
			"InfraEnv, ClusterDeployment and AgentClusterInstall are already created",
			func() {
				It("should create data secret", func() {
					// http://assisted-service.assisted-installer.com/api/assisted-install/v2/infra-envs/e6f55793-95f8-484e-83f3-ac33f05f274b/downloads/files?api_key=eyJhbGciOiJFUzI1NiIsInR5cCI6IkpXVCJ9.eyJpbmZyYV9lbnZfaWQiOiJlNmY1NTc5My05NWY4LTQ4NGUtODNmMy1hYzMzZjA1ZjI3NGIifQ.HCwlge7dTI8tUR2FC3YPhfIk7hG2p0tcbV1AzaZ2V_o-5lackqPHV18Ai3wPYnUPFLSgtW4-SnL28QsZRW82Vg&file_name=discovery.ign
					mockResponse := `{"fake":"ignition"}`
					server := httptest.NewServer(http.HandlerFunc(
						func(w http.ResponseWriter, r *http.Request) {
							w.WriteHeader(http.StatusOK)
							_, _ = w.Write([]byte(mockResponse))
						}))
					defer server.Close()

					controllerReconciler = &OpenshiftAssistedConfigReconciler{
						Client:     k8sClient,
						Scheme:     k8sClient.Scheme(),
						HttpClient: server.Client(),
					}

					oac := setupControlPlaneOpenshiftAssistedConfig(ctx, k8sClient)
					mockControlPlaneInitialization(ctx, k8sClient)
					infraEnv := testutils.NewInfraEnv(namespace, machineName)
					Expect(k8sClient.Create(ctx, infraEnv)).To(Succeed())
					infraEnv.Status.InfraEnvDebugInfo.EventsURL = server.URL + "/api/assisted-install/v2/events?api_key=eyJhbGciOiJFUzI1NiIsInR5cCI6IkpXVCJ9.eyJpbmZyYV9lbnZfaWQiOiJlNmY1NTc5My05NWY4LTQ4NGUtODNmMy1hYzMzZjA1ZjI3NGIifQ.HCwlge7dTI8tUR2FC3YPhfIk7hG2p0tcbV1AzaZ2V_o-5lackqPHV18Ai3wPYnUPFLSgtW4-SnL28QsZRW82Vg&infra_env_id=e6f55793-95f8-484e-83f3-ac33f05f274b"
					Expect(k8sClient.Status().Update(ctx, infraEnv)).To(Succeed())

					oac.Status.ISODownloadURL = isoExampleURL
					Expect(k8sClient.Status().Update(ctx, oac)).To(Succeed())

					_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
						NamespacedName: client.ObjectKeyFromObject(oac),
					})
					Expect(err).To(BeNil())

					//Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(oac), oac)).To(Succeed())
					//assertBootstrapReady(oac)
					secret := corev1.Secret{}
					Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(oac), &secret)).To(Succeed())
					Expect(len(secret.Data)).To(Equal(2))
					format, ok := secret.Data["format"]
					Expect(ok).To(BeTrue())
					Expect(string(format)).To(Equal("ignition"))
					ignition, ok := secret.Data["value"]
					Expect(ok).To(BeTrue())
					Expect(string(ignition)).ToNot(BeEmpty())
					Expect(string(ignition)).To(Equal(mockResponse))

				})
			},
		)
		When(
			"InfraEnv, ClusterDeployment and AgentClusterInstall are already created, internal URLs are expected",
			func() {
				It("should create data secret", func() {
					// http://assisted-service.assisted-installer.com/api/assisted-install/v2/infra-envs/e6f55793-95f8-484e-83f3-ac33f05f274b/downloads/files?api_key=eyJhbGciOiJFUzI1NiIsInR5cCI6IkpXVCJ9.eyJpbmZyYV9lbnZfaWQiOiJlNmY1NTc5My05NWY4LTQ4NGUtODNmMy1hYzMzZjA1ZjI3NGIifQ.HCwlge7dTI8tUR2FC3YPhfIk7hG2p0tcbV1AzaZ2V_o-5lackqPHV18Ai3wPYnUPFLSgtW4-SnL28QsZRW82Vg&file_name=discovery.ign
					mockResponse := `{"fake":"ignition"}`

					mockHandler := func(req *http.Request) (*http.Response, error) {
						if req.URL.Host != "assisted-service.assisted-installer.svc.cluster.local:8090" {
							return nil, fmt.Errorf("unexpected host: %s", req.URL.Host)
						}
						mockBody := io.NopCloser(strings.NewReader(mockResponse))
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       mockBody,
							Header:     make(http.Header),
						}, nil
					}
					controllerReconciler = &OpenshiftAssistedConfigReconciler{
						Client: k8sClient,
						Scheme: k8sClient.Scheme(),
						HttpClient: &http.Client{
							Transport: &mockTransport{mockHandler: mockHandler},
						},
						AssistedInstallerConfig: assistedinstaller.ServiceConfig{
							UseInternalImageURL:        true,
							AssistedServiceName:        "assisted-service",
							AssistedInstallerNamespace: "assisted-installer",
						},
					}

					oac := setupControlPlaneOpenshiftAssistedConfig(ctx, k8sClient)
					mockControlPlaneInitialization(ctx, k8sClient)
					infraEnv := testutils.NewInfraEnv(namespace, machineName)
					Expect(k8sClient.Create(ctx, infraEnv)).To(Succeed())
					infraEnv.Status.InfraEnvDebugInfo.EventsURL = "http://assisted-service.assisted-installer.com/api/assisted-install/v2/events?api_key=eyJhbGciOiJFUzI1NiIsInR5cCI6IkpXVCJ9.eyJpbmZyYV9lbnZfaWQiOiJlNmY1NTc5My05NWY4LTQ4NGUtODNmMy1hYzMzZjA1ZjI3NGIifQ.HCwlge7dTI8tUR2FC3YPhfIk7hG2p0tcbV1AzaZ2V_o-5lackqPHV18Ai3wPYnUPFLSgtW4-SnL28QsZRW82Vg&infra_env_id=e6f55793-95f8-484e-83f3-ac33f05f274b"
					Expect(k8sClient.Status().Update(ctx, infraEnv)).To(Succeed())

					oac.Status.ISODownloadURL = isoExampleURL
					Expect(k8sClient.Status().Update(ctx, oac)).To(Succeed())

					_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
						NamespacedName: client.ObjectKeyFromObject(oac),
					})
					Expect(err).To(BeNil())

					//Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(oac), oac)).To(Succeed())
					//assertBootstrapReady(oac)
					secret := corev1.Secret{}
					Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(oac), &secret)).To(Succeed())
					Expect(len(secret.Data)).To(Equal(2))
					format, ok := secret.Data["format"]
					Expect(ok).To(BeTrue())
					Expect(string(format)).To(Equal("ignition"))
					ignition, ok := secret.Data["value"]
					Expect(ok).To(BeTrue())
					Expect(string(ignition)).ToNot(BeEmpty())
					Expect(string(ignition)).To(Equal(mockResponse))

				})
			},
		)
	})
})

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

func setupControlPlaneOpenshiftAssistedConfig(
	ctx context.Context,
	k8sClient client.Client,
) *bootstrapv1alpha1.OpenshiftAssistedConfig {
	cluster := testutils.NewCluster(clusterName, namespace)
	Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

	acp := testutils.NewOpenshiftAssistedControlPlane(namespace, acpName)
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
