package upgrade_test

import (
	"context"
	"fmt"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/upgrade"
	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/workloadclient"
	"github.com/openshift-assisted/cluster-api-agent/pkg/containers"
	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const pullsecret string = `
{
  "auths": {
    "cloud.openshift.com": {"auth":"Zm9vOmJhcgo="}
  }
}`

var _ = Describe("OpenShift Upgrader", func() {
	var (
		ctx             context.Context
		mockCtrl        *gomock.Controller
		mockRemoteImage *containers.MockRemoteImage
		clientGenerator *workloadclient.MockClientGenerator
		upgradeFactory  upgrade.ClusterUpgradeFactory
		clusterVersion  configv1.ClusterVersion
		fakeClient      client.Client
	)

	BeforeEach(func() {
		ctx = context.Background()
		mockCtrl = gomock.NewController(GinkgoT())
		mockRemoteImage = containers.NewMockRemoteImage(mockCtrl)
		clientGenerator = workloadclient.NewMockClientGenerator(mockCtrl)

		updateHistory := []configv1.UpdateHistory{
			{
				State:   configv1.CompletedUpdate,
				Version: "4.10.0",
			},
		}
		clusterVersion = getClusterVersion(updateHistory)

		upgradeFactory = upgrade.NewOpenshiftUpgradeFactory(mockRemoteImage, clientGenerator)

		fakeClient = fake.NewClientBuilder().
			WithScheme(testScheme).
			WithObjects(&clusterVersion).
			WithStatusSubresource(&configv1.ClusterVersion{}).
			Build()
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Describe("NewUpgrader", func() {
		It("should create new upgrader successfully", func() {
			kubeConfig := []byte("fake-kubeconfig")
			clientGenerator.EXPECT().GetWorkloadClusterClient(kubeConfig).Return(fakeClient, nil)

			upgrader, err := upgradeFactory.NewUpgrader(kubeConfig)
			Expect(err).NotTo(HaveOccurred())
			Expect(upgrader).NotTo(BeNil())
		})

		It("should return error when client generation fails", func() {
			kubeConfig := []byte("fake-kubeconfig")
			clientGenerator.EXPECT().GetWorkloadClusterClient(kubeConfig).Return(nil, fmt.Errorf("client generation failed"))

			upgrader, err := upgradeFactory.NewUpgrader(kubeConfig)
			Expect(err).To(HaveOccurred())
			Expect(upgrader).To(BeNil())
		})
	})

	Describe("OpenShiftUpgrader", func() {
		var upgrader upgrade.OpenshiftUpgrader

		BeforeEach(func() {
			upgrader = upgrade.NewOpenshiftUpgrader(fakeClient, mockRemoteImage)
		})

		Context("IsUpgradeInProgress", func() {
			It("should return false when no upgrade is in progress", func() {
				inProgress, err := upgrader.IsUpgradeInProgress(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(inProgress).To(BeFalse())
			})

			It("should return true when partial update is present", func() {
				clusterVersion.Status.History[0].State = configv1.PartialUpdate
				err := fakeClient.Status().Update(ctx, &clusterVersion)
				Expect(err).NotTo(HaveOccurred())

				inProgress, err := upgrader.IsUpgradeInProgress(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(inProgress).To(BeTrue())
			})
			It("should return true when progressing condition is true", func() {
				clusterVersion.Status.Conditions = []configv1.ClusterOperatorStatusCondition{
					{
						Type:   configv1.OperatorProgressing,
						Status: configv1.ConditionTrue,
					},
				}
				err := fakeClient.Status().Update(ctx, &clusterVersion)
				Expect(err).NotTo(HaveOccurred())

				inProgress, err := upgrader.IsUpgradeInProgress(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(inProgress).To(BeTrue())
			})
		})

		Context("GetCurrentVersion", func() {
			It("should return current version", func() {
				version, err := upgrader.GetCurrentVersion(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(version).To(Equal("4.10.0"))
			})
		})

		Context("IsDesiredVersionUpdated", func() {
			It("should be updated", func() {
				isUpdated, err := upgrader.IsDesiredVersionUpdated(ctx, "4.10.0")
				Expect(err).NotTo(HaveOccurred())
				Expect(isUpdated).To(BeTrue())
			})
			It("should not be updated", func() {
				isUpdated, err := upgrader.IsDesiredVersionUpdated(ctx, "4.11.0")
				Expect(err).NotTo(HaveOccurred())
				Expect(isUpdated).To(BeFalse())
			})

		})

		Context("UpdateClusterVersionDesiredUpdate", func() {
			It("should update GA version without image", func() {
				err := upgrader.UpdateClusterVersionDesiredUpdate(ctx, "4.11.0",
					"x86_64",
					upgrade.ClusterUpgradeOption{
						Name:  upgrade.ReleaseImageRepositoryOverrideOption,
						Value: "quay.io/openshift-release-dev/ocp-release",
					})
				Expect(err).NotTo(HaveOccurred())

				updatedCV := &configv1.ClusterVersion{}
				err = fakeClient.Get(ctx, client.ObjectKey{Name: upgrade.ClusterVersionName}, updatedCV)
				Expect(err).NotTo(HaveOccurred())
				Expect(updatedCV.Spec.DesiredUpdate.Version).To(Equal("4.11.0"))
			})

			It("should update non-GA version with image", func() {
				mockRemoteImage.EXPECT().GetDigest("quay.io/openshift-release-dev/ocp-release:4.11.0-rc.1-x86_64", gomock.Any()).Return("sha256:123456", nil)

				err := upgrader.UpdateClusterVersionDesiredUpdate(ctx, "4.11.0-rc.1",
					"x86_64",
					upgrade.ClusterUpgradeOption{
						Name:  upgrade.ReleaseImagePullSecretOption,
						Value: pullsecret,
					})
				Expect(err).NotTo(HaveOccurred())

				updatedCV := &configv1.ClusterVersion{}
				err = fakeClient.Get(ctx, client.ObjectKey{Name: upgrade.ClusterVersionName}, updatedCV)
				Expect(err).NotTo(HaveOccurred())
				Expect(updatedCV.Spec.DesiredUpdate.Image).To(ContainSubstring("sha256:123456"))
				Expect(updatedCV.Spec.DesiredUpdate.Force).To(BeTrue())
			})
		})
		Context("GetUpgradeStatus", func() {
			It("should return an upgrade is in progress message", func() {
				msg, err := upgrader.GetUpgradeStatus(ctx)
				Expect(err).To(BeNil())
				Expect(msg).To(Equal("upgrade is in progress"))
			})
		})
	})
})

func getClusterVersion(history []configv1.UpdateHistory) configv1.ClusterVersion {
	return configv1.ClusterVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name: upgrade.ClusterVersionName,
		},
		Status: configv1.ClusterVersionStatus{
			History: history,
			Desired: configv1.Release{
				Version: "4.10.0",
			},
			Conditions: []configv1.ClusterOperatorStatusCondition{
				{
					Message: "upgrade is in progress",
					Status:  configv1.ConditionTrue,
				},
				{
					Message: "upgrade is not in progress",
					Status:  configv1.ConditionFalse,
				},
			},
		},
	}
}
