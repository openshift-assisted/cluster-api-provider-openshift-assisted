package kubevirt_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/kubevirt"
)

var _ = Describe("Networking helpers", func() {
	Describe("IsPodNetworking", func() {
		It("should return true when both VIP lists are empty", func() {
			Expect(kubevirt.IsPodNetworking(nil, nil)).To(BeTrue())
		})

		It("should return true when API VIPs are empty", func() {
			Expect(kubevirt.IsPodNetworking(nil, []string{"10.0.0.2"})).To(BeTrue())
		})

		It("should return true when Ingress VIPs are empty", func() {
			Expect(kubevirt.IsPodNetworking([]string{"10.0.0.1"}, nil)).To(BeTrue())
		})

		It("should return false when both VIP lists are populated", func() {
			Expect(kubevirt.IsPodNetworking([]string{"10.0.0.1"}, []string{"10.0.0.2"})).To(BeFalse())
		})
	})

	Describe("IsBridgeNetworking", func() {
		It("should return true when both VIP lists are populated", func() {
			Expect(kubevirt.IsBridgeNetworking([]string{"10.0.0.1"}, []string{"10.0.0.2"})).To(BeTrue())
		})

		It("should return false when API VIPs are empty", func() {
			Expect(kubevirt.IsBridgeNetworking(nil, []string{"10.0.0.2"})).To(BeFalse())
		})

		It("should return false when Ingress VIPs are empty", func() {
			Expect(kubevirt.IsBridgeNetworking([]string{"10.0.0.1"}, nil)).To(BeFalse())
		})

		It("should return false when both VIP lists are empty", func() {
			Expect(kubevirt.IsBridgeNetworking(nil, nil)).To(BeFalse())
		})
	})
})
