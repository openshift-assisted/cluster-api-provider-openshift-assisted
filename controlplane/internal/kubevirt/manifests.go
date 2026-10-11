/*
Copyright 2026.

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

package kubevirt

import (
	"encoding/base64"
	"fmt"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
)

const (
	NetworkMTUConfigMapName            = "kubevirt-network-mtu-manifests"
	ResolvFixManifestsConfigMapName    = "kubevirt-resolv-fix-manifests"
	PodNetDNSFixManifestsConfigMapName = "kubevirt-pod-net-dns-fix-manifests"
	KubeVirtTenantClusterMTU           = 1300
)

// ManifestEntry represents a single manifest file to inject during installation.
type ManifestEntry struct {
	Filename string
	Content  string
}

// GenerateResolvFixManifests produces a MachineConfig that ensures DNS resolves
// correctly on first boot, before the resolv-prepender service is active.
func GenerateResolvFixManifests() []ManifestEntry {
	return []ManifestEntry{
		{
			Filename: "00-fix-resolv-firstboot.yaml",
			Content: `apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  name: 00-fix-resolv-firstboot
  labels:
    machineconfiguration.openshift.io/role: master
spec:
  config:
    ignition:
      version: 3.2.0
    systemd:
      units:
      - name: fix-resolv-firstboot.service
        enabled: true
        contents: |
          [Unit]
          Description=Ensure working DNS before resolv-prepender on first boot
          Before=on-prem-resolv-prepender.service nodeip-configuration.service
          After=NetworkManager-wait-online.service
          ConditionPathExists=/run/NetworkManager/resolv.conf

          [Service]
          Type=oneshot
          ExecStart=/bin/bash -c 'TIMEOUT=120; ELAPSED=0; while ! grep -q nameserver /run/NetworkManager/resolv.conf 2>/dev/null; do sleep 0.5; ELAPSED=$((ELAPSED+1)); if [ $ELAPSED -ge $((TIMEOUT*2)) ]; then echo "Timed out waiting for nameserver in resolv.conf" >&2; exit 1; fi; done; cp /run/NetworkManager/resolv.conf /etc/resolv.conf'

          [Install]
          WantedBy=multi-user.target
`,
		},
	}
}

// GeneratePodNetworkDNSFixManifests produces MachineConfigs that fix pod-networking
// specific issues for KubeVirt tenant clusters:
//
// 1. DNS fix: Ensures /etc/resolv.conf points to the infra CoreDNS IP so that
//    kubelet can resolve api-int and join the cluster during bootstrap.
//    DNS resolution goes through: resolv.conf → infra CoreDNS (172.30.0.10) →
//    DNS proxy → bootstrap pod IP (during installation) or service ClusterIP
//    (post-install). We deliberately do NOT add /etc/hosts entries for api-int
//    because /etc/hosts takes precedence over DNS and would bypass the DNS proxy's
//    bootstrap-aware routing. The DNS proxy correctly routes api-int to the
//    bootstrap pod's direct IP during installation, avoiding the service ClusterIP
//    which load-balances across all VM pods (including those not yet running
//    kube-apiserver).
//
// 2. TX checksum/TSO fix: Disables TX checksum offloading and TSO on the guest NIC.
//    KubeVirt 1.8+ (CNV 4.22+) enables TX checksum on the k6t bridge, causing packets
//    with CHECKSUM_PARTIAL to reach the VM. OVS inside the VM (used by OVN-Kubernetes)
//    can't handle partial checksums on raw AF_PACKET sockets, preventing Geneve tunnel
//    establishment and breaking nested OVN bootstrap.
//
//    This is applied via a udev rule (not a systemd service ordered after
//    NetworkManager-wait-online.service) so it takes effect the instant the NIC is
//    created by the kernel — before NetworkManager ever starts DHCP on it and long
//    before ovs-configuration.service runs. Applying this later, against a live
//    already-addressed interface, lets `ethtool -K` momentarily reset the NIC; that
//    reset can race with ovs-configuration.service reading the interface's IP to
//    migrate it onto br-ex, occasionally leaving the node with no usable address on
//    either interface until the VM is rebooted.
func GeneratePodNetworkDNSFixManifests(clusterName, baseDomain, _, infraCoreDNSIP string) []ManifestEntry {
	if infraCoreDNSIP == "" {
		infraCoreDNSIP = defaultInfraCoreDNSIP
	}

	resolvConf := fmt.Sprintf("nameserver %s\nsearch cluster.local svc.cluster.local\noptions ndots:5\n", infraCoreDNSIP)
	resolvConfB64 := base64.StdEncoding.EncodeToString([]byte(resolvConf))

	// udev rule disables TX checksum offloading and TSO on the guest NIC the instant
	// it's created, to fix nested OVN bootstrap on KubeVirt 1.8+ where the k6t bridge
	// enables TX checksum. Running this at udev-time (rather than via a systemd
	// service ordered after NetworkManager-wait-online.service) avoids resetting an
	// already-addressed interface, which could otherwise race with
	// ovs-configuration.service and leave the node without a usable IP.
	ethtoolUdevRule := `ACTION=="add", SUBSYSTEM=="net", NAME=="enp1s0", RUN+="/usr/sbin/ethtool -K enp1s0 tx-checksum-ip-generic off tx off tso off"
`
	ethtoolUdevRuleB64 := base64.StdEncoding.EncodeToString([]byte(ethtoolUdevRule))

	return []ManifestEntry{
		{
			Filename: "00-fix-dns-pod-network.yaml",
			Content: fmt.Sprintf(`apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  name: 00-fix-dns-pod-network
  labels:
    machineconfiguration.openshift.io/role: master
spec:
  config:
    ignition:
      version: 3.2.0
    storage:
      files:
      - path: /etc/resolv.conf
        mode: 0644
        overwrite: true
        contents:
          source: "data:text/plain;charset=utf-8;base64,%s"
      - path: /etc/NetworkManager/conf.d/99-capoa-dns.conf
        mode: 0644
        overwrite: true
        contents:
          source: "data:text/plain;charset=utf-8;base64,W21haW5dCmRucz1ub25lCg=="
      - path: /etc/udev/rules.d/99-capoa-fix-tx-offload.rules
        mode: 0644
        overwrite: true
        contents:
          source: "data:text/plain;charset=utf-8;base64,%s"
    systemd:
      units:
      - name: nodeip-configuration.service
        enabled: false
      - name: set-node-ip.service
        enabled: true
        contents: |
          [Unit]
          Description=Set node IP for kubelet (replaces nodeip-configuration for KubeVirt pod-network VMs)
          Before=kubelet-dependencies.target
          After=NetworkManager-wait-online.service

          [Service]
          Type=oneshot
          ExecStart=/bin/bash -c 'NODE_IP=$(ip -4 -o addr show enp1s0 | head -1 | cut -d" " -f4 | cut -d/ -f1); mkdir -p /etc/systemd/system/kubelet.service.d; echo -e "[Service]\nEnvironment=\"KUBELET_NODE_IP=$$NODE_IP\"" > /etc/systemd/system/kubelet.service.d/20-nodenet.conf; systemctl daemon-reload'

          [Install]
          RequiredBy=kubelet-dependencies.target
`, resolvConfB64, ethtoolUdevRuleB64),
		},
		{
			Filename: "00-fix-dns-pod-network-worker.yaml",
			Content: fmt.Sprintf(`apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  name: 00-fix-dns-pod-network-worker
  labels:
    machineconfiguration.openshift.io/role: worker
spec:
  config:
    ignition:
      version: 3.2.0
    storage:
      files:
      - path: /etc/resolv.conf
        mode: 0644
        overwrite: true
        contents:
          source: "data:text/plain;charset=utf-8;base64,%s"
      - path: /etc/NetworkManager/conf.d/99-capoa-dns.conf
        mode: 0644
        overwrite: true
        contents:
          source: "data:text/plain;charset=utf-8;base64,W21haW5dCmRucz1ub25lCg=="
      - path: /etc/udev/rules.d/99-capoa-fix-tx-offload.rules
        mode: 0644
        overwrite: true
        contents:
          source: "data:text/plain;charset=utf-8;base64,%s"
    systemd:
      units:
      - name: nodeip-configuration.service
        enabled: false
      - name: set-node-ip.service
        enabled: true
        contents: |
          [Unit]
          Description=Set node IP for kubelet (replaces nodeip-configuration for KubeVirt pod-network VMs)
          Before=kubelet-dependencies.target
          After=NetworkManager-wait-online.service

          [Service]
          Type=oneshot
          ExecStart=/bin/bash -c 'NODE_IP=$(ip -4 -o addr show enp1s0 | head -1 | cut -d" " -f4 | cut -d/ -f1); mkdir -p /etc/systemd/system/kubelet.service.d; echo -e "[Service]\nEnvironment=\"KUBELET_NODE_IP=$$NODE_IP\"" > /etc/systemd/system/kubelet.service.d/20-nodenet.conf; systemctl daemon-reload'

          [Install]
          RequiredBy=kubelet-dependencies.target
`, resolvConfB64, ethtoolUdevRuleB64),
		},
	}
}

// GenerateNetworkMTUManifests produces manifests to configure the tenant cluster's
// network MTU for KubeVirt environments.
//
// KubeVirt VMs running in bridge mode get their network interface directly from the
// pod network. On a typical infra cluster (e.g., Azure with OVN-Kubernetes), the pod
// MTU is ~1400. The tenant cluster's OVN adds another layer of Geneve encapsulation
// (58 bytes overhead), so the effective MTU for tenant pods must be reduced.
func GenerateNetworkMTUManifests() []ManifestEntry {
	return []ManifestEntry{
		{
			Filename: "01-cluster-network-mtu.yaml",
			Content: fmt.Sprintf(`apiVersion: operator.openshift.io/v1
kind: Network
metadata:
  name: cluster
spec:
  defaultNetwork:
    ovnKubernetesConfig:
      mtu: %d
      genevePort: 9880
`, KubeVirtTenantClusterMTU),
		},
	}
}

// GenerateNetworkManifests produces OVN-Kubernetes Network operator manifests
// from the user-provided spec.config.network configuration.
// Returns nil if no network configuration is specified.
func GenerateNetworkManifests(oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane) []ManifestEntry {
	if oacp.Spec.Config.Network == nil || oacp.Spec.Config.Network.OVNKubernetes == nil {
		return nil
	}

	ovn := oacp.Spec.Config.Network.OVNKubernetes
	if ovn.MTU == nil && ovn.GenevePort == nil {
		return nil
	}

	spec := "  defaultNetwork:\n    ovnKubernetesConfig:\n"
	if ovn.MTU != nil {
		spec += fmt.Sprintf("      mtu: %d\n", *ovn.MTU)
	}
	if ovn.GenevePort != nil {
		spec += fmt.Sprintf("      genevePort: %d\n", *ovn.GenevePort)
	}

	return []ManifestEntry{
		{
			Filename: "01-cluster-network-config.yaml",
			Content: fmt.Sprintf(`apiVersion: operator.openshift.io/v1
kind: Network
metadata:
  name: cluster
spec:
%s`, spec),
		},
	}
}

// indentMultiline indents every non-empty line of s by 4 spaces.
func indentMultiline(s string) string {
	const indent = "    "
	result := ""
	for i, line := range splitLines(s) {
		if i > 0 {
			result += "\n"
		}
		if line != "" {
			result += indent + line
		}
	}
	return result
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
