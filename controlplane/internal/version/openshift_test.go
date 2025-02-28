package version_test

import (
	"context"
	"fmt"

	"github.com/golang/mock/gomock"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/release"
	"github.com/openshift-assisted/cluster-api-agent/test/utils"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	//	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/release"
	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/version"
	"github.com/openshift-assisted/cluster-api-agent/external_mocks"
	"github.com/openshift-assisted/cluster-api-agent/pkg/containers"

	"io"

	"github.com/openshift-assisted/cluster-api-agent/util/test"
	configv1 "github.com/openshift/api/config/v1"
	imageapi "github.com/openshift/api/image/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/yaml"
)

var _ = Describe("GetKubernetesVersion", func() {
	var (
		ctrl            *gomock.Controller
		mockImage       *external_mocks.MockImage
		detector        version.KubernetesVersionDetector
		mockRemoteImage *containers.MockRemoteImage
		pullsecret      = `
{
  "auths": {
    "cloud.openshift.com": {"auth":"Zm9vOmJhcgo="}
  }
}`
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockImage = external_mocks.NewMockImage(ctrl)
		mockRemoteImage = containers.NewMockRemoteImage(ctrl)
		detector = version.NewKubernetesVersionDetector(mockRemoteImage)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("should return the Kubernetes version from the image", func() {
		tarFile, err := createTarWithReleaseManifest("1.21.0")
		Expect(err).ToNot(HaveOccurred())

		mockLayer := external_mocks.NewMockLayer(ctrl)
		mockLayer.EXPECT().Uncompressed().Return(tarFile, nil)
		mockImage.EXPECT().Layers().Return([]v1.Layer{mockLayer}, nil)

		mockRemoteImage.EXPECT().GetImage("dummyImageRef", gomock.Any()).Return(mockImage, nil)

		version, err := detector.GetKubernetesVersion("dummyImageRef", pullsecret)
		Expect(err).ToNot(HaveOccurred())
		Expect(*version).To(Equal("1.21.0"))
	})

	It("should return an error if the Kubernetes version is not found", func() {
		imageStream := imageapi.ImageStream{
			Spec: imageapi.ImageStreamSpec{
				Tags: []imageapi.TagReference{},
			},
		}
		tarFile, err := createTarWithImageStream(imageStream)
		Expect(err).ToNot(HaveOccurred())

		mockLayer := external_mocks.NewMockLayer(ctrl)
		mockLayer.EXPECT().Uncompressed().Return(tarFile, nil)
		mockImage.EXPECT().Layers().Return([]v1.Layer{mockLayer}, nil)

		mockRemoteImage.EXPECT().GetImage("dummyImageRef", gomock.Any()).Return(mockImage, nil)

		_, err = detector.GetKubernetesVersion("dummyImageRef", pullsecret)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError("unable to extract k8s from release image-references: unable to find kubernetes version"))
	})
})

var _ = Describe("UpdateClusterVersionDesiredUpdate", func() {
	const (
		releaseImageWithDigest = "myrepo.com/mynamespace/release@sha256:573a86d57acab6dfb90799f568421e80a41f85aaef1e94a16e13af13339524c1"
	)
	var (
		namespace string = "test-namespace"
		k8sClient client.Client
		ctx       context.Context
	)
	BeforeEach(func() {
		ctx = context.TODO()
		k8sClient = fakeclient.NewClientBuilder().
			WithScheme(testScheme).Build()
	})

	Context("when updating cluster version with no annotation and different version", func() {
		It("should update version successfully", func() {
			update := &configv1.Update{
				Version: "4.17.0",
			}
			clusterVersion, err := createClusterVersion(k8sClient, ctx, update)
			Expect(err).NotTo(HaveOccurred())

			oacp := utils.NewOpenshiftAssistedControlPlane(namespace, "oacp")

			Expect(version.UpdateClusterVersionDesiredUpdate(
				k8sClient,
				ctx,
				oacp,
				clusterVersion,
				releaseImageWithDigest,
			)).To(Succeed())

			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(clusterVersion), clusterVersion)).To(Succeed())

			Expect(clusterVersion.Spec.DesiredUpdate).NotTo(BeNil())
			Expect(clusterVersion.Spec.DesiredUpdate.Version).To(Equal("4.18.0"))
			Expect(clusterVersion.Spec.DesiredUpdate.Image).To(BeEmpty())
		})
	})

	Context("when updating cluster version with no annotation and the same version", func() {
		It("should not update clusterversion", func() {
			update := &configv1.Update{
				Version: "4.18.0",
			}
			clusterVersion, err := createClusterVersion(k8sClient, ctx, update)
			Expect(err).NotTo(HaveOccurred())

			oacp := utils.NewOpenshiftAssistedControlPlane(namespace, "oacp")
			Expect(version.UpdateClusterVersionDesiredUpdate(
				k8sClient,
				ctx,
				oacp,
				clusterVersion,
				releaseImageWithDigest,
			)).To(Succeed())

			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(clusterVersion), clusterVersion)).To(Succeed())

			Expect(clusterVersion.Spec.DesiredUpdate).NotTo(BeNil())
			Expect(clusterVersion.Spec.DesiredUpdate.Version).To(Equal("4.18.0"))
			Expect(clusterVersion.Spec.DesiredUpdate.Image).To(BeEmpty())
		})
	})
	Context("when updating cluster version with no annotation and no desiredUpdate", func() {
		It("should update clusterversion with the right version", func() {
			clusterVersion, err := createClusterVersion(k8sClient, ctx, nil)
			Expect(err).NotTo(HaveOccurred())

			oacp := utils.NewOpenshiftAssistedControlPlane(namespace, "oacp")
			Expect(version.UpdateClusterVersionDesiredUpdate(
				k8sClient,
				ctx,
				oacp,
				clusterVersion,
				releaseImageWithDigest,
			)).To(Succeed())

			Expect(clusterVersion.Spec.DesiredUpdate).NotTo(BeNil())
			Expect(clusterVersion.Spec.DesiredUpdate.Version).To(Equal("4.18.0"))
			Expect(clusterVersion.Spec.DesiredUpdate.Image).To(Equal(""))
		})
	})
	Context("when updating cluster version with annotation with GA repository", func() {
		It("should update clusterversion with the right version", func() {

			clusterVersion, err := createClusterVersion(k8sClient, ctx, nil)
			Expect(err).NotTo(HaveOccurred())

			oacp := utils.NewOpenshiftAssistedControlPlane(namespace, "oacp")
			oacp.Annotations = map[string]string{
				release.ReleaseImageRepositoryOverrideAnnotation: release.OCPRepository,
			}
			Expect(version.UpdateClusterVersionDesiredUpdate(
				k8sClient,
				ctx,
				oacp,
				clusterVersion,
				releaseImageWithDigest,
			)).To(Succeed())

			Expect(clusterVersion.Spec.DesiredUpdate).NotTo(BeNil())
			Expect(clusterVersion.Spec.DesiredUpdate.Version).To(Equal("4.18.0"))
			Expect(clusterVersion.Spec.DesiredUpdate.Image).To(Equal(""))
		})
	})
	Context("when updating cluster version with non-GA repo annotation", func() {
		It("should update clusterversion with the right image and no version", func() {

			clusterVersion, err := createClusterVersion(k8sClient, ctx, nil)
			Expect(err).NotTo(HaveOccurred())

			oacp := utils.NewOpenshiftAssistedControlPlane(namespace, "oacp")
			oacp.Annotations = map[string]string{
				release.ReleaseImageRepositoryOverrideAnnotation: "quay.io/mynamespace/myrepo",
			}
			Expect(version.UpdateClusterVersionDesiredUpdate(
				k8sClient,
				ctx,
				oacp,
				clusterVersion,
				releaseImageWithDigest,
			)).To(Succeed())

			Expect(clusterVersion.Spec.DesiredUpdate).NotTo(BeNil())
			Expect(clusterVersion.Spec.DesiredUpdate.Version).To(Equal(""))
			Expect(clusterVersion.Spec.DesiredUpdate.Image).To(Equal(releaseImageWithDigest))
			Expect(clusterVersion.Spec.DesiredUpdate.Force).To(BeTrue())
		})
	})
	Context("when updating cluster version with no annotation but non GA version", func() {
		It("should update clusterversion with the right image and no version", func() {

			clusterVersion, err := createClusterVersion(k8sClient, ctx, nil)
			Expect(err).NotTo(HaveOccurred())

			oacp := utils.NewOpenshiftAssistedControlPlane(namespace, "oacp")
			oacp.Spec.DistributionVersion = "4.19.0-ec.2"
			Expect(version.UpdateClusterVersionDesiredUpdate(
				k8sClient,
				ctx,
				oacp,
				clusterVersion,
				releaseImageWithDigest,
			)).To(Succeed())

			Expect(clusterVersion.Spec.DesiredUpdate).NotTo(BeNil())
			Expect(clusterVersion.Spec.DesiredUpdate.Version).To(Equal(""))
			Expect(clusterVersion.Spec.DesiredUpdate.Image).To(Equal(releaseImageWithDigest))
			Expect(clusterVersion.Spec.DesiredUpdate.Force).To(BeTrue())
		})
	})
	Context("when updating cluster version with no annotation and OKD GA version", func() {
		It("should update clusterversion with the right version", func() {

			clusterVersion, err := createClusterVersion(k8sClient, ctx, nil)
			Expect(err).NotTo(HaveOccurred())

			oacp := utils.NewOpenshiftAssistedControlPlane(namespace, "oacp")
			oacp.Spec.DistributionVersion = "4.18.0-okd-scos.2"
			Expect(version.UpdateClusterVersionDesiredUpdate(
				k8sClient,
				ctx,
				oacp,
				clusterVersion,
				releaseImageWithDigest,
			)).To(Succeed())

			Expect(clusterVersion.Spec.DesiredUpdate).NotTo(BeNil())
			Expect(clusterVersion.Spec.DesiredUpdate.Version).To(Equal("4.18.0-okd-scos.2"))
			Expect(clusterVersion.Spec.DesiredUpdate.Image).To(Equal(""))
			Expect(clusterVersion.Spec.DesiredUpdate.Force).To(BeFalse())
		})
	})
	Context("when updating cluster version with no annotation and OKD non-GA version", func() {
		It("should update clusterversion with the right version", func() {

			clusterVersion, err := createClusterVersion(k8sClient, ctx, nil)
			Expect(err).NotTo(HaveOccurred())

			oacp := utils.NewOpenshiftAssistedControlPlane(namespace, "oacp")
			oacp.Spec.DistributionVersion = "4.18.0-okd-scos.ec.3"
			Expect(version.UpdateClusterVersionDesiredUpdate(
				k8sClient,
				ctx,
				oacp,
				clusterVersion,
				releaseImageWithDigest,
			)).To(Succeed())

			Expect(clusterVersion.Spec.DesiredUpdate).NotTo(BeNil())
			Expect(clusterVersion.Spec.DesiredUpdate.Version).To(Equal(""))
			Expect(clusterVersion.Spec.DesiredUpdate.Image).To(Equal(releaseImageWithDigest))
			Expect(clusterVersion.Spec.DesiredUpdate.Force).To(BeTrue())
		})
	})
})

func createClusterVersion(k8sClient client.Client, ctx context.Context, update *configv1.Update) (*configv1.ClusterVersion, error) {
	cv := &configv1.ClusterVersion{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "config.openshift.io/v1",
			Kind:       "ClusterVersion",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "version",
		},
		Spec: configv1.ClusterVersionSpec{
			DesiredUpdate: update,
		},
	}
	return cv, k8sClient.Create(ctx, cv)
}

func createTarWithReleaseManifest(k8sversion string) (io.ReadCloser, error) {
	imageStream := imageapi.ImageStream{
		Spec: imageapi.ImageStreamSpec{
			Tags: []imageapi.TagReference{
				{
					Annotations: map[string]string{
						"io.openshift.build.versions": fmt.Sprintf("kubernetes=%s", k8sversion),
					},
				},
			},
		},
	}
	return createTarWithImageStream(imageStream)
}

func createTarWithImageStream(imageStream imageapi.ImageStream) (io.ReadCloser, error) {
	fileContent, err := yaml.Marshal(imageStream)
	if err != nil {
		return nil, err
	}
	return test.CreateTarArchive(test.TarArchive{
		Filepath:      "release-manifests/image-references",
		Content:       string(fileContent),
		IsSymlink:     false,
		SymlinkTarget: "",
	}), nil
}
