package version

import (
	"context"
	"fmt"
	"slices"
	"strings"

	controlplanev1alpha2 "github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1alpha2"
	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/release"
	"github.com/openshift-assisted/cluster-api-agent/pkg/containers"
	v1 "github.com/openshift/api/config/v1"
	imageapi "github.com/openshift/api/image/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

//go:generate mockgen -destination=mock_version.go -package=version -source openshift.go KubernetesVersionDetector
//go:generate mockgen -destination=mock_version.go -package=version -source openshift.go KubernetesVersionDetector
type KubernetesVersionDetector interface {
	GetKubernetesVersion(imageRef, pullsecret string) (*string, error)
}

type OpenShiftKubernetesVersionDetectorType struct {
	filepath                string
	annotationBuildVersions string
	remoteImageRepository   containers.RemoteImage
}

func NewKubernetesVersionDetector(remoteImageRepo containers.RemoteImage) KubernetesVersionDetector {
	const filepath = "/release-manifests/image-references"
	const annotationBuildVersions = "io.openshift.build.versions"

	return &OpenShiftKubernetesVersionDetectorType{
		filepath:                filepath,
		annotationBuildVersions: annotationBuildVersions,
		remoteImageRepository:   remoteImageRepo,
	}
}

func (o *OpenShiftKubernetesVersionDetectorType) GetKubernetesVersion(imageRef, pullsecret string) (*string, error) {

	auth, err := containers.PullSecretKeyChainFromString(pullsecret)
	if err != nil {
		return nil, fmt.Errorf("unable to load auth from pull-secret: %v", err)
	}

	image, err := o.remoteImageRepository.GetImage(imageRef, auth)
	if err != nil {
		return nil, fmt.Errorf("unable to get image: %v", err)
	}
	extractor, err := containers.NewImageInspector(image)
	if err != nil {
		return nil, fmt.Errorf("unable to create extractor: %v", err)
	}
	fileContent, err := extractor.ExtractFileFromImage(o.filepath)
	if err != nil {
		return nil, fmt.Errorf("unable to extract file %s from imageRef %s : %v", o.filepath, imageRef, err)
	}

	is := &imageapi.ImageStream{}
	if err := yaml.Unmarshal(fileContent, &is); err != nil {
		return nil, fmt.Errorf("unable to load release image-references: %v", err)
	}
	k8sVersion, err := o.getK8sVersionFromImageStream(*is)
	if err != nil {
		return nil, fmt.Errorf("unable to extract k8s from release image-references: %v", err)
	}
	return &k8sVersion, nil
}

func (o *OpenShiftKubernetesVersionDetectorType) getK8sVersionFromImageStream(is imageapi.ImageStream) (string, error) {

	for _, tag := range is.Spec.Tags {
		versions, ok := tag.Annotations[o.annotationBuildVersions]
		if !ok {
			continue
		}
		parts := strings.Split(versions, "=")
		if len(parts) != 2 {
			continue
		}
		if parts[0] == "kubernetes" {
			return parts[1], nil
		}
	}
	return "", fmt.Errorf("unable to find kubernetes version")
}

func UpdateClusterVersionDesiredUpdate(c client.Client, ctx context.Context, oacp *controlplanev1alpha2.OpenshiftAssistedControlPlane, clusterVersion *v1.ClusterVersion, releaseImageWithDigest string) error {

	if isGARelease(oacp) {
		if clusterVersion.Spec.DesiredUpdate == nil || clusterVersion.Spec.DesiredUpdate.Version != oacp.Spec.DistributionVersion {
			clusterVersion.Spec.DesiredUpdate = &v1.Update{
				Version: oacp.Spec.DistributionVersion,
			}
			return doUpdate(c, ctx, clusterVersion)
		}
		return nil
	}
	if clusterVersion.Spec.DesiredUpdate == nil || clusterVersion.Spec.DesiredUpdate.Image != releaseImageWithDigest {
		clusterVersion.Spec.DesiredUpdate = &v1.Update{Image: releaseImageWithDigest, Force: true}
		return doUpdate(c, ctx, clusterVersion)
	}
	return nil
}

func doUpdate(c client.Client, ctx context.Context, clusterVersion *v1.ClusterVersion) error {
	if err := c.Update(ctx, clusterVersion); err != nil {
		return err
	}
	return nil
}

// checks whether the current proposed version is a GA release
func isGARelease(oacp *controlplanev1alpha2.OpenshiftAssistedControlPlane) bool {
	if releaseImageOverride, ok := oacp.Annotations[release.ReleaseImageRepositoryOverrideAnnotation]; ok {
		// if override is not the default repos
		if !slices.Contains([]string{release.OCPRepository, release.OKDRepository}, releaseImageOverride) {
			return false
		}
	}
	return release.IsGA(oacp.Spec.DistributionVersion)
}
