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

// IsGA returns true if the provided version string represents a general availability (GA) release.
// A version is considered GA if it has no build or pre-release metadata, or if it qualifies as an OKD GA release.
// The version string is parsed in a tolerant manner; if parsing fails, the function returns false.
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

// GetReleaseImage returns the release image string based on the specified version, an optional repository override, and a list of architectures.
// When a repository override is provided, it uses that override; otherwise, it selects the OCP repository for standard releases or the OKD repository if the version is recognized as OKD.
// The image string is formatted as "repository:version-architecture", where the architecture is determined from the provided list.
func GetReleaseImage(desiredVersion, repositoryOverride string, architectures []string) string {
	arch := getArchitecture(architectures)
	if repositoryOverride != "" {
		return fmt.Sprintf("%s:%s-%s", repositoryOverride, desiredVersion, arch)

	}
	repository := OCPRepository
	if IsOKD(desiredVersion) {
		repository = OKDRepository
	}
	return fmt.Sprintf("%s:%s-%s", repository, desiredVersion, arch)
}

// getArchitecture returns the canonical architecture from a list of architectures.
// It returns "multi" if the list is empty or if the architectures differ.
// If the slice contains exactly one architecture or if all entries are identical, that single architecture is returned.
func getArchitecture(architectures []string) string {
	multiArch := "multi"
	// by default, return multi arch
	if len(architectures) < 1 {
		return multiArch
	}
	// if there is only one architecture, return it
	if len(architectures) == 1 {
		return architectures[0]
	}
	firstArch := architectures[0]
	for _, arch := range architectures {
		if arch != firstArch {
			return multiArch
		}
	}
	// if all architectures are the same
	return firstArch
}
