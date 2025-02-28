package release_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/release"
)

var _ = Describe("Version Validation Functions", func() {
	Describe("IsOKD", func() {
		DescribeTable("is OKD",
			func(version string, expected bool) {
				Expect(release.IsOKD(version)).To(Equal(expected))
			},
			Entry("valid OKD GA version", "4.18.0-okd-scos.2", true),
			Entry("valid OKD non-GA version", "4.17.0-okd-scos.ec.4", true),
			Entry("nightly OKD version", "4.16.0-0.okd-scos-2024-09-24-151747", true),
			Entry("non-OKD version", "4.12.0", false),
			Entry("non-GA OCP version", "4.19.0-ec.2", false),
			Entry("nightly OCP version", "4.18.0-0.nightly-2025-02-28-213046", false),
			Entry("nightly OCP version", "4.17.0-0.ci-2025-03-03-134232", false),
		)
	})
	Describe("IsOKDGA", func() {
		DescribeTable("version validation",
			func(version string, expected bool) {
				Expect(release.IsOKDGA(version)).To(Equal(expected))
			},
			Entry("valid OKD GA version", "4.18.0-okd-scos.2", true),
			Entry("valid OKD non-GA version", "4.17.0-okd-scos.ec.4", false),
			Entry("nightly OKD version", "4.16.0-0.okd-scos-2024-09-24-151747", false),
			Entry("non-OKD version", "4.12.0", false),
			Entry("non-GA OCP version", "4.19.0-ec.2", false),
			Entry("nightly OCP version", "4.18.0-0.nightly-2025-02-28-213046", false),
			Entry("nightly OCP version", "4.17.0-0.ci-2025-03-03-134232", false),
		)
	})

	Describe("IsGA", func() {
		DescribeTable("version validation",
			func(version string, expected bool) {
				Expect(release.IsGA(version)).To(Equal(expected))
			},
			Entry("valid OKD GA version", "4.18.0-okd-scos.2", true),
			Entry("valid OKD non-GA version", "4.17.0-okd-scos.ec.4", false),
			Entry("nightly OKD version", "4.16.0-0.okd-scos-2024-09-24-151747", false),
			Entry("non-OKD version", "4.12.0", true),
			Entry("non-GA OCP version", "4.19.0-ec.2", false),
			Entry("nightly OCP version", "4.18.0-0.nightly-2025-02-28-213046", false),
			Entry("nightly OCP version", "4.17.0-0.ci-2025-03-03-134232", false),
		)

		Context("when handling edge cases", func() {
			It("should handle non-versions", func() {
				Expect(release.IsGA("notavalidversion")).To(BeFalse())
			})
			It("should handle versions with multiple pre-release segments", func() {
				Expect(release.IsGA("4.12.0-alpha.1.beta")).To(BeFalse())
			})

			It("should handle versions with multiple build metadata segments", func() {
				Expect(release.IsGA("4.12.0+build.123.meta")).To(BeFalse())
			})

			It("should handle versions with both pre-release and build metadata", func() {
				Expect(release.IsGA("4.12.0-rc.1+build.123")).To(BeFalse())
			})
		})
	})
})
