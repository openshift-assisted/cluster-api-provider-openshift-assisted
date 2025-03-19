package release

import (
	"fmt"
	"strings"

	"github.com/blang/semver/v4"
)

func IsOKD(version string) bool {
	distributionVersion, err := semver.ParseTolerant(version)
	if err != nil {
		return false
	}

	for _, pre := range distributionVersion.Pre {
		if strings.HasPrefix(pre.VersionStr, OKDPreStr) {
			return true
		}
	}
	return false
}

func IsOKDGA(version string) bool {
	if !IsOKD(version) {
		return false
	}
	distributionVersion, err := semver.ParseTolerant(version)
	if err != nil {
		return false
	}
	// if it has not exactly 2 parts, it's not GA (i.e. okd-scos.2)
	if len(distributionVersion.Pre) != 2 {
		return false
	}
	return distributionVersion.Pre[0].VersionStr == OKDPreStr && distributionVersion.Pre[1].IsNum
}

func IsGA(version string) bool {
	distributionVersion, err := semver.ParseTolerant(version)
	if err != nil {
		return false
	}
	// if no builds nor pre, it's GA release
	if len(distributionVersion.Build) == 0 && len(distributionVersion.Pre) == 0 {
		return true
	}
	// if not OCP, check OKD
	return IsOKDGA(version)
}

// Get release image based on desired version and potentiall override (can be empty)
func GetReleaseImage(desiredVersion, repositoryOverride string, architecture string) string {
	if repositoryOverride != "" {
		return fmt.Sprintf("%s:%s-%s", repositoryOverride, desiredVersion, architecture)

	}
	repository := OCPRepository
	if IsOKD(desiredVersion) {
		repository = OKDRepository
	}
	return fmt.Sprintf("%s:%s-%s", repository, desiredVersion, architecture)
}
