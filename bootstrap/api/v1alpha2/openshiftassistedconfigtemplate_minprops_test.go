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

package v1alpha2

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// These specs guard against MGMT-24965: the embedded upstream CAPI struct
// clusterv1.ObjectMeta carries +kubebuilder:validation:MinProperties=1, which
// propagates to the CRD schema as minProperties: 1 on template.metadata. Go's
// `omitempty` does not drop zero-value structs, so without `omitzero` a typed client
// leaving template.metadata unset serializes it as `{}` (0 properties) and the API
// server rejects the create with a 422.
//
// fakeclient-based tests do not enforce CRD schema, so this needs envtest against a
// real API server.
var _ = Describe("OpenshiftAssistedConfigTemplate template metadata minProperties", func() {
	var (
		ctx       context.Context
		namespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = "default"
	})

	// Regression: a typed client that leaves template.metadata as a zero-value struct
	// must be accepted. This is the exact scenario reported in MGMT-24965.
	It("accepts a typed create with zero-value template metadata", func() {
		oacct := &OpenshiftAssistedConfigTemplate{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-minprops-",
				Namespace:    namespace,
			},
			Spec: OpenshiftAssistedConfigTemplateSpec{
				Template: OpenshiftAssistedConfigTemplateResource{
					// ObjectMeta is deliberately left as a zero-value struct --
					// the omitzero tag must keep it off the wire.
				},
			},
		}

		err := k8sClient.Create(ctx, oacct, client.DryRunAll)
		Expect(err).NotTo(HaveOccurred())
	})

	// Documents intended behavior: an explicit empty metadata in the payload
	// (e.g. `metadata: {}` in raw YAML) is still correctly rejected by minProperties.
	It("rejects a create with explicit empty template metadata", func() {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(GroupVersion.WithKind("OpenshiftAssistedConfigTemplate"))
		obj.SetGenerateName("test-minprops-empty-")
		obj.SetNamespace(namespace)
		Expect(unstructured.SetNestedMap(obj.Object, map[string]interface{}{
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{},
			},
		}, "spec")).To(Succeed())

		err := k8sClient.Create(ctx, obj, client.DryRunAll)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("should have at least 1 properties"))
	})
})
