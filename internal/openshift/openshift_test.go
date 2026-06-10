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

package openshift

import (
	"context"
	"fmt"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	restclient "k8s.io/client-go/rest"
	openapi_v2 "github.com/google/gnostic-models/openapiv2"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/openapi"
)

type fakeDiscovery struct {
	serverResourcesFn func(groupVersion string) (*metav1.APIResourceList, error)
}

func (f *fakeDiscovery) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	return f.serverResourcesFn(groupVersion)
}

func (f *fakeDiscovery) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	return nil, nil, nil
}

func (f *fakeDiscovery) ServerPreferredResources() ([]*metav1.APIResourceList, error) {
	return nil, nil
}

func (f *fakeDiscovery) ServerPreferredNamespacedResources() ([]*metav1.APIResourceList, error) {
	return nil, nil
}

func (f *fakeDiscovery) ServerGroups() (*metav1.APIGroupList, error) {
	return nil, nil
}

func (f *fakeDiscovery) ServerVersion() (*version.Info, error) {
	return nil, nil
}

func (f *fakeDiscovery) OpenAPISchema() (*openapi_v2.Document, error) {
	return nil, nil
}

func (f *fakeDiscovery) OpenAPIV3() openapi.Client {
	return nil
}

func (f *fakeDiscovery) RESTClient() restclient.Interface {
	return nil
}

func (f *fakeDiscovery) WithLegacy() discovery.DiscoveryInterface {
	return f
}

var _ discovery.DiscoveryInterface = &fakeDiscovery{}

var _ = Describe("isOpenShiftWithDiscovery", func() {
	It("should detect OpenShift when apiservers resource exists", func() {
		disc := &fakeDiscovery{
			serverResourcesFn: func(_ string) (*metav1.APIResourceList, error) {
				return &metav1.APIResourceList{
					APIResources: []metav1.APIResource{
						{Name: "apiservers"},
					},
				}, nil
			},
		}

		result, err := isOpenShiftWithDiscovery(context.Background(), disc)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeTrue())
	})

	It("should detect vanilla K8s when API group does not exist", func() {
		disc := &fakeDiscovery{
			serverResourcesFn: func(_ string) (*metav1.APIResourceList, error) {
				return nil, apierrors.NewNotFound(
					schema.GroupResource{Group: "config.openshift.io", Resource: "apiservers"}, "")
			},
		}

		result, err := isOpenShiftWithDiscovery(context.Background(), disc)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeFalse())
	})

	It("should detect vanilla K8s when API group exists but apiservers resource is missing", func() {
		disc := &fakeDiscovery{
			serverResourcesFn: func(_ string) (*metav1.APIResourceList, error) {
				return &metav1.APIResourceList{
					APIResources: []metav1.APIResource{
						{Name: "infrastructures"},
						{Name: "ingresses"},
					},
				}, nil
			},
		}

		result, err := isOpenShiftWithDiscovery(context.Background(), disc)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeFalse())
	})

	It("should retry on transient API error then succeed", func() {
		var calls int32
		disc := &fakeDiscovery{
			serverResourcesFn: func(_ string) (*metav1.APIResourceList, error) {
				call := atomic.AddInt32(&calls, 1)
				if call == 1 {
					return nil, fmt.Errorf("connection refused")
				}

				return &metav1.APIResourceList{
					APIResources: []metav1.APIResource{
						{Name: "apiservers"},
					},
				}, nil
			},
		}

		result, err := isOpenShiftWithDiscovery(context.Background(), disc)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeTrue())
		Expect(atomic.LoadInt32(&calls)).To(BeNumerically("==", 2))
	})

	It("should return error when context is cancelled during retry", func() {
		ctx, cancel := context.WithCancel(context.Background())
		disc := &fakeDiscovery{
			serverResourcesFn: func(_ string) (*metav1.APIResourceList, error) {
				cancel()

				return nil, fmt.Errorf("connection refused")
			},
		}

		result, err := isOpenShiftWithDiscovery(ctx, disc)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("waiting for API server"))
		Expect(result).To(BeFalse())
	})

	It("should return false when resource list is empty", func() {
		disc := &fakeDiscovery{
			serverResourcesFn: func(_ string) (*metav1.APIResourceList, error) {
				return &metav1.APIResourceList{
					APIResources: []metav1.APIResource{},
				}, nil
			},
		}

		result, err := isOpenShiftWithDiscovery(context.Background(), disc)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeFalse())
	})
})
