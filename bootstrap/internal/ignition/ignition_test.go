package ignition

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	config_31 "github.com/coreos/ignition/v2/config/v3_1"
)

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

	When("merging ignition configs", func() {
		const base = `{"ignition":{"version":"3.1.0"},"storage":{"files":[{"path":"/base","contents":{"source":"data:,"},"mode":384}]}}`
		const override = `{"ignition":{"version":"3.1.0"},"storage":{"files":[{"path":"/override","contents":{"source":"data:,"},"mode":384}]}}`

		It("MergeIgnitionConfigStrings merges override into base", func() {
			merged, err := MergeIgnitionConfigStrings(base, override)
			Expect(err).NotTo(HaveOccurred())
			cfg, _, err := config_31.Parse([]byte(merged))
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Storage.Files).To(HaveLen(2))
			var hasBase, hasOverride bool
			for _, f := range cfg.Storage.Files {
				switch f.Path {
				case "/base":
					hasBase = true
				case "/override":
					hasOverride = true
				}
			}
			Expect(hasBase).To(BeTrue())
			Expect(hasOverride).To(BeTrue())
		})

		It("MergeIgnitionConfigStrings returns base when override is empty", func() {
			merged, err := MergeIgnitionConfigStrings(base, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(merged).To(Equal(base))
		})
	})
})
