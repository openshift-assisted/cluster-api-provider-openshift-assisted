package kubevirt_test

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/kubevirt"
)

var _ = Describe("DNS Proxy Manifests", func() {
	Describe("GenerateDNSProxyManifests", func() {
		It("should generate configmap and daemonset manifests", func() {
			manifests := kubevirt.GenerateDNSProxyManifests("test-cluster", "apps.mgmt.example.com", "test-ns", "10.96.0.10")
			Expect(manifests).To(HaveLen(2))
			Expect(manifests[0].Filename).To(Equal("01-dns-proxy-configmap.yaml"))
			Expect(manifests[0].Content).To(ContainSubstring("test-cluster.apps.mgmt.example.com"))
			Expect(manifests[1].Filename).To(Equal("02-dns-proxy-daemonset.yaml"))
		})
	})

	Describe("GenerateTenantDNSForwarderManifests", func() {
		It("should generate a forwarder manifest with node IPs", func() {
			manifests := kubevirt.GenerateTenantDNSForwarderManifests(
				"test-cluster.apps.mgmt.example.com",
				[]string{"10.128.2.5", "10.128.2.6"},
			)
			Expect(manifests).To(HaveLen(1))
			Expect(manifests[0].Filename).To(Equal("99-tenant-dns-forwarder.yaml"))
			Expect(manifests[0].Content).To(ContainSubstring("10.128.2.5"))
			Expect(manifests[0].Content).To(ContainSubstring("test-cluster.apps.mgmt.example.com"))
		})
	})
})

var _ = Describe("Ignition Overrides", func() {
	Describe("KubeVirtDiscoveryIgnitionOverride", func() {
		It("should generate ignition JSON containing API IP in base64 encoded content", func() {
			override, err := kubevirt.KubeVirtDiscoveryIgnitionOverride(
				"ssh-rsa AAAA...", "10.96.0.100", "test-cluster", "apps.mgmt.example.com", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(override).To(ContainSubstring("ignition"))

			// The IP is base64 encoded inside the ignition script content
			for _, part := range strings.Split(override, "base64,") {
				if len(part) > 10 {
					decoded, err := base64.StdEncoding.DecodeString(strings.Split(part, "\"")[0])
					if err == nil && strings.Contains(string(decoded), "10.96.0.100") {
						Expect(string(decoded)).To(ContainSubstring("10.96.0.100"))
						return
					}
				}
			}
			Fail("API IP 10.96.0.100 not found in any base64 encoded content")
		})
	})

	Describe("KubeVirtInstallIgnitionOverride", func() {
		It("should generate valid ignition JSON with SSH key and DNS config", func() {
			override, err := kubevirt.KubeVirtInstallIgnitionOverride("ssh-rsa AAAA...", "172.30.0.10")
			Expect(err).NotTo(HaveOccurred())
			Expect(override).To(ContainSubstring("ignition"))
			Expect(override).To(ContainSubstring("ssh-rsa AAAA..."))

			Expect(decodeIgnitionFileContent(override, "/etc/resolv.conf")).To(Equal("nameserver 172.30.0.10\n"))
			Expect(decodeIgnitionFileContent(override, "/etc/NetworkManager/conf.d/99-capoa-dns.conf")).To(Equal("[main]\ndns=none\n"))
			Expect(decodeIgnitionFileContent(override, "/etc/gai.conf")).To(Equal("precedence ::ffff:0/0 100\n"))
		})

		It("should use the provided infra DNS IP", func() {
			override, err := kubevirt.KubeVirtInstallIgnitionOverride("", "10.0.0.53")
			Expect(err).NotTo(HaveOccurred())
			Expect(decodeIgnitionFileContent(override, "/etc/resolv.conf")).To(Equal("nameserver 10.0.0.53\n"))
		})

		It("should skip DNS files when infra DNS IP is empty (bridge networking)", func() {
			override, err := kubevirt.KubeVirtInstallIgnitionOverride("", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(override).To(ContainSubstring("ignition"))
			Expect(override).To(ContainSubstring("placeholder"))
			Expect(override).NotTo(ContainSubstring("resolv.conf"))
			Expect(override).NotTo(ContainSubstring("99-capoa-dns"))
			Expect(override).NotTo(ContainSubstring("gai.conf"))
		})

		It("should include both SSH key and DNS config when both are provided", func() {
			override, err := kubevirt.KubeVirtInstallIgnitionOverride("ssh-ed25519 AAAA...", "10.96.0.10")
			Expect(err).NotTo(HaveOccurred())
			Expect(override).To(ContainSubstring("ssh-ed25519 AAAA..."))
			Expect(decodeIgnitionFileContent(override, "/etc/resolv.conf")).To(Equal("nameserver 10.96.0.10\n"))
			Expect(decodeIgnitionFileContent(override, "/etc/NetworkManager/conf.d/99-capoa-dns.conf")).To(Equal("[main]\ndns=none\n"))
		})
	})
})

func decodeIgnitionFileContent(override, path string) string {
	var ignition struct {
		Storage struct {
			Files []struct {
				Path     string `json:"path"`
				Contents struct {
					Source string `json:"source"`
				} `json:"contents"`
			} `json:"files"`
		} `json:"storage"`
	}
	if err := json.Unmarshal([]byte(override), &ignition); err != nil {
		return ""
	}
	for _, f := range ignition.Storage.Files {
		if f.Path == path {
			src := f.Contents.Source
			if idx := strings.Index(src, "base64,"); idx >= 0 {
				decoded, err := base64.StdEncoding.DecodeString(src[idx+7:])
				if err == nil {
					return string(decoded)
				}
			}
		}
	}
	return ""
}
