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
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

var _ = Describe("CCM egress destinations", func() {
	var (
		ctx    context.Context
		scheme *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(discoveryv1.AddToScheme(scheme)).To(Succeed())
	})

	It("returns only ready tenant API endpoint addresses", func() {
		ready := true
		notReady := false
		endpointSlice := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster-api",
				Namespace: "infra-ns",
				Labels:    map[string]string{discoveryv1.LabelServiceName: "test-cluster-api"},
			},
			Endpoints: []discoveryv1.Endpoint{
				{
					Addresses:  []string{"10.132.0.10"},
					Conditions: discoveryv1.EndpointConditions{Ready: &ready},
				},
				{
					Addresses:  []string{"10.132.0.11"},
					Conditions: discoveryv1.EndpointConditions{Ready: &notReady},
				},
				{
					Addresses:  []string{"fd00:132::10"},
					Conditions: discoveryv1.EndpointConditions{Ready: &ready},
				},
			},
		}
		infraClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(endpointSlice).Build()

		destinations, err := GetTenantAPIEndpointCIDRs(ctx, infraClient, "test-cluster", "infra-ns")
		Expect(err).NotTo(HaveOccurred())
		Expect(destinations).To(Equal([]string{"10.132.0.10/32", "fd00:132::10/128"}))
	})

	It("treats a forbidden Kubernetes Service read as optional", func() {
		infraClient := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, clientWithWatch client.WithWatch, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
				if key == (types.NamespacedName{Name: "kubernetes", Namespace: "default"}) {
					return apierrors.NewForbidden(discoveryv1.Resource("services"), "kubernetes", errors.New("denied"))
				}
				return clientWithWatch.Get(ctx, key, object, options...)
			},
		}).Build()

		serviceIP, err := GetOptionalInfraKubernetesServiceIP(ctx, infraClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(serviceIP).To(BeEmpty())
	})

	It("uses the original kubeconfig server IP when the Service is unavailable", func() {
		destinations, err := kubeconfigServerDestinationCIDRs(`apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://192.0.2.50:6443
  name: infra
contexts:
- context:
    cluster: infra
    user: infra
  name: infra
current-context: infra
users:
- name: infra
  user: {}
`)
		Expect(err).NotTo(HaveOccurred())
		Expect(destinations.InfraAPI).To(Equal([]string{"192.0.2.50/32"}))
		Expect(destinations.InfraAPIPorts).To(Equal([]int32{6443}))
		Expect(destinations.InfraAPIHost).To(BeEmpty())
	})

	It("uses the kubeconfig proxy destination when the Service is unavailable", func() {
		destinations, err := kubeconfigServerDestinationCIDRs(`apiVersion: v1
kind: Config
clusters:
- cluster:
    proxy-url: http://192.0.2.99:8443
    server: https://api.infra.example.com:6443
  name: infra
contexts:
- context:
    cluster: infra
    user: infra
  name: infra
current-context: infra
users:
- name: infra
  user: {}
`)
		Expect(err).NotTo(HaveOccurred())
		Expect(destinations.InfraAPI).To(Equal([]string{"192.0.2.99/32"}))
		Expect(destinations.InfraAPIPorts).To(Equal([]int32{8443}))
		Expect(destinations.InfraAPIHost).To(BeEmpty())
	})
})
