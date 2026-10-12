package controller

import (
	"testing"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

func TestDeepMerge(t *testing.T) {
	tests := []struct {
		name     string
		json1    string
		json2    string
		expected string
	}{
		{
			name:     "disjoint keys",
			json1:    `{"a": 1}`,
			json2:    `{"b": 2}`,
			expected: `{"a":1,"b":2}`,
		},
		{
			name:     "scalar override",
			json1:    `{"a": 1}`,
			json2:    `{"a": 2}`,
			expected: `{"a":2}`,
		},
		{
			name:     "nested objects are merged not replaced",
			json1:    `{"capabilities":{"baseline":"vCurrent","additional":["Foo"]}}`,
			json2:    `{"capabilities":{"baseline":"None"}}`,
			expected: `{"capabilities":{"additional":["Foo"],"baseline":"None"}}`,
		},
		{
			name:     "deeply nested merge",
			json1:    `{"a":{"b":{"c":1,"d":2}}}`,
			json2:    `{"a":{"b":{"c":3,"e":4}}}`,
			expected: `{"a":{"b":{"c":3,"d":2,"e":4}}}`,
		},
		{
			name:     "empty first json",
			json1:    `{}`,
			json2:    `{"a":1}`,
			expected: `{"a":1}`,
		},
		{
			name:     "empty second json",
			json1:    `{"a":1}`,
			json2:    `{}`,
			expected: `{"a":1}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := mergeJson(tt.json1, tt.json2)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("got %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestResolveInfraVersion(t *testing.T) {
	tests := []struct {
		name     string
		group    string
		kind     string
		expected string
	}{
		{
			name:     "KubevirtCluster maps to v1alpha1",
			group:    "infrastructure.cluster.x-k8s.io",
			kind:     "KubevirtCluster",
			expected: "v1alpha1",
		},
		{
			name:     "KubevirtMachine maps to v1alpha1",
			group:    "infrastructure.cluster.x-k8s.io",
			kind:     "KubevirtMachine",
			expected: "v1alpha1",
		},
		{
			name:     "Metal3Cluster maps to v1beta1",
			group:    "infrastructure.cluster.x-k8s.io",
			kind:     "Metal3Cluster",
			expected: "v1beta1",
		},
		{
			name:     "Metal3MachineTemplate maps to v1beta1",
			group:    "infrastructure.cluster.x-k8s.io",
			kind:     "Metal3MachineTemplate",
			expected: "v1beta1",
		},
		{
			name:     "bootstrap OpenshiftAssistedConfig maps to v1alpha2",
			group:    "bootstrap.cluster.x-k8s.io",
			kind:     "OpenshiftAssistedConfig",
			expected: "v1alpha2",
		},
		{
			name:     "controlplane OpenshiftAssistedControlPlane maps to v1alpha3",
			group:    "controlplane.cluster.x-k8s.io",
			kind:     "OpenshiftAssistedControlPlane",
			expected: "v1alpha3",
		},
		{
			name:     "unknown group+kind defaults to v1beta1",
			group:    "custom.example.io",
			kind:     "CustomMachine",
			expected: "v1beta1",
		},
		{
			name:     "empty group+kind defaults to v1beta1",
			group:    "",
			kind:     "",
			expected: "v1beta1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref := clusterv1.ContractVersionedObjectReference{
				APIGroup: tt.group,
				Kind:     tt.kind,
			}
			result := resolveInfraVersion(ref)
			if result != tt.expected {
				t.Errorf("got %s, want %s", result, tt.expected)
			}
		})
	}
}
