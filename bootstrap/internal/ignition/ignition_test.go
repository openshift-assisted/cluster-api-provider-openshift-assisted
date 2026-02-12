package ignition

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	config_31 "github.com/coreos/ignition/v2/config/v3_1"
	config_types "github.com/coreos/ignition/v2/config/v3_1/types"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Ignition utils")
}

// decodeFileContents extracts and decodes the base64 content from an ignition file source URL.
func decodeFileContents(file config_types.File) string {
	source := *file.FileEmbedded1.Contents.Source
	b64 := strings.TrimPrefix(source, "data:text/plain;charset=utf-8;base64,")
	decoded, err := base64.StdEncoding.DecodeString(b64)
	Expect(err).NotTo(HaveOccurred())
	return string(decoded)
}

/*
METADATA_LOCAL_HOSTNAME=bmh-vm-03
METADATA_METAL3_NAME=bmh-vm-03
METADATA_METAL3_NAMESPACE=test-capi
METADATA_NAME=bmh-vm-03
METADATA_UUID=0854b65a-42ec-4155-8e7c-ffc2b634c947
*/
var _ = Describe("Ignition utils", func() {
	When("generating ignition", func() {
		It("should be parsed with no errors", func() {
			capiSuccessFile := CreateIgnitionFile("/run/cluster-api/bootstrap-success.complete",
				"root", "data:text/plain;charset=utf-8;base64,c3VjY2Vzcw==", 420, true)
			i, err := GetIgnitionConfigOverrides(IgnitionOptions{}, capiSuccessFile)
			Expect(err).NotTo(HaveOccurred())
			cfg, rep, err := config_31.Parse([]byte(i))
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg).NotTo(BeNil())
		})

		It("should include hostname unit when NodeNameEnvVar is set", func() {
			opts := IgnitionOptions{
				NodeNameEnvVar: "$METADATA_NAME",
			}
			i, err := GetIgnitionConfigOverrides(opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, rep, err := config_31.Parse([]byte(i))
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg).NotTo(BeNil())

			// Check for set-hostname.service unit
			var foundHostnameUnit bool
			var foundHostnameScript bool
			for _, unit := range cfg.Systemd.Units {
				if unit.Name == "set-hostname.service" {
					foundHostnameUnit = true
					Expect(*unit.Contents).To(ContainSubstring("After=configdrive-metadata.service"))
				}
			}
			for _, file := range cfg.Storage.Files {
				if file.Path == "/usr/local/bin/set_hostname" {
					foundHostnameScript = true
				}
			}
			Expect(foundHostnameUnit).To(BeTrue(), "set-hostname.service unit should be present")
			Expect(foundHostnameScript).To(BeTrue(), "set_hostname script should be present")
		})

		It("should not include hostname unit when NodeNameEnvVar is empty", func() {
			opts := IgnitionOptions{}
			i, err := GetIgnitionConfigOverrides(opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, rep, err := config_31.Parse([]byte(i))
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg).NotTo(BeNil())

			// Check that set-hostname.service unit is NOT present
			for _, unit := range cfg.Systemd.Units {
				Expect(unit.Name).NotTo(Equal("set-hostname.service"))
			}
			for _, file := range cfg.Storage.Files {
				Expect(file.Path).NotTo(Equal("/usr/local/bin/set_hostname"))
			}
		})

		It("should handle ${VAR} notation for NodeNameEnvVar", func() {
			opts := IgnitionOptions{
				NodeNameEnvVar: "${METADATA_NAME}",
			}
			i, err := GetIgnitionConfigOverrides(opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, rep, err := config_31.Parse([]byte(i))
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg).NotTo(BeNil())

			// Check for set-hostname.service unit with ${VAR} notation
			var foundHostnameUnit bool
			var foundHostnameScript bool
			for _, unit := range cfg.Systemd.Units {
				if unit.Name == "set-hostname.service" {
					foundHostnameUnit = true
				}
			}
			for _, file := range cfg.Storage.Files {
				if file.Path == "/usr/local/bin/set_hostname" {
					foundHostnameScript = true
				}
			}
			Expect(foundHostnameUnit).To(BeTrue(), "set-hostname.service unit should be present with ${VAR} notation")
			Expect(foundHostnameScript).To(BeTrue(), "set_hostname script should be present with ${VAR} notation")
		})

		It("should include pre-install script and unit when PreInstallCommands is set", func() {
			opts := IgnitionOptions{
				PreInstallCommands: []string{
					"echo hello",
					"echo world",
				},
			}
			i, err := GetIgnitionConfigOverrides(opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, rep, err := config_31.Parse([]byte(i))
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())

			var foundUnit bool
			var foundScript bool
			for _, unit := range cfg.Systemd.Units {
				if unit.Name == "pre-install.service" {
					foundUnit = true
					Expect(*unit.Enabled).To(BeTrue())
					Expect(*unit.Contents).To(ContainSubstring("ExecStart=/usr/local/bin/pre-install"))
				}
			}
			for _, file := range cfg.Storage.Files {
				if file.Path == "/usr/local/bin/pre-install" {
					foundScript = true
					content := decodeFileContents(file)
					Expect(content).To(Equal("#!/bin/bash\nset -e\necho hello\necho world\n"))
				}
			}
			Expect(foundUnit).To(BeTrue(), "pre-install.service unit should be present")
			Expect(foundScript).To(BeTrue(), "pre-install script should be present")
		})

		It("should not include pre-install script when PreInstallCommands is empty", func() {
			opts := IgnitionOptions{}
			i, err := GetIgnitionConfigOverrides(opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, rep, err := config_31.Parse([]byte(i))
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())

			for _, unit := range cfg.Systemd.Units {
				Expect(unit.Name).NotTo(Equal("pre-install.service"))
			}
			for _, file := range cfg.Storage.Files {
				Expect(file.Path).NotTo(Equal("/usr/local/bin/pre-install"))
			}
		})
	})

	When("merging ignition", func() {
		var baseIgnition []byte

		BeforeEach(func() {
			base := config_types.Config{
				Ignition: config_types.Ignition{
					Version: "3.1.0",
				},
			}
			var err error
			baseIgnition, err = json.Marshal(base)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should include post-install script and unit when PostInstallCommands is set", func() {
			log := zap.New(zap.UseDevMode(true))
			opts := IgnitionOptions{
				PostInstallCommands: []string{
					"systemctl restart kubelet",
					"touch /var/post-install-done",
				},
			}
			result, err := MergeIgnitionConfig(log, baseIgnition, opts)
			Expect(err).NotTo(HaveOccurred())

			cfg, rep, err := config_31.Parse(result)
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())

			var foundUnit bool
			var foundScript bool
			for _, unit := range cfg.Systemd.Units {
				if unit.Name == "post-install.service" {
					foundUnit = true
					Expect(*unit.Enabled).To(BeTrue())
					Expect(*unit.Contents).To(ContainSubstring("ExecStart=/usr/local/bin/post-install"))
				}
			}
			for _, file := range cfg.Storage.Files {
				if file.Path == "/usr/local/bin/post-install" {
					foundScript = true
					content := decodeFileContents(file)
					Expect(content).To(Equal("#!/bin/bash\nset -e\nsystemctl restart kubelet\ntouch /var/post-install-done\n"))
				}
			}
			Expect(foundUnit).To(BeTrue(), "post-install.service unit should be present")
			Expect(foundScript).To(BeTrue(), "post-install script should be present")
		})

		It("should not include post-install script when PostInstallCommands is empty", func() {
			log := zap.New(zap.UseDevMode(true))
			opts := IgnitionOptions{
				NodeNameEnvVar: "$METADATA_NAME",
			}
			result, err := MergeIgnitionConfig(log, baseIgnition, opts)
			Expect(err).NotTo(HaveOccurred())

			cfg, rep, err := config_31.Parse(result)
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())

			for _, unit := range cfg.Systemd.Units {
				Expect(unit.Name).NotTo(Equal("post-install.service"))
			}
			for _, file := range cfg.Storage.Files {
				Expect(file.Path).NotTo(Equal("/usr/local/bin/post-install"))
			}
		})

		It("should always include configdrive metadata even when no options are set", func() {
			log := zap.New(zap.UseDevMode(true))
			opts := IgnitionOptions{}
			result, err := MergeIgnitionConfig(log, baseIgnition, opts)
			Expect(err).NotTo(HaveOccurred())

			cfg, rep, err := config_31.Parse(result)
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())

			unitNames := make([]string, 0, len(cfg.Systemd.Units))
			for _, unit := range cfg.Systemd.Units {
				unitNames = append(unitNames, unit.Name)
			}
			Expect(unitNames).To(ContainElement("configdrive-metadata.service"))
			Expect(unitNames).NotTo(ContainElement("set-hostname.service"))
			Expect(unitNames).NotTo(ContainElement("post-install.service"))

			filePaths := make([]string, 0, len(cfg.Storage.Files))
			for _, file := range cfg.Storage.Files {
				filePaths = append(filePaths, file.Path)
			}
			Expect(filePaths).To(ContainElement("/usr/local/bin/configdrive_metadata"))
		})

		It("should include both hostname and post-install when both are set", func() {
			log := zap.New(zap.UseDevMode(true))
			opts := IgnitionOptions{
				NodeNameEnvVar:      "$METADATA_NAME",
				PostInstallCommands: []string{"echo done"},
			}
			result, err := MergeIgnitionConfig(log, baseIgnition, opts)
			Expect(err).NotTo(HaveOccurred())

			cfg, rep, err := config_31.Parse(result)
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())

			unitNames := make([]string, 0, len(cfg.Systemd.Units))
			for _, unit := range cfg.Systemd.Units {
				unitNames = append(unitNames, unit.Name)
			}
			Expect(unitNames).To(ContainElement("set-hostname.service"))
			Expect(unitNames).To(ContainElement("post-install.service"))
			Expect(unitNames).To(ContainElement("configdrive-metadata.service"))

			filePaths := make([]string, 0, len(cfg.Storage.Files))
			for _, file := range cfg.Storage.Files {
				filePaths = append(filePaths, file.Path)
			}
			Expect(filePaths).To(ContainElement("/usr/local/bin/post-install"))
			Expect(filePaths).To(ContainElement("/usr/local/bin/set_hostname"))
		})
	})
})
