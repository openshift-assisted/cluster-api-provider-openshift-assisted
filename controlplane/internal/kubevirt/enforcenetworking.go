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
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	// LiveMigrationAnnotation is the KubeVirt annotation that triggers OVN-Kubernetes
	// to provide DHCP and handle L3 routing for bridge-bound VMs on the pod network.
	// This is the same mechanism HyperShift uses for KubeVirt hosted clusters.
	LiveMigrationAnnotation = "kubevirt.io/allow-pod-bridge-network-live-migration"
)

// ValidateNetworkingRequirements checks that a KubevirtMachineTemplate's VM spec
// has the required networking configuration for a working multi-node cluster.
//
// These requirements follow HyperShift's pattern for KubeVirt hosted clusters:
//
//  1. Interface binding MUST be bridge: {} on the default pod network.
//     Masquerade gives all VMs the same internal IP (10.0.2.2), breaking etcd
//     peer communication and kubectl exec.
//
//  2. The VMI template MUST have the annotation:
//     kubevirt.io/allow-pod-bridge-network-live-migration: ""
//     This triggers OVN-K to skip IP assignment at the virt-launcher pod netns
//     and serve the allocated IP via DHCP instead.
//
// The user is responsible for setting these in the KubevirtMachineTemplate spec.
// This function validates they are present and returns actionable error messages.
func ValidateNetworkingRequirements(
	infraMachineTemplate *unstructured.Unstructured,
) error {
	vmiSpec, found, err := unstructured.NestedMap(infraMachineTemplate.Object,
		"spec", "template", "spec", "virtualMachineTemplate", "spec", "template", "spec")
	if err != nil || !found {
		return fmt.Errorf("failed to find VMI spec in KubevirtMachineTemplate: found=%v, err=%v", found, err)
	}

	if err := validateBridgeInterface(vmiSpec); err != nil {
		return err
	}

	annotations, _, _ := unstructured.NestedStringMap(infraMachineTemplate.Object,
		"spec", "template", "spec", "virtualMachineTemplate", "spec", "template", "metadata", "annotations")
	if _, ok := annotations[LiveMigrationAnnotation]; !ok {
		return fmt.Errorf(
			"KubevirtMachineTemplate is missing the required VMI annotation %q; "+
				"without it, OVN-Kubernetes will not provide DHCP to the VM and networking will fail; "+
				"add it to spec.template.spec.virtualMachineTemplate.spec.template.metadata.annotations",
			LiveMigrationAnnotation)
	}

	return nil
}

// validateBridgeInterface checks that at least one interface uses bridge: {} binding.
func validateBridgeInterface(vmiSpec map[string]interface{}) error {
	domain, ok := vmiSpec["domain"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("domain not found in VMI spec")
	}
	devices, ok := domain["devices"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("devices not found in domain")
	}

	interfaces, ok := devices["interfaces"].([]interface{})
	if !ok || len(interfaces) == 0 {
		return fmt.Errorf(
			"KubevirtMachineTemplate has no network interfaces configured; " +
				"at least one interface with bridge: {} binding is required")
	}

	for _, iface := range interfaces {
		ifaceMap, ok := iface.(map[string]interface{})
		if !ok {
			continue
		}
		if _, hasBridge := ifaceMap["bridge"]; hasBridge {
			return nil
		}
	}

	return fmt.Errorf(
		"KubevirtMachineTemplate uses masquerade or other non-bridge binding; " +
			"bridge: {} is required on the default pod network interface; " +
			"masquerade gives all VMs the same IP (10.0.2.2), breaking etcd and kubectl exec")
}
