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

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
)

const NetworkMTUConfigMapName = "kubevirt-network-mtu-manifests"

// ManifestEntry represents a single manifest file to inject during installation.
type ManifestEntry struct {
	Filename string
	Content  string
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

// indentMultiline indents every non-empty line of s by the given number of spaces.
func indentMultiline(s string, spaces int) string {
	indent := ""
	for i := 0; i < spaces; i++ {
		indent += " "
	}
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
