package release_test

import (
	"errors"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/release"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/pkg/containers"
)

const (
	testOCPReleaseImage = "quay.io/openshift-release-dev/ocp-release:4.16.0-x86_64"
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

	Describe("GetRepoImage", func() {
		DescribeTable("extracts repository from image reference",
			func(image string, expectedRepo string, shouldError bool) {
				repo, err := release.GetRepoImage(image)
				if shouldError {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).NotTo(HaveOccurred())
					Expect(repo).To(Equal(expectedRepo))
				}
			},
			Entry("OCP release image with tag",
				testOCPReleaseImage,
				"quay.io/openshift-release-dev/ocp-release",
				false),
			Entry("OKD release image with tag",
				"quay.io/okd/scos-release:4.18.0-okd-scos.2",
				"quay.io/okd/scos-release",
				false),
			Entry("image with digest",
				"quay.io/openshift-release-dev/ocp-release@sha256:abc123",
				"quay.io/openshift-release-dev/ocp-release@sha256",
				false),
			Entry("image with multiple colons",
				"registry.example.com:5000/repo/image:tag",
				"registry.example.com:5000/repo/image",
				false),
			Entry("invalid image without colon",
				"invalidimage",
				"",
				true),
			Entry("empty image string",
				"",
				"",
				true),
		)
	})

	Describe("GetReleaseImageWithDigest", func() {
		var (
			mockCtrl        *gomock.Controller
			mockRemoteImage *containers.MockRemoteImage
			testPullSecret  []byte
		)

		BeforeEach(func() {
			mockCtrl = gomock.NewController(GinkgoT())
			mockRemoteImage = containers.NewMockRemoteImage(mockCtrl)
			testPullSecret = []byte(`{"auths":{"quay.io":{"auth":"dGVzdDp0ZXN0"}}}`)
		})

		AfterEach(func() {
			mockCtrl.Finish()
		})

		It("should resolve digest for tag-based image", func() {
			image := testOCPReleaseImage
			expectedDigest := "sha256:abc123def456"

			mockRemoteImage.EXPECT().
				GetDigest(image, gomock.Any()).
				Return(expectedDigest, nil)

			result, err := release.GetReleaseImageWithDigest(image, testPullSecret, mockRemoteImage)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("quay.io/openshift-release-dev/ocp-release@" + expectedDigest))
		})

		It("should return error when digest resolution fails", func() {
			image := testOCPReleaseImage
			expectedError := errors.New("registry unavailable")

			mockRemoteImage.EXPECT().
				GetDigest(image, gomock.Any()).
				Return("", expectedError)

			_, err := release.GetReleaseImageWithDigest(image, testPullSecret, mockRemoteImage)
			Expect(err).To(HaveOccurred())
			Expect(err).To(Equal(expectedError))
		})

		It("should return error when pull secret is invalid", func() {
			image := testOCPReleaseImage
			invalidPullSecret := []byte(`invalid json`)

			_, err := release.GetReleaseImageWithDigest(image, invalidPullSecret, mockRemoteImage)
			Expect(err).To(HaveOccurred())
		})

		It("should return digest-based image as-is without remote call", func() {
			digestImage := "quay.io/openshift-release-dev/ocp-release@sha256:abc123def456"

			// Mock should NOT be called since image already has digest
			mockRemoteImage.EXPECT().
				GetDigest(gomock.Any(), gomock.Any()).
				Times(0)

			result, err := release.GetReleaseImageWithDigest(digestImage, testPullSecret, mockRemoteImage)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(digestImage))
		})
	})
})
