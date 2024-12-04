package version_test

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-assisted/cluster-api-agent/util/version"

	"testing"
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Version Suite")
}

var _ = DescribeTable("GetK8sVersionFromImage Test",
	func(image, expected string, expectedErr error) {
		v, err := version.GetK8sVersionFromOpenShiftImage(image)
		if expectedErr != nil {
			Expect(err).To(MatchError(expectedErr))
		}
		if expectedErr == nil {
			Expect(err).To(BeNil())
		}
		Expect(v).To(Equal(expected))
	},
	Entry("empty image name", "", "", errors.New("unexpected image format ``")),
	Entry("ocp rc 4.17 image", "quay.io/openshift-release-dev/ocp-release:4.17.0-rc.2-x86_64", "1.30.0", nil),
	Entry("ocp 4.12.70 image", "quay.io/openshift-release-dev/ocp-release:4.12.70-x86_64 ", "1.25.70", nil),
	Entry("ocp 4.15.39 image", "quay.io/openshift-release-dev/ocp-release:4.15.39-multi ", "1.28.39", nil),
	Entry("malformed image", "4.12.70-x86_64", "", errors.New("unexpected image format `4.12.70-x86_64`")),
	Entry("malformed image",
		"quay.io/openshift-release-dev/ocp-release:4.12.70+noarch",
		"",
		errors.New("unexpected image tag format `4.12.70+noarch`"),
	),
)
