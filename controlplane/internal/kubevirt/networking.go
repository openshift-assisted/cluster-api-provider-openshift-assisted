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

package kubevirt

// VMNetworkingRequirements documents the networking constraints applied to
// KubevirtMachines for correct operation on OVN-Kubernetes.
//
// The approach follows HyperShift's (openshift/hypershift) proven mechanism:
//
// 1. Interface binding MUST be bridge: {} on the default pod network.
//    - Masquerade gives all VMs the same internal IP (10.0.2.2), breaking etcd.
//    - Bridge binding allows OVN-Kubernetes to deliver a unique, routable IP to each VM.
//
// 2. The VMI MUST have the annotation:
//    kubevirt.io/allow-pod-bridge-network-live-migration: ""
//    This triggers OVN-Kubernetes to:
//    - Skip IP assignment at the virt-launcher pod's network namespace
//    - Serve the allocated IP to the VM via DHCP (OVN LSP DHCP options)
//    - Enable point-to-point routing for cross-node traffic
//    - Support transparent live migration
//
// 3. EvictionStrategy MUST be "LiveMigrateIfPossible".
//    - On RWX storage: VMs live-migrate transparently during node drains.
//    - On RWO storage: VMs receive ACPI shutdown (graceful), then restart.
//    - This ensures etcd and other stateful services shut down cleanly,
//      preventing data corruption during infra cluster upgrades.
//
// 4. DNS does NOT need to be overridden.
//    - OVN-Kubernetes DHCP provides proper network configuration to the VM.
//    - The VM's guest OS uses standard DHCP-provided DNS.
type VMNetworkingRequirements struct{}

// IsPodNetworking returns true when the cluster uses pod-based networking
// (no VIPs configured). Pod-networking clusters need a DNS proxy, MCS proxy,
// reduced MTU, and other infrastructure that bridge-networking clusters don't.
func IsPodNetworking(apiVIPs, ingressVIPs []string) bool {
	return len(apiVIPs) == 0 || len(ingressVIPs) == 0
}

// IsBridgeNetworking returns true when the cluster uses bridge networking
// with keepalived-managed VIPs. These clusters use BareMetal platform type
// and rely on standard DNS resolution via VIPs.
func IsBridgeNetworking(apiVIPs, ingressVIPs []string) bool {
	return len(apiVIPs) > 0 && len(ingressVIPs) > 0
}
