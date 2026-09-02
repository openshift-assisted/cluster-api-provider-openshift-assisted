package kubevirt_test

import (
	"encoding/base64"
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
		It("should generate valid ignition JSON with SSH key", func() {
			override, err := kubevirt.KubeVirtInstallIgnitionOverride("ssh-rsa AAAA...")
			Expect(err).NotTo(HaveOccurred())
			Expect(override).To(ContainSubstring("ignition"))
			Expect(override).To(ContainSubstring("ssh-rsa AAAA..."))
		})
	})
})
