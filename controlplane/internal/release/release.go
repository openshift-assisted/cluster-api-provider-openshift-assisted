package release

import (
	"fmt"
	"strings"

	"github.com/blang/semver/v4"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/pkg/containers"
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

// GetReleaseImage returns the release image based on the desired version, optional repository override, and architecture.
func GetReleaseImage(desiredVersion, repositoryOverride string, architecture string) string {
	if repositoryOverride != "" {
		return fmt.Sprintf("%s:%s-%s", repositoryOverride, desiredVersion, architecture)

	}
	if IsOKD(desiredVersion) {
		return fmt.Sprintf("%s:%s", OKDRepository, desiredVersion)
	}
	return fmt.Sprintf("%s:%s-%s", OCPRepository, desiredVersion, architecture)
}

// GetReleaseImageWithDigest resolves a tag-based release image reference to a digest-based reference.
// It uses the provided pull secret for authentication and the RemoteImage interface for digest resolution.
// Returns the digest-based image reference in the format "repository@sha256:digest" or an error.
// If the image is already digest-based (contains "@sha"), it is returned as-is without a remote call.
func GetReleaseImageWithDigest(image string, pullSecret []byte, remoteImage containers.RemoteImage) (string, error) {
	// If the image is already digest-based, return it as-is
	if strings.Contains(image, "@sha") {
		return image, nil
	}

	keychain, err := containers.PullSecretKeyChainFromString(string(pullSecret))
	if err != nil {
		return "", err
	}

	digest, err := remoteImage.GetDigest(image, keychain)
	if err != nil {
		return "", err
	}

	repoImage, err := GetRepoImage(image)
	if err != nil {
		return "", err
	}

	return repoImage + "@" + digest, nil
}

// GetRepoImage extracts the repository portion from an image reference.
// For example, "quay.io/openshift-release-dev/ocp-release:4.16.0-x86_64" returns "quay.io/openshift-release-dev/ocp-release".
func GetRepoImage(image string) (string, error) {
	// Find the last occurrence of ":" to handle registry ports (e.g., registry.example.com:5000/repo:tag)
	lastColonIndex := strings.LastIndex(image, ":")
	if lastColonIndex == -1 {
		return "", fmt.Errorf("could not parse image %s", image)
	}
	return image[:lastColonIndex], nil
}
