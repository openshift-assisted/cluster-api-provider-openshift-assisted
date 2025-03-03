package upgrade

import (
	"context"
	"errors"
	"fmt"

	controlplanev1alpha2 "github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1alpha2"
	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/workloadclient"
	"github.com/openshift-assisted/cluster-api-agent/util"

	configv1 "github.com/openshift/api/config/v1"

	"github.com/coreos/go-semver/semver"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// UpgradeImageOverrideAnnotation is a (temporary) solution to provide an upgrade image to CVO
	// It is used while the OCP version being used for testing is not GA and should not be necessary if
	// a GA version of OCP is used.
	// Example: GA version = 4.19.0, Non-GA = 4.19.0-0.nightly-2025-01-30-091858
	UpgradeImageOverrideAnnotation = "cluster.x-k8s.io/upgrade-image-override"
)

func IsUpgradeRequested(ctx context.Context, oacp *controlplanev1alpha2.OpenshiftAssistedControlPlane) bool {
	log := ctrl.LoggerFrom(ctx)
	oacpDistVersion, err := semver.NewVersion(oacp.Spec.DistributionVersion)
	if err != nil {
		log.Error(err, "failed to detect OpenShift version from ACP spec", "version", oacp.Spec.DistributionVersion)
		return false
	}

	if oacp.Status.DistributionVersion == "" {
		return false
	}
	currentOACPDistVersion, err := semver.NewVersion(oacp.Status.DistributionVersion)
	if err != nil {
		log.Error(err, "failed to detect OpenShift version from ACP status", "version", oacp.Spec.DistributionVersion)
		return false
	}

	versionComparison := oacpDistVersion.Compare(*currentOACPDistVersion)
	if versionComparison == 0 {
		log.Info("Versions are the same, no upgrade has been requested")
		return false
	}

	if versionComparison > 0 {
		log.Info("Upgrade detected, new requested version is greater than current version",
			"new requested version", oacpDistVersion.String(), "current version", currentOACPDistVersion.String())
		return true
	}
	log.Info("Upgrade request failed: upgrade version requested is less than the current workload cluster version",
		"new requested version", oacpDistVersion.String(), "current version", currentOACPDistVersion.String())
	return false
}

func GetWorkloadClusterVersion(ctx context.Context, client client.Client,
	workloadClusterClientGenerator workloadclient.ClientGenerator,
	oacp *controlplanev1alpha2.OpenshiftAssistedControlPlane) (string, error) {
	workloadClient, err := getWorkloadClient(ctx, client, workloadClusterClientGenerator, oacp)
	if err != nil {
		return "", err
	}

	var clusterVersion configv1.ClusterVersion
	if err := workloadClient.Get(ctx, types.NamespacedName{Name: "version"}, &clusterVersion); err != nil {
		err = errors.Join(err, fmt.Errorf(("failed to get ClusterVersion from workload cluster")))
		return "", err
	}

	for _, history := range clusterVersion.Status.History {
		if history.State == configv1.CompletedUpdate {
			return history.Version, nil
		}
	}

	return clusterVersion.Status.Desired.Version, nil
}

func getWorkloadClient(ctx context.Context, client client.Client,
	workloadClusterClientGenerator workloadclient.ClientGenerator,
	oacp *controlplanev1alpha2.OpenshiftAssistedControlPlane) (client.Client, error) {
	if !isKubeconfigAvailable(oacp) {
		return nil, fmt.Errorf("kubeconfig for workload cluster is not available yet")
	}

	kubeconfigSecret, err := util.GetClusterKubeconfigSecret(ctx, client, oacp.Labels[clusterv1.ClusterNameLabel], oacp.Namespace)
	if err != nil {
		err = errors.Join(err, fmt.Errorf("failed to get cluster kubeconfig secret"))
		return nil, err
	}

	if kubeconfigSecret == nil {
		return nil, fmt.Errorf("kubeconfig secret was not found")
	}

	kubeconfig, err := util.ExtractKubeconfigFromSecret(kubeconfigSecret, "value")
	if err != nil {
		err = errors.Join(err, fmt.Errorf("failed to extract kubeconfig from secret %s", kubeconfigSecret.Name))
		return nil, err
	}

	workloadClient, err := workloadClusterClientGenerator.GetWorkloadClusterClient(kubeconfig)
	if err != nil {
		err = errors.Join(err, fmt.Errorf("failed to establish client for workload cluster from kubeconfig"))
		return nil, err
	}
	return workloadClient, nil
}

// isKubeconfigAvailable returns true if the openshift assisted control plane
// condition KubeconfigAvailable is true
func isKubeconfigAvailable(oacp *controlplanev1alpha2.OpenshiftAssistedControlPlane) bool {
	kubeconfigFoundCondition := util.FindStatusCondition(oacp.Status.Conditions, controlplanev1alpha2.KubeconfigAvailableCondition)
	if kubeconfigFoundCondition == nil {
		return false
	}
	return kubeconfigFoundCondition.Status == corev1.ConditionTrue
}

func IsUpgradeCompleted(ctx context.Context, hubClient client.Client,
	workloadClusterClientGenerator workloadclient.ClientGenerator,
	oacp *controlplanev1alpha2.OpenshiftAssistedControlPlane) bool {
	log := log.FromContext(ctx)
	workloadClient, err := getWorkloadClient(ctx, hubClient, workloadClusterClientGenerator, oacp)
	if err != nil {
		return false
	}

	if workloadClient == nil {
		return false
	}

	var clusterVersion configv1.ClusterVersion
	if err := workloadClient.Get(ctx, types.NamespacedName{Name: "version"}, &clusterVersion); err != nil {
		//err = errors.Join(err, fmt.Errorf(("failed to get ClusterVersion from workload cluster")))
		return false
	}
	for _, history := range clusterVersion.Status.History {
		if history.Version == oacp.Spec.DistributionVersion {
			log.Info("Found history version that matches distribution version", "history", history.Version, "distributionVersion", oacp.Spec.DistributionVersion)
			if history.State == configv1.CompletedUpdate {
				log.Info("upgrade is complete!")
				return true
			}
		}
	}
	log.Info("upgrade not complete")
	return false
}

func CheckNodes(ctx context.Context, hubClient client.Client, workloadClusterClientGenerator workloadclient.ClientGenerator, oacp *controlplanev1alpha2.OpenshiftAssistedControlPlane) error {
	log := log.FromContext(ctx)
	workloadClient, err := getWorkloadClient(ctx, hubClient, workloadClusterClientGenerator, oacp)
	if err != nil {
		return err
	}

	if workloadClient == nil {
		return fmt.Errorf("workload client is not available yet")
	}
	nodes := &corev1.NodeList{}
	if err := workloadClient.List(ctx, nodes, client.MatchingLabels{"node-role.kubernetes.io/control-plane": ""}); err != nil {
		return err
	}
	readyNodes := 0
	for _, node := range nodes.Items {
		log.Info("node", "name", node.Name, "os image", node.Status.NodeInfo.OSImage)
		// It's difficult to determine which OCP version is running on this Node, so we will
		// just ensure that the Node is ready
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
				readyNodes++
				break
			}
		}
	}
	log.Info("Finished checking workload cluster nodes", "ready control plane nodes", readyNodes, "total control plane nodes found", len(nodes.Items))
	oacp.Status.ReadyReplicas = int32(readyNodes)
	oacp.Status.UpdatedReplicas = int32(readyNodes)
	return nil
}

func UpgradeWorkloadCluster(ctx context.Context, client client.Client,
	workloadClusterClientGenerator workloadclient.ClientGenerator,
	oacp *controlplanev1alpha2.OpenshiftAssistedControlPlane) error {
	workloadClient, err := getWorkloadClient(ctx, client, workloadClusterClientGenerator, oacp)
	if err != nil {
		return err
	}

	if workloadClient == nil {
		return fmt.Errorf("workload client is not available yet")
	}

	var clusterVersion configv1.ClusterVersion
	if err := workloadClient.Get(ctx, types.NamespacedName{Name: "version"}, &clusterVersion); err != nil {
		err = errors.Join(err, fmt.Errorf(("failed to get ClusterVersion from workload cluster")))
		return err
	}

	clusterVersion.Spec.DesiredUpdate = &configv1.Update{
		Version: oacp.Spec.DistributionVersion,
	}

	if releaseImage, ok := oacp.Annotations[UpgradeImageOverrideAnnotation]; ok {
		clusterVersion.Spec.DesiredUpdate.Image = releaseImage
		clusterVersion.Spec.DesiredUpdate.Force = true
	}

	return workloadClient.Update(ctx, &clusterVersion)
}
