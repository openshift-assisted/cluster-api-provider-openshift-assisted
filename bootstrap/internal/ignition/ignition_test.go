package ignition

import (
	"encoding/base64"
	"strings"
	"testing"

	config_31 "github.com/coreos/ignition/v2/config/v3_1"
	config_types "github.com/coreos/ignition/v2/config/v3_1/types"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const preBootstrapServiceName = "capoa-pre-bootstrap.service"
const postBootstrapServiceName = "capoa-post-bootstrap.service"
const providerIDDropinPath = "/etc/systemd/system/kubelet.service.d/10-provider-id.conf"
const baseIgnitionJSON = `{
	"ignition": {"version": "3.1.0"},
	"storage": {"files": []},
	"systemd": {"units": []}
}`

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Ignition utils")
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
	})

	When("generating ignition with preBootstrapCommands", func() {
		It("should not include pre-bootstrap unit when commands are empty", func() {
			opts := IgnitionOptions{}
			i, err := GetIgnitionConfigOverrides(opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, rep, err := config_31.Parse([]byte(i))
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())

			for _, unit := range cfg.Systemd.Units {
				Expect(unit.Name).NotTo(Equal("capoa-pre-bootstrap.service"))
			}
			for _, file := range cfg.Storage.Files {
				Expect(file.Path).NotTo(Equal("/usr/local/bin/capoa-pre-bootstrap.sh"))
			}
		})

		It("should include pre-bootstrap unit and script when commands are provided", func() {
			opts := IgnitionOptions{
				PreBootstrapCommands: []string{
					"echo 'setting up disks'",
					"sgdisk -n 1:0:0 /dev/sdb",
				},
			}
			i, err := GetIgnitionConfigOverrides(opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, rep, err := config_31.Parse([]byte(i))
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())

			var foundUnit bool
			for _, unit := range cfg.Systemd.Units {
				if unit.Name == preBootstrapServiceName {
					foundUnit = true
					Expect(*unit.Enabled).To(BeTrue())
					Expect(*unit.Contents).To(ContainSubstring("Before=kubelet.service"))
					Expect(*unit.Contents).To(ContainSubstring("After=network.target"))
					Expect(*unit.Contents).To(ContainSubstring("ostree-finalize-staged.service"))
					Expect(*unit.Contents).To(ContainSubstring("configdrive-metadata.service"))
					Expect(*unit.Contents).To(ContainSubstring("kubelet-customlabels.service"))
					Expect(*unit.Contents).To(ContainSubstring("ConditionPathExists=!/var/lib/capoa/pre-bootstrap.done"))
				}
			}
			Expect(foundUnit).To(BeTrue(), "capoa-pre-bootstrap.service unit should be present")

			var foundScript bool
			for _, file := range cfg.Storage.Files {
				if file.Path == "/usr/local/bin/capoa-pre-bootstrap.sh" {
					foundScript = true
					Expect(*file.Mode).To(Equal(0700))
				}
			}
			Expect(foundScript).To(BeTrue(), "capoa-pre-bootstrap.sh script should be present")
		})

		It("should order pre-bootstrap after hostname setup when hostname unit is enabled", func() {
			opts := IgnitionOptions{
				NodeNameEnvVar:       "$METADATA_NAME",
				PreBootstrapCommands: []string{"echo pre"},
			}
			i, err := GetIgnitionConfigOverrides(opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, rep, err := config_31.Parse([]byte(i))
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())

			for _, unit := range cfg.Systemd.Units {
				if unit.Name == preBootstrapServiceName {
					Expect(*unit.Contents).To(ContainSubstring("set-hostname.service"))
					return
				}
			}
			Fail("capoa-pre-bootstrap.service unit should be present")
		})

		It("should generate a script with set -euo pipefail, commands in order, and sentinel", func() {
			opts := IgnitionOptions{
				PreBootstrapCommands: []string{
					"echo step1",
					"echo step2",
					"echo step3",
				},
			}
			i, err := GetIgnitionConfigOverrides(opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, _, err := config_31.Parse([]byte(i))
			Expect(err).NotTo(HaveOccurred())

			scriptContent := extractFileContent(cfg, "/usr/local/bin/capoa-pre-bootstrap.sh")
			Expect(scriptContent).NotTo(BeEmpty())
			Expect(scriptContent).To(ContainSubstring("set -euo pipefail"))
			Expect(scriptContent).To(ContainSubstring("echo step1"))
			Expect(scriptContent).To(ContainSubstring("echo step2"))
			Expect(scriptContent).To(ContainSubstring("echo step3"))
			Expect(scriptContent).To(ContainSubstring("/var/lib/capoa/pre-bootstrap.done"))

			// Commands should appear before sentinel
			cmdIdx := strings.Index(scriptContent, "echo step1")
			sentinelIdx := strings.Index(scriptContent, "pre-bootstrap.done")
			Expect(cmdIdx).To(BeNumerically("<", sentinelIdx))
		})

		It("should use a custom sentinel directory when configured", func() {
			opts := IgnitionOptions{
				SentinelDirectory:    "/etc/capoa-state",
				PreBootstrapCommands: []string{"echo step1"},
			}
			i, err := GetIgnitionConfigOverrides(opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, rep, err := config_31.Parse([]byte(i))
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())

			var foundUnit bool
			for _, unit := range cfg.Systemd.Units {
				if unit.Name == preBootstrapServiceName {
					foundUnit = true
					Expect(*unit.Contents).To(ContainSubstring("ConditionPathExists=!/etc/capoa-state/pre-bootstrap.done"))
				}
			}
			Expect(foundUnit).To(BeTrue(), "capoa-pre-bootstrap.service unit should be present")

			scriptContent := extractFileContent(cfg, "/usr/local/bin/capoa-pre-bootstrap.sh")
			Expect(scriptContent).To(ContainSubstring("mkdir -p /etc/capoa-state"))
			Expect(scriptContent).To(ContainSubstring("touch /etc/capoa-state/pre-bootstrap.done"))
		})

		It("should normalize a trailing slash in the sentinel directory", func() {
			opts := IgnitionOptions{
				SentinelDirectory:    "/custom/path/",
				PreBootstrapCommands: []string{"echo step1"},
			}
			i, err := GetIgnitionConfigOverrides(opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, rep, err := config_31.Parse([]byte(i))
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())

			var foundUnit bool
			for _, unit := range cfg.Systemd.Units {
				if unit.Name == preBootstrapServiceName {
					foundUnit = true
					Expect(*unit.Contents).To(ContainSubstring("ConditionPathExists=!/custom/path/pre-bootstrap.done"))
					Expect(*unit.Contents).NotTo(ContainSubstring("//"))
				}
			}
			Expect(foundUnit).To(BeTrue(), "capoa-pre-bootstrap.service unit should be present")

			scriptContent := extractFileContent(cfg, "/usr/local/bin/capoa-pre-bootstrap.sh")
			Expect(scriptContent).To(ContainSubstring("mkdir -p /custom/path"))
			Expect(scriptContent).To(ContainSubstring("touch /custom/path/pre-bootstrap.done"))
			Expect(scriptContent).NotTo(ContainSubstring("//"))
		})
	})

	When("generating ignition with postBootstrapCommands", func() {
		It("should not include post-bootstrap unit when commands are empty", func() {
			opts := IgnitionOptions{}
			i, err := GetIgnitionConfigOverrides(opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, rep, err := config_31.Parse([]byte(i))
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())

			for _, unit := range cfg.Systemd.Units {
				Expect(unit.Name).NotTo(Equal("capoa-post-bootstrap.service"))
			}
			for _, file := range cfg.Storage.Files {
				Expect(file.Path).NotTo(Equal("/usr/local/bin/capoa-post-bootstrap.sh"))
			}
		})

		It("should include post-bootstrap unit and script when commands are provided", func() {
			opts := IgnitionOptions{
				PostBootstrapCommands: []string{
					"echo 'post-kubelet setup'",
				},
			}
			i, err := GetIgnitionConfigOverrides(opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, rep, err := config_31.Parse([]byte(i))
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())

			var foundUnit bool
			for _, unit := range cfg.Systemd.Units {
				if unit.Name == "capoa-post-bootstrap.service" {
					foundUnit = true
					Expect(*unit.Enabled).To(BeTrue())
					Expect(*unit.Contents).To(ContainSubstring("Requires=kubelet.service"))
					Expect(*unit.Contents).To(ContainSubstring("After=kubelet.service"))
					Expect(*unit.Contents).NotTo(ContainSubstring("Before=kubelet.service"))
					Expect(*unit.Contents).To(ContainSubstring("ConditionPathExists=!/var/lib/capoa/post-bootstrap.done"))
				}
			}
			Expect(foundUnit).To(BeTrue(), "capoa-post-bootstrap.service unit should be present")

			var foundScript bool
			for _, file := range cfg.Storage.Files {
				if file.Path == "/usr/local/bin/capoa-post-bootstrap.sh" {
					foundScript = true
					Expect(*file.Mode).To(Equal(0700))
				}
			}
			Expect(foundScript).To(BeTrue(), "capoa-post-bootstrap.sh script should be present")
		})

		It("should generate a post-bootstrap script with set -euo pipefail, commands, and sentinel", func() {
			opts := IgnitionOptions{
				PostBootstrapCommands: []string{
					"systemctl restart custom-agent",
				},
			}
			i, err := GetIgnitionConfigOverrides(opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, _, err := config_31.Parse([]byte(i))
			Expect(err).NotTo(HaveOccurred())

			scriptContent := extractFileContent(cfg, "/usr/local/bin/capoa-post-bootstrap.sh")
			Expect(scriptContent).NotTo(BeEmpty())
			Expect(scriptContent).To(ContainSubstring("set -euo pipefail"))
			Expect(scriptContent).To(ContainSubstring("systemctl restart custom-agent"))
			Expect(scriptContent).To(ContainSubstring("/var/lib/capoa/post-bootstrap.done"))
		})

		It("should use a custom sentinel directory when configured", func() {
			opts := IgnitionOptions{
				SentinelDirectory:     "/etc/capoa-state",
				PostBootstrapCommands: []string{"echo post"},
			}
			i, err := GetIgnitionConfigOverrides(opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, rep, err := config_31.Parse([]byte(i))
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())

			var foundUnit bool
			for _, unit := range cfg.Systemd.Units {
				if unit.Name == postBootstrapServiceName {
					foundUnit = true
					Expect(*unit.Contents).To(ContainSubstring("ConditionPathExists=!/etc/capoa-state/post-bootstrap.done"))
				}
			}
			Expect(foundUnit).To(BeTrue(), "capoa-post-bootstrap.service unit should be present")

			scriptContent := extractFileContent(cfg, "/usr/local/bin/capoa-post-bootstrap.sh")
			Expect(scriptContent).To(ContainSubstring("mkdir -p /etc/capoa-state"))
			Expect(scriptContent).To(ContainSubstring("touch /etc/capoa-state/post-bootstrap.done"))
		})

		It("should not include certificate wait in post-bootstrap script", func() {
			opts := IgnitionOptions{
				PostBootstrapCommands: []string{
					"echo 'post-kubelet setup'",
				},
			}
			i, err := GetIgnitionConfigOverrides(opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, _, err := config_31.Parse([]byte(i))
			Expect(err).NotTo(HaveOccurred())

			scriptContent := extractFileContent(cfg, "/usr/local/bin/capoa-post-bootstrap.sh")
			Expect(scriptContent).NotTo(BeEmpty())

			// Should NOT contain certificate path references
			Expect(scriptContent).NotTo(ContainSubstring("/var/lib/kubelet/pki/kubelet-client-current.pem"))
			Expect(scriptContent).NotTo(ContainSubstring("CERT_PATH"))
			Expect(scriptContent).NotTo(ContainSubstring("Waiting for kubelet client certificate"))

			// Should still contain kubectl API wait
			Expect(scriptContent).To(ContainSubstring("KUBECONFIG_PATH"))
			Expect(scriptContent).To(ContainSubstring("/var/lib/kubelet/kubeconfig"))
			Expect(scriptContent).To(ContainSubstring("kubectl --kubeconfig"))

			// Should contain node existence check
			Expect(scriptContent).To(ContainSubstring("Waiting for node"))
			Expect(scriptContent).To(ContainSubstring("$(hostname)"))
		})

		It("should include node existence check in post-bootstrap script", func() {
			opts := IgnitionOptions{
				PostBootstrapCommands: []string{
					"echo 'post-kubelet setup'",
				},
			}
			i, err := GetIgnitionConfigOverrides(opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, _, err := config_31.Parse([]byte(i))
			Expect(err).NotTo(HaveOccurred())

			scriptContent := extractFileContent(cfg, "/usr/local/bin/capoa-post-bootstrap.sh")
			Expect(scriptContent).NotTo(BeEmpty())

			// Should contain node existence check (Stage 2)
			Expect(scriptContent).To(ContainSubstring("kubectl"))
			Expect(scriptContent).To(ContainSubstring("get node"))
			Expect(scriptContent).To(ContainSubstring("$(hostname)"))
			Expect(scriptContent).To(ContainSubstring("Waiting for node"))
			Expect(scriptContent).To(ContainSubstring("to join cluster"))
			Expect(scriptContent).To(ContainSubstring("Node registered in cluster"))

			// Should still contain API accessibility check (Stage 1)
			Expect(scriptContent).To(ContainSubstring("Waiting for kube API accessibility"))
			Expect(scriptContent).To(ContainSubstring("Kube API accessible"))
		})

		It("should use custom kubeconfig path when specified", func() {
			opts := IgnitionOptions{
				PostBootstrapCommands: []string{"echo 'test'"},
				KubeconfigPath:        "/custom/path/kubeconfig",
			}
			i, err := GetIgnitionConfigOverrides(opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, _, err := config_31.Parse([]byte(i))
			Expect(err).NotTo(HaveOccurred())

			scriptContent := extractFileContent(cfg, "/usr/local/bin/capoa-post-bootstrap.sh")
			Expect(scriptContent).NotTo(BeEmpty())

			// Should contain custom path
			Expect(scriptContent).To(ContainSubstring(`KUBECONFIG_PATH="/custom/path/kubeconfig"`))
			Expect(scriptContent).To(ContainSubstring(`kubectl --kubeconfig="${KUBECONFIG_PATH}"`))

			// Should NOT contain default path
			Expect(scriptContent).NotTo(ContainSubstring("/var/lib/kubelet/kubeconfig"))
		})

		It("should use default kubeconfig path when not specified", func() {
			opts := IgnitionOptions{
				PostBootstrapCommands: []string{"echo 'test'"},
				KubeconfigPath:        "", // empty = use default
			}
			i, err := GetIgnitionConfigOverrides(opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, _, err := config_31.Parse([]byte(i))
			Expect(err).NotTo(HaveOccurred())

			scriptContent := extractFileContent(cfg, "/usr/local/bin/capoa-post-bootstrap.sh")
			Expect(scriptContent).NotTo(BeEmpty())

			// Should contain default path
			Expect(scriptContent).To(ContainSubstring(`KUBECONFIG_PATH="/var/lib/kubelet/kubeconfig"`))
		})
	})

	When("generating ignition with both pre and post bootstrap commands", func() {
		It("should include both units and scripts", func() {
			opts := IgnitionOptions{
				PreBootstrapCommands:  []string{"echo pre"},
				PostBootstrapCommands: []string{"echo post"},
			}
			i, err := GetIgnitionConfigOverrides(opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, rep, err := config_31.Parse([]byte(i))
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())

			unitNames := make(map[string]bool)
			for _, unit := range cfg.Systemd.Units {
				unitNames[unit.Name] = true
			}
			Expect(unitNames).To(HaveKey("capoa-pre-bootstrap.service"))
			Expect(unitNames).To(HaveKey("capoa-post-bootstrap.service"))

			filePaths := make(map[string]bool)
			for _, file := range cfg.Storage.Files {
				filePaths[file.Path] = true
			}
			Expect(filePaths).To(HaveKey("/usr/local/bin/capoa-pre-bootstrap.sh"))
			Expect(filePaths).To(HaveKey("/usr/local/bin/capoa-post-bootstrap.sh"))
		})

		It("should work alongside hostname unit", func() {
			opts := IgnitionOptions{
				NodeNameEnvVar:        "$METADATA_NAME",
				PreBootstrapCommands:  []string{"echo pre"},
				PostBootstrapCommands: []string{"echo post"},
			}
			i, err := GetIgnitionConfigOverrides(opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, rep, err := config_31.Parse([]byte(i))
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())

			unitNames := make(map[string]bool)
			for _, unit := range cfg.Systemd.Units {
				unitNames[unit.Name] = true
			}
			Expect(unitNames).To(HaveKey("capoa-pre-bootstrap.service"))
			Expect(unitNames).To(HaveKey("capoa-post-bootstrap.service"))
			Expect(unitNames).To(HaveKey("set-hostname.service"))
		})
	})

	When("merging ignition with bootstrap commands via MergeIgnitionConfig", func() {
		const baseIgnition = `{"ignition":{"version":"3.1.0"},"storage":{"files":[{"path":"/base-file","contents":{"source":"data:,"},"mode":384}]}}`

		It("should include pre-bootstrap unit when merging with base ignition", func() {
			opts := IgnitionOptions{
				PreBootstrapCommands: []string{"echo merged-pre"},
			}
			merged, err := MergeIgnitionConfig(logr.Discard(), []byte(baseIgnition), opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, _, err := config_31.Parse(merged)
			Expect(err).NotTo(HaveOccurred())

			var foundUnit bool
			for _, unit := range cfg.Systemd.Units {
				if unit.Name == "capoa-pre-bootstrap.service" {
					foundUnit = true
				}
			}
			Expect(foundUnit).To(BeTrue(), "capoa-pre-bootstrap.service should be in merged config")

			scriptContent := extractFileContent(cfg, "/usr/local/bin/capoa-pre-bootstrap.sh")
			Expect(scriptContent).To(ContainSubstring("echo merged-pre"))
		})

		It("should not include bootstrap units when no commands specified", func() {
			opts := IgnitionOptions{}
			merged, err := MergeIgnitionConfig(logr.Discard(), []byte(baseIgnition), opts)
			Expect(err).NotTo(HaveOccurred())

			// When no options are set, base is returned unchanged
			Expect(merged).To(Equal([]byte(baseIgnition)))
		})
	})

	When("merging ignition configs", func() {
		const base = `{"ignition":{"version":"3.1.0"},"storage":{"files":[{"path":"/base","contents":{"source":"data:,"},"mode":384}]}}`
		const override = `{"ignition":{"version":"3.1.0"},"storage":{"files":[{"path":"/override","contents":{"source":"data:,"},"mode":384}]}}`

		It("MergeIgnitionConfigStrings merges override into base", func() {
			merged, err := MergeIgnitionConfigStrings(base, override)
			Expect(err).NotTo(HaveOccurred())
			cfg, _, err := config_31.Parse([]byte(merged))
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Storage.Files).To(HaveLen(2))
		})

		It("MergeIgnitionConfigStrings returns base when override is empty", func() {
			merged, err := MergeIgnitionConfigStrings(base, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(merged).To(Equal(base))
		})
	})

	When("generating ignition with providerID via GetIgnitionConfigOverrides", func() {
		It("should include kubelet provider-id drop-in when ProviderID is set", func() {
			// Create a kubelet_custom_labels file (required when providerID is set)
			scriptContent := "#!/bin/bash\necho test\n"
			b64Content := base64.StdEncoding.EncodeToString([]byte(scriptContent))
			kubeletCustomLabelsFile := CreateIgnitionFile("/usr/local/bin/kubelet_custom_labels",
				"root", "data:text/plain;charset=utf-8;base64,"+b64Content, 493, true)

			opts := IgnitionOptions{
				ProviderID:              "openstack://$METADATA_UUID",
				KubeletCustomLabelsFile: &kubeletCustomLabelsFile,
			}
			i, err := GetIgnitionConfigOverrides(opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, rep, err := config_31.Parse([]byte(i))
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())

			var foundDropin bool
			for _, file := range cfg.Storage.Files {
				if file.Path == providerIDDropinPath {
					foundDropin = true
					content, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(*file.Contents.Source, "data:text/plain;charset=utf-8;base64,"))
					Expect(err).NotTo(HaveOccurred())
					contentStr := string(content)
					Expect(contentStr).To(ContainSubstring("ExecStartPre"))
					Expect(contentStr).To(ContainSubstring("/run/kubelet-provider-id"))
					break
				}
			}
			Expect(foundDropin).To(BeTrue(), "kubelet provider-id drop-in should be present in GetIgnitionConfigOverrides output")
		})

		It("should not include kubelet provider-id drop-in when ProviderID is empty", func() {
			opts := IgnitionOptions{
				NodeNameEnvVar: "$METADATA_NAME",
			}
			i, err := GetIgnitionConfigOverrides(opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, rep, err := config_31.Parse([]byte(i))
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())

			for _, file := range cfg.Storage.Files {
				Expect(file.Path).NotTo(Equal(providerIDDropinPath))
			}
		})

		It("should include configdrive-metadata and kubelet-customlabels only when needed", func() {
			opts := IgnitionOptions{}
			i, err := GetIgnitionConfigOverrides(opts)
			Expect(err).NotTo(HaveOccurred())
			cfg, rep, err := config_31.Parse([]byte(i))
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())

			// With empty opts, no units should be present
			Expect(cfg.Systemd.Units).To(BeEmpty())
			// No installed-node files should be present
			for _, file := range cfg.Storage.Files {
				Expect(file.Path).NotTo(Equal("/usr/local/bin/configdrive_metadata"))
				Expect(file.Path).NotTo(Equal("/usr/local/bin/kubelet_custom_labels"))
			}
		})
	})

	When("generating kubelet provider-id injector", func() {
		It("should generate systemd drop-in that injects provider-id dynamically", func() {
			dropinFile := GetKubeletProviderIDInjector()

			Expect(dropinFile.Path).To(Equal(providerIDDropinPath))
			Expect(*dropinFile.User.Name).To(Equal("root"))
			Expect(*dropinFile.Mode).To(Equal(0644))
			Expect(*dropinFile.Overwrite).To(BeTrue())

			Expect(dropinFile.Contents.Source).NotTo(BeNil())
			content, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(*dropinFile.Contents.Source, "data:text/plain;charset=utf-8;base64,"))
			Expect(err).NotTo(HaveOccurred())

			contentStr := string(content)
			Expect(contentStr).To(ContainSubstring("[Service]"))
			Expect(contentStr).To(ContainSubstring("ExecStartPre"))
			Expect(contentStr).To(ContainSubstring("/run/kubelet-provider-id"))
			Expect(contentStr).To(ContainSubstring("sed -i"))
			Expect(contentStr).To(ContainSubstring("/usr/local/bin/kubelet.sh"))
		})
	})

	When("merging ignition with providerID", func() {
		It("should include kubelet provider-id drop-in when ProviderID is set", func() {
			baseIgnition := baseIgnitionJSON

			opts := IgnitionOptions{
				ProviderID: "openstack://$METADATA_UUID",
			}

			mergedIgnition, err := MergeIgnitionConfig(logr.Discard(), []byte(baseIgnition), opts)
			Expect(err).NotTo(HaveOccurred())

			cfg, rep, err := config_31.Parse(mergedIgnition)
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())

			var found bool
			for _, file := range cfg.Storage.Files {
				if file.Path == providerIDDropinPath {
					found = true
					Expect(*file.User.Name).To(Equal("root"))
					Expect(*file.Mode).To(Equal(0644))

					content, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(*file.Contents.Source, "data:text/plain;charset=utf-8;base64,"))
					Expect(err).NotTo(HaveOccurred())
					contentStr := string(content)
					Expect(contentStr).To(ContainSubstring("ExecStartPre"))
					Expect(contentStr).To(ContainSubstring("/run/kubelet-provider-id"))
					Expect(contentStr).To(ContainSubstring("sed -i"))
					break
				}
			}
			Expect(found).To(BeTrue(), "kubelet provider-id drop-in file should be present")
		})

		It("should not include kubelet provider-id drop-in when ProviderID is empty", func() {
			baseIgnition := baseIgnitionJSON

			opts := IgnitionOptions{
				NodeNameEnvVar: "$METADATA_NAME",
			}

			mergedIgnition, err := MergeIgnitionConfig(logr.Discard(), []byte(baseIgnition), opts)
			Expect(err).NotTo(HaveOccurred())

			cfg, rep, err := config_31.Parse(mergedIgnition)
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())

			for _, file := range cfg.Storage.Files {
				Expect(file.Path).NotTo(Equal(providerIDDropinPath))
			}
		})

		It("should include kubelet_custom_labels file and unit when KubeletCustomLabelsFile is set", func() {
			baseIgnition := baseIgnitionJSON

			// Create a sample kubelet_custom_labels file
			scriptContent := "#!/bin/bash\necho test\n"
			b64Content := base64.StdEncoding.EncodeToString([]byte(scriptContent))
			kubeletCustomLabelsFile := CreateIgnitionFile("/usr/local/bin/kubelet_custom_labels",
				"root", "data:text/plain;charset=utf-8;base64,"+b64Content, 493, true)

			opts := IgnitionOptions{
				ProviderID:              "openstack://$METADATA_UUID",
				KubeletCustomLabelsFile: &kubeletCustomLabelsFile,
			}

			mergedIgnition, err := MergeIgnitionConfig(logr.Discard(), []byte(baseIgnition), opts)
			Expect(err).NotTo(HaveOccurred())

			cfg, rep, err := config_31.Parse(mergedIgnition)
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())

			// Verify kubelet_custom_labels file is present
			var foundFile bool
			for _, file := range cfg.Storage.Files {
				if file.Path == "/usr/local/bin/kubelet_custom_labels" {
					foundFile = true
					Expect(*file.User.Name).To(Equal("root"))
					Expect(*file.Mode).To(Equal(493))
					break
				}
			}
			Expect(foundFile).To(BeTrue(), "kubelet_custom_labels file should be present")

			// Verify kubelet-customlabels.service unit is present
			var foundUnit bool
			for _, unit := range cfg.Systemd.Units {
				if unit.Name == "kubelet-customlabels.service" {
					foundUnit = true
					Expect(*unit.Enabled).To(BeTrue())
					Expect(*unit.Contents).To(ContainSubstring("ExecStart=/usr/local/bin/kubelet_custom_labels"))
					Expect(*unit.Contents).To(ContainSubstring("Before=kubelet.service"))
					break
				}
			}
			Expect(foundUnit).To(BeTrue(), "kubelet-customlabels.service unit should be present")

			// Verify kubelet provider-id drop-in is also present
			var foundDropin bool
			for _, file := range cfg.Storage.Files {
				if file.Path == providerIDDropinPath {
					foundDropin = true
					break
				}
			}
			Expect(foundDropin).To(BeTrue(), "kubelet provider-id drop-in should be present")
		})
	})
})

func extractFileContent(cfg config_types.Config, path string) string {
	for _, file := range cfg.Storage.Files {
		if file.Path == path {
			if file.Contents.Source == nil {
				return ""
			}
			source := *file.Contents.Source
			if strings.HasPrefix(source, "data:text/plain;charset=utf-8;base64,") {
				b64Data := strings.TrimPrefix(source, "data:text/plain;charset=utf-8;base64,")
				decoded, err := base64.StdEncoding.DecodeString(b64Data)
				Expect(err).NotTo(HaveOccurred())
				return string(decoded)
			}
			return source
		}
	}
	return ""
}
