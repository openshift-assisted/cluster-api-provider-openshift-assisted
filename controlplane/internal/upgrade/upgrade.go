package upgrade

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/release"
	"github.com/openshift-assisted/cluster-api-agent/pkg/containers"

	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/workloadclient"
	configv1 "github.com/openshift/api/config/v1"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ClusterVersionName                   = "version"
	ReleaseImageRepositoryOverrideOption = "ReleaseImageRepositoryOverride"
	ReleaseImagePullSecretOption         = "ReleaseImagePullSecret"
)

type ClusterUpgradeOption struct {
	Name  string
	Value string
}

//go:generate mockgen -destination=mock_upgrade.go -package=upgrade -source upgrade.go ClusterUpgradeFactory,ClusterUpgrade
type ClusterUpgradeFactory interface {
	NewUpgrader(kubeConfig []byte) (ClusterUpgrade, error)
}
type ClusterUpgrade interface {
	IsUpgradeInProgress(ctx context.Context) (bool, error)
	GetCurrentVersion(ctx context.Context) (string, error)
	IsDesiredVersionUpdated(ctx context.Context, desiredVersion string) (bool, error)
	UpdateClusterVersionDesiredUpdate(ctx context.Context, desiredVersion string, architecture string, options ...ClusterUpgradeOption) error
}

func NewOpenshiftUpgradeFactory(remoteImage containers.RemoteImage, clientGenerator workloadclient.ClientGenerator) *OpenshiftUpgradeFactory {
	return &OpenshiftUpgradeFactory{
		remoteImage:     remoteImage,
		clientGenerator: clientGenerator,
	}
}

type OpenshiftUpgradeFactory struct {
	remoteImage     containers.RemoteImage
	clientGenerator workloadclient.ClientGenerator
}

func (f *OpenshiftUpgradeFactory) NewUpgrader(kubeConfig []byte) (ClusterUpgrade, error) {
	c, err := f.clientGenerator.GetWorkloadClusterClient(kubeConfig)
	if err != nil {
		return nil, err
	}
	return &OpenshiftUpgrader{
		client:      c,
		remoteImage: f.remoteImage,
	}, nil
}

func NewOpenshiftUpgrader(client client.Client, remoteImage containers.RemoteImage) OpenshiftUpgrader {
	return OpenshiftUpgrader{
		client:      client,
		remoteImage: remoteImage,
	}
}

type OpenshiftUpgrader struct {
	client      client.Client
	remoteImage containers.RemoteImage
}

// Returns true if upgrade in progress, false otherwise. If any error occurs while performing this operation, it will be
// returned
func (u *OpenshiftUpgrader) IsUpgradeInProgress(ctx context.Context) (bool, error) {
	clusterVersion, err := u.getClusterVersion(ctx)
	if err != nil {
		return false, err
	}

	return isUpdateInProgress(clusterVersion), nil
}

func isUpdateInProgress(clusterVersion configv1.ClusterVersion) bool {
	for _, updateHistory := range clusterVersion.Status.History {
		if updateHistory.State == configv1.PartialUpdate {
			return true
		}
	}
	for _, condition := range clusterVersion.Status.Conditions {
		if condition.Type == configv1.OperatorProgressing && condition.Status == configv1.ConditionTrue {
			return true
		}
	}
	return false
}

// Returns the current version string for the client's cluster. If any error occurs while performing this operation, it will be
// returned
func (u *OpenshiftUpgrader) GetCurrentVersion(ctx context.Context) (string, error) {
	clusterVersion, err := u.getClusterVersion(ctx)
	if err != nil {
		return "", err
	}
	for _, history := range clusterVersion.Status.History {
		if history.State == configv1.CompletedUpdate {
			return history.Version, nil
		}
	}
	return "", fmt.Errorf("no completed update found in ClusterVersion history")
}

// Returns true if the desired version matches the cluster's desired version, false otherwise. If any error occurs while
// performing this operation, it will be returned
func (u *OpenshiftUpgrader) IsDesiredVersionUpdated(ctx context.Context, desiredVersion string) (bool, error) {
	clusterVersion, err := u.getClusterVersion(ctx)
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(desiredVersion, clusterVersion.Status.Desired.Version), nil
}

// Updates the cluster's desired version
func (u *OpenshiftUpgrader) UpdateClusterVersionDesiredUpdate(ctx context.Context, desiredVersion string, architecture string, options ...ClusterUpgradeOption) error {
	repositoryOverride := getOption(ReleaseImageRepositoryOverrideOption, options...)
	clusterVersion, err := u.getClusterVersion(ctx)
	if err != nil {
		return err
	}
	if isGARelease(desiredVersion, repositoryOverride) {
		if clusterVersion.Spec.DesiredUpdate == nil || clusterVersion.Spec.DesiredUpdate.Version != desiredVersion {
			clusterVersion.Spec.DesiredUpdate = &configv1.Update{
				Version: desiredVersion,
			}
			return u.client.Update(ctx, &clusterVersion)
		}
		return nil
	}
	pullSecret := getOption(ReleaseImagePullSecretOption, options...)
	releaseImageWithDigest, err := u.getReleaseImageWithDigest(release.GetReleaseImage(desiredVersion, repositoryOverride, architecture), []byte(pullSecret))
	if err != nil {
		return err
	}
	if clusterVersion.Spec.DesiredUpdate == nil || clusterVersion.Spec.DesiredUpdate.Image != releaseImageWithDigest {
		clusterVersion.Spec.DesiredUpdate = &configv1.Update{Image: releaseImageWithDigest, Force: true}
		return u.client.Update(ctx, &clusterVersion)
	}
	return nil
}

func (u *OpenshiftUpgrader) getClusterVersion(ctx context.Context) (configv1.ClusterVersion, error) {
	clusterVersion := configv1.ClusterVersion{}
	if err := u.client.Get(ctx, types.NamespacedName{Name: ClusterVersionName}, &clusterVersion); err != nil {
		return clusterVersion, err
	}
	return clusterVersion, nil
}

func getOption(optionName string, options ...ClusterUpgradeOption) string {
	for _, opt := range options {
		if opt.Name == optionName {
			return opt.Value
		}
	}
	return ""
}

// checks whether the current proposed version is a GA release
func isGARelease(desiredVersion, repositoryOverride string) bool {
	// if override is not the default repos
	if !slices.Contains([]string{release.OCPRepository, release.OKDRepository}, repositoryOverride) {
		return false
	}
	return release.IsGA(desiredVersion)
}

func (u *OpenshiftUpgrader) getReleaseImageWithDigest(image string, pullsecret []byte) (string, error) {
	keychain, err := containers.PullSecretKeyChainFromString(string(pullsecret))
	if err != nil {
		return "", err
	}

	digest, err := u.remoteImage.GetDigest(image, keychain)
	if err != nil {
		return "", err
	}
	repoImage, err := getRepoImage(image)
	if err != nil {
		return "", err
	}
	return repoImage + "@" + digest, nil
}

func getRepoImage(image string) (string, error) {
	parts := strings.Split(image, ":")
	if len(parts) < 1 {
		return "", fmt.Errorf("could not parse image %s", image)
	}
	return parts[0], nil
}
