package assistedinstaller

import (
	"github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1alpha1"
	"github.com/openshift-assisted/cluster-api-agent/util"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/openshift/hive/apis/hive/v1/agent"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetClusterDeploymentFromConfig(
	acp *v1alpha1.OpenshiftAssistedControlPlane,
	clusterName string,
) *hivev1.ClusterDeployment {
	assistedClusterName := clusterName
	if acp.Spec.Config.ClusterName != "" {
		assistedClusterName = acp.Spec.Config.ClusterName
	}
	// Get cluster clusterName instead of reference to ACP clusterName
	clusterDeployment := &hivev1.ClusterDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      acp.Name,
			Namespace: acp.Namespace,
			Labels:    util.ControlPlaneMachineLabelsForCluster(acp, clusterName),
		},
		Spec: hivev1.ClusterDeploymentSpec{
			ClusterName: assistedClusterName,
			BaseDomain:  acp.Spec.Config.BaseDomain,
			Platform: hivev1.Platform{
				AgentBareMetal: &agent.BareMetalPlatform{},
			},
			PullSecretRef: acp.Spec.Config.PullSecretRef,
		},
	}
	return clusterDeployment
}
