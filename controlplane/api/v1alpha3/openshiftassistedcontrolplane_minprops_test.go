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

package v1alpha3

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// These specs guard against MGMT-24965: the embedded upstream CAPI structs
// clusterv1.ObjectMeta and clusterv1.MachineDeletionSpec carry
// +kubebuilder:validation:MinProperties=1, which propagates to the CRD schema as
// minProperties: 1. Go's `omitempty` does not drop zero-value structs, so without
// `omitzero` a typed client leaving machineTemplate.metadata/deletion unset serializes
// them as `{}` (0 properties) and the API server rejects the create with a 422.
//
// fakeclient-based tests do not enforce CRD schema, so this needs envtest against a
// real API server.
var _ = Describe("OpenshiftAssistedControlPlane machineTemplate minProperties", func() {
	var (
		ctx       context.Context
		namespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = "default"
	})

	// Regression: a typed client that leaves metadata and deletion as zero-value
	// structs must be accepted. This is the exact scenario reported in MGMT-24965.
	It("accepts a typed create with zero-value metadata and deletion", func() {
		oacp := &OpenshiftAssistedControlPlane{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-minprops-",
				Namespace:    namespace,
			},
			Spec: OpenshiftAssistedControlPlaneSpec{
				DistributionVersion: "4.18.0",
				Replicas:            1,
				Config: OpenshiftAssistedControlPlaneConfigSpec{
					BaseDomain:    "example.com",
					PullSecretRef: &corev1.LocalObjectReference{Name: "pull-secret"},
				},
				MachineTemplate: OpenshiftAssistedControlPlaneMachineTemplate{
					// ObjectMeta and Deletion are deliberately left as zero-value
					// structs -- the omitzero tag must keep them off the wire.
					InfrastructureRef: clusterv1.ContractVersionedObjectReference{
						APIGroup: "infrastructure.cluster.x-k8s.io",
						Kind:     "Metal3MachineTemplate",
						Name:     "test-mt",
					},
				},
			},
		}

		err := k8sClient.Create(ctx, oacp, client.DryRunAll)
		Expect(err).NotTo(HaveOccurred())
	})

	// Documents intended behavior: an explicit empty metadata/deletion in the payload
	// (e.g. `metadata: {}` in raw YAML) is still correctly rejected by minProperties.
	It("rejects a create with explicit empty metadata and deletion", func() {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(GroupVersion.WithKind("OpenshiftAssistedControlPlane"))
		obj.SetGenerateName("test-minprops-empty-")
		obj.SetNamespace(namespace)
		Expect(unstructured.SetNestedMap(obj.Object, map[string]interface{}{
			"distributionVersion": "4.18.0",
			"config": map[string]interface{}{
				"baseDomain": "example.com",
			},
			"machineTemplate": map[string]interface{}{
				"metadata": map[string]interface{}{},
				"deletion": map[string]interface{}{},
				"infrastructureRef": map[string]interface{}{
					"apiGroup": "infrastructure.cluster.x-k8s.io",
					"kind":     "Metal3MachineTemplate",
					"name":     "test-mt",
				},
			},
		}, "spec")).To(Succeed())

		err := k8sClient.Create(ctx, obj, client.DryRunAll)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("should have at least 1 properties"))
	})
})
