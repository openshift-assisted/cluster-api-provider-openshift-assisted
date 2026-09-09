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

package controller

import (
	"testing"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
)

func TestGetClusterNetworks(t *testing.T) {
	tests := []struct {
		name         string
		podCIDRs     []string
		serviceCIDRs []string
		expected     []hiveext.ClusterNetworkEntry
	}{
		{
			name:         "IPv4",
			podCIDRs:     []string{"10.128.0.0/14"},
			serviceCIDRs: []string{"172.30.0.0/16"},
			expected: []hiveext.ClusterNetworkEntry{
				{CIDR: "10.128.0.0/14", HostPrefix: 23},
			},
		},
		{
			name:         "IPv6",
			podCIDRs:     []string{"fd01::/48"},
			serviceCIDRs: []string{"fd02::/112"},
			expected: []hiveext.ClusterNetworkEntry{
				{CIDR: "fd01::/48", HostPrefix: 64},
			},
		},
		{
			name:         "dual stack",
			podCIDRs:     []string{"10.128.0.0/14", "fd01::/48"},
			serviceCIDRs: []string{"172.30.0.0/16", "fd02::/112"},
			expected: []hiveext.ClusterNetworkEntry{
				{CIDR: "10.128.0.0/14", HostPrefix: 23},
				{CIDR: "fd01::/48", HostPrefix: 64},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &clusterv1.Cluster{}
			cluster.Spec.ClusterNetwork.Pods.CIDRBlocks = tt.podCIDRs
			cluster.Spec.ClusterNetwork.Services.CIDRBlocks = tt.serviceCIDRs

			clusterNetwork, serviceNetwork := getClusterNetworks(cluster)

			if len(clusterNetwork) != len(tt.expected) {
				t.Fatalf("expected %d cluster network entries, got %d", len(tt.expected), len(clusterNetwork))
			}
			for i := range tt.expected {
				if clusterNetwork[i] != tt.expected[i] {
					t.Errorf("cluster network entry %d: expected %#v, got %#v", i, tt.expected[i], clusterNetwork[i])
				}
			}
			if len(serviceNetwork) != len(tt.serviceCIDRs) {
				t.Fatalf("expected %d service network entries, got %d", len(tt.serviceCIDRs), len(serviceNetwork))
			}
			for i := range tt.serviceCIDRs {
				if serviceNetwork[i] != tt.serviceCIDRs[i] {
					t.Errorf("service network entry %d: expected %q, got %q", i, tt.serviceCIDRs[i], serviceNetwork[i])
				}
			}
		})
	}
}
