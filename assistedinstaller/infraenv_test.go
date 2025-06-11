package assistedinstaller

import (
	"fmt"
	"testing"

	bootstrapv1alpha1 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/bootstrap/api/v1alpha1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"

	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	eventURLPattern = "%s://assisted-service.assisted-installer.com/api/assisted-install/v2/events?api_key=%s&infra_env_id=%s" //nolint:lll
	dummyAPIKey     = "eyJhbGciO"
	dummyInfraenvID = "e6f55793-95f8-484e-83f3-ac33f05f274b"
)

var _ = Describe("Assisted Installer InfraEnv generation", func() {
	When("Generating InfraEnv from OpenshiftAssistedConfig", func() {
		It("should reference the right resources", func() {
			clusterDeployment := &hivev1.ClusterDeployment{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-cluster-deployment",
					Namespace: "my-namespace",
				},
			}
			config := &bootstrapv1alpha1.OpenshiftAssistedConfig{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-config",
					Namespace: "my-namespace",
					Labels: map[string]string{
						clusterv1.ClusterNameLabel: "test-cluster",
					},
				},
				Spec: bootstrapv1alpha1.OpenshiftAssistedConfigSpec{
					PullSecretRef: &corev1.LocalObjectReference{
						Name: "pull-secret",
					},
				},
			}
			infraEnv := GetInfraEnvFromConfig(
				"test-infraenv",
				config,
				clusterDeployment,
			)
			Expect(infraEnv.Name).To(Equal("test-infraenv"))
			Expect(infraEnv.Spec.ClusterRef.Name).To(Equal("test-cluster-deployment"))
			Expect(infraEnv.Spec.ClusterRef.Namespace).To(Equal("my-namespace"))
			Expect(infraEnv.Spec.PullSecretRef.Name).To(Equal("pull-secret"))
		})
	})

	When("Generating InfraEnv from OpenshiftAssistedConfig with discovery annotations", func() {
		It("should set the right fields", func() {
			clusterDeployment := &hivev1.ClusterDeployment{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-cluster-deployment",
					Namespace: "my-namespace",
				},
			}
			config := &bootstrapv1alpha1.OpenshiftAssistedConfig{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-config",
					Namespace: "my-namespace",
					Labels: map[string]string{
						clusterv1.ClusterNameLabel: "test-cluster",
					},
					Annotations: map[string]string{
						bootstrapv1alpha1.DiscoveryIgnitionOverrideAnnotation: `{"test-ignition-override":"this-is-json"}`,
					},
				},
				Spec: bootstrapv1alpha1.OpenshiftAssistedConfigSpec{
					PullSecretRef: &corev1.LocalObjectReference{
						Name: "pull-secret",
					},
				},
			}
			infraEnv := GetInfraEnvFromConfig(
				"test-infraenv",
				config,
				clusterDeployment,
			)
			Expect(infraEnv.Name).To(Equal("test-infraenv"))
			Expect(infraEnv.Spec.ClusterRef.Name).To(Equal("test-cluster-deployment"))
			Expect(infraEnv.Spec.ClusterRef.Namespace).To(Equal("my-namespace"))
			Expect(infraEnv.Spec.PullSecretRef.Name).To(Equal("pull-secret"))
			Expect(infraEnv.Spec.IgnitionConfigOverride).To(Equal(`{"test-ignition-override":"this-is-json"}`))
		})

		It("should not set the ignitionOverride infraEnv field when the json content is invalid", func() {
			clusterDeployment := &hivev1.ClusterDeployment{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-cluster-deployment",
					Namespace: "my-namespace",
				},
			}
			config := &bootstrapv1alpha1.OpenshiftAssistedConfig{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-config",
					Namespace: "my-namespace",
					Labels: map[string]string{
						clusterv1.ClusterNameLabel: "test-cluster",
					},
					Annotations: map[string]string{
						bootstrapv1alpha1.DiscoveryIgnitionOverrideAnnotation: `{"test-ignition-override":"this-is-not-json",}`,
					},
				},
				Spec: bootstrapv1alpha1.OpenshiftAssistedConfigSpec{
					PullSecretRef: &corev1.LocalObjectReference{
						Name: "pull-secret",
					},
				},
			}
			infraEnv := GetInfraEnvFromConfig(
				"test-infraenv",
				config,
				clusterDeployment,
			)
			Expect(infraEnv.Name).To(Equal("test-infraenv"))
			Expect(infraEnv.Spec.ClusterRef.Name).To(Equal("test-cluster-deployment"))
			Expect(infraEnv.Spec.ClusterRef.Namespace).To(Equal("my-namespace"))
			Expect(infraEnv.Spec.PullSecretRef.Name).To(Equal("pull-secret"))
			Expect(infraEnv.Spec.IgnitionConfigOverride).To(BeEmpty())
		})
	})

})

var _ = Describe("Assisted Installer InfraEnv", func() {
	When("Retrieving ignition URL from InfraEnv externally", func() {
		It("should generate the expected ignition URL", func() {
			cfg := ServiceConfig{UseInternalImageURL: false}
			infraEnv := utils.NewInfraEnv("test-ns", "test-infraenv")
			infraEnv.Status.InfraEnvDebugInfo.EventsURL = fmt.Sprintf(eventURLPattern, "http", dummyAPIKey, dummyInfraenvID)
			ignitionURL, err := GetIgnitionURLFromInfraEnv(cfg, *infraEnv)
			Expect(err).To(BeNil())
			Expect(ignitionURL.Scheme).To(Equal("http"))
			Expect(ignitionURL.Host).To(
				Equal("assisted-service.assisted-installer.com"),
			)
			expectedPath := fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/files", dummyInfraenvID)
			Expect(ignitionURL.Path).To(Equal(expectedPath))
			Expect(ignitionURL.Query().Get("api_key")).To(Equal("eyJhbGciO"))
			Expect(ignitionURL.Query().Get("file_name")).To(Equal("discovery.ign"))
		})
	})
	When("Retrieving ignition URL from InfraEnv externally, but InfraEnv has no EventsURL", func() {
		It("should fail to generate the expected ignition URL", func() {
			cfg := ServiceConfig{UseInternalImageURL: false}
			infraEnv := utils.NewInfraEnv("test-ns", "test-infraenv")

			_, err := GetIgnitionURLFromInfraEnv(cfg, *infraEnv)
			Expect(err).To(MatchError("cannot generate ignition url if events URL is not generated"))
		})
	})
	When("Retrieving ignition URL from InfraEnv internally, but InfraEnv has no EventsURL", func() {
		It("should fail to generate the expected ignition URL", func() {
			cfg := ServiceConfig{UseInternalImageURL: true}
			infraEnv := utils.NewInfraEnv("test-ns", "test-infraenv")

			_, err := GetIgnitionURLFromInfraEnv(cfg, *infraEnv)
			Expect(err).To(MatchError("cannot generate ignition url if events URL is not generated"))
		})
	})
	When("Retrieving ignition URL from InfraEnv internally, and InfraEnv has EventsURL", func() {
		It("should generate the expected ignition URL", func() {
			cfg := ServiceConfig{UseInternalImageURL: true, AssistedServiceName: "assisted-service"}
			infraEnv := utils.NewInfraEnv("test-ns", "test-infraenv")
			infraEnv.Status.InfraEnvDebugInfo.EventsURL = fmt.Sprintf(eventURLPattern, "https", dummyAPIKey, dummyInfraenvID)

			ignitionURL, err := GetIgnitionURLFromInfraEnv(cfg, *infraEnv)
			Expect(err).To(BeNil())
			Expect(ignitionURL.Scheme).To(Equal("http"))
			Expect(ignitionURL.Host).To(
				Equal("assisted-service.test-ns.svc.cluster.local:8090"),
			)
			expectedPath := fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/files", dummyInfraenvID)
			Expect(ignitionURL.Path).To(Equal(expectedPath))
			Expect(ignitionURL.Query().Get("api_key")).To(Equal("eyJhbGciO"))
			Expect(ignitionURL.Query().Get("file_name")).To(Equal("discovery.ign"))
		})
	})
	When("Retrieving ignition URL from InfraEnv internally with overrides, and InfraEnv has EventsURL", func() {
		It("should generate the expected ignition URL", func() {
			cfg := ServiceConfig{
				UseInternalImageURL:        true,
				AssistedServiceName:        "my-assisted-service",
				AssistedInstallerNamespace: "my-assisted-ns",
			}
			infraEnv := utils.NewInfraEnv("test-ns", "test-infraenv")
			infraEnv.Status.InfraEnvDebugInfo.EventsURL = fmt.Sprintf(eventURLPattern, "https", dummyAPIKey, dummyInfraenvID)

			ignitionURL, err := GetIgnitionURLFromInfraEnv(cfg, *infraEnv)
			expectedPath := fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/files", dummyInfraenvID)
			Expect(err).To(BeNil())
			Expect(ignitionURL.Scheme).To(Equal("http"))
			Expect(ignitionURL.Host).To(Equal("my-assisted-service.my-assisted-ns.svc.cluster.local:8090"))
			Expect(ignitionURL.Path).To(Equal(expectedPath))
			Expect(ignitionURL.Query().Get("api_key")).To(Equal(dummyAPIKey))
			Expect(ignitionURL.Query().Get("file_name")).To(Equal("discovery.ign"))
		})
	})
})

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Test assisted installer utils")
}
