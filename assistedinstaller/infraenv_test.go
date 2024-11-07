package assistedinstaller

import (
	"fmt"
	"testing"

	"github.com/openshift-assisted/cluster-api-agent/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	eventURLPattern = "%s://assisted-service.assisted-installer.com/api/assisted-install/v2/events?api_key=%s&infra_env_id=%s" //nolint:lll
	dummyAPIKey     = "eyJhbGciO"
	dummyInfraenvID = "e6f55793-95f8-484e-83f3-ac33f05f274b"
)

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
