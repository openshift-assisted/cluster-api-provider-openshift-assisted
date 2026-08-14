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

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/external_mocks"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var _ = Describe("isOpenShiftWithDiscovery", func() {
	var (
		mockCtrl *gomock.Controller
		disc     *external_mocks.MockDiscoveryInterface
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		disc = external_mocks.NewMockDiscoveryInterface(mockCtrl)
	})

	It("should detect OpenShift when apiservers resource exists", func() {
		disc.EXPECT().
			ServerResourcesForGroupVersion(gomock.Any()).
			Return(&metav1.APIResourceList{
				APIResources: []metav1.APIResource{
					{Name: "apiservers"},
				},
			}, nil)

		result, err := isOpenShiftWithDiscovery(context.Background(), disc)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeTrue())
	})

	It("should detect vanilla K8s when API group does not exist", func() {
		disc.EXPECT().
			ServerResourcesForGroupVersion(gomock.Any()).
			Return(nil, apierrors.NewNotFound(
				schema.GroupResource{Group: "config.openshift.io", Resource: "apiservers"}, ""))

		result, err := isOpenShiftWithDiscovery(context.Background(), disc)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeFalse())
	})

	It("should detect vanilla K8s when API group exists but apiservers resource is missing", func() {
		disc.EXPECT().
			ServerResourcesForGroupVersion(gomock.Any()).
			Return(&metav1.APIResourceList{
				APIResources: []metav1.APIResource{
					{Name: "infrastructures"},
					{Name: "ingresses"},
				},
			}, nil)

		result, err := isOpenShiftWithDiscovery(context.Background(), disc)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeFalse())
	})

	It("should retry on transient API error then succeed", func() {
		var calls int32
		disc.EXPECT().
			ServerResourcesForGroupVersion(gomock.Any()).
			DoAndReturn(func(_ string) (*metav1.APIResourceList, error) {
				call := atomic.AddInt32(&calls, 1)
				if call == 1 {
					return nil, fmt.Errorf("connection refused")
				}
				return &metav1.APIResourceList{
					APIResources: []metav1.APIResource{
						{Name: "apiservers"},
					},
				}, nil
			}).Times(2)

		result, err := isOpenShiftWithDiscovery(context.Background(), disc)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeTrue())
		Expect(atomic.LoadInt32(&calls)).To(BeNumerically("==", 2))
	})

	It("should return error when context is cancelled during retry", func() {
		ctx, cancel := context.WithCancel(context.Background())
		disc.EXPECT().
			ServerResourcesForGroupVersion(gomock.Any()).
			DoAndReturn(func(_ string) (*metav1.APIResourceList, error) {
				cancel()
				return nil, fmt.Errorf("connection refused")
			})

		result, err := isOpenShiftWithDiscovery(ctx, disc)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("waiting for API server"))
		Expect(result).To(BeFalse())
	})

	It("should return false when resource list is empty", func() {
		disc.EXPECT().
			ServerResourcesForGroupVersion(gomock.Any()).
			Return(&metav1.APIResourceList{
				APIResources: []metav1.APIResource{},
			}, nil)

		result, err := isOpenShiftWithDiscovery(context.Background(), disc)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeFalse())
	})
})
