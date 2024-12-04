package version

import (
	"fmt"
	"strings"

	"github.com/blang/semver/v4"
)

// Returns Major.Minor k8s version from an OpenShift release image
// i.e. quay.io/openshift-release-dev/ocp-release:4.17.0-rc.2-x86_64 will return 1.30
// z-stream will be ignored, as it's not predictable
func GetK8sVersionFromOpenShiftImage(image string) (string, error) {

	// quay.io/openshift-release-dev/ocp-release:4.17.0-rc.2-x86_64
	releaseImageParts := strings.Split(image, ":")
	if len(releaseImageParts) < 2 {
		return "", fmt.Errorf("unexpected image format `%s`", image)
	}
	releaseImageTag := releaseImageParts[len(releaseImageParts)-1]

	openShiftVersionParts := strings.Split(releaseImageTag, "-")
	if len(openShiftVersionParts) < 2 {
		return "", fmt.Errorf("unexpected image tag format `%s`", releaseImageTag)
	}
	// discard architecture part
	v, err := semver.ParseTolerant(openShiftVersionParts[0])
	if err != nil {
		return "", err
	}
	v.Minor = v.Minor + 13
	v.Major = v.Major - 3
	return v.String(), nil
}
