package assistedinstaller

import (
	bootstrapv1alpha1 "github.com/openshift-assisted/cluster-api-agent/bootstrap/api/v1alpha1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

func GetInfraEnvFromConfig(
	infraEnvName string,
	config *bootstrapv1alpha1.OpenshiftAssistedConfig,
	clusterDeployment *hivev1.ClusterDeployment,
) *aiv1beta1.InfraEnv {
	infraEnv := &aiv1beta1.InfraEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name:      infraEnvName,
			Namespace: config.Namespace,
			Labels: map[string]string{
				bootstrapv1alpha1.OpenshiftAssistedConfigLabel: config.Name,
			},
		},
	}

	clusterName, ok := config.Labels[clusterv1.ClusterNameLabel]
	if ok {
		infraEnv.Labels[clusterv1.ClusterNameLabel] = clusterName
	}

	//TODO: create logic for placeholder pull secret
	var pullSecret *corev1.LocalObjectReference
	if config.Spec.PullSecretRef != nil {
		pullSecret = config.Spec.PullSecretRef
	}
	infraEnv.Spec = aiv1beta1.InfraEnvSpec{
		AdditionalNTPSources:  config.Spec.AdditionalNTPSources,
		AdditionalTrustBundle: config.Spec.AdditionalTrustBundle,
		ClusterRef: &aiv1beta1.ClusterReference{
			Name:      clusterDeployment.Name,
			Namespace: clusterDeployment.Namespace,
		},
		CpuArchitecture:            config.Spec.CpuArchitecture,
		KernelArguments:            config.Spec.KernelArguments,
		NMStateConfigLabelSelector: config.Spec.NMStateConfigLabelSelector,
		OSImageVersion:             config.Spec.OSImageVersion,
		Proxy:                      config.Spec.Proxy,
		PullSecretRef:              pullSecret,
		SSHAuthorizedKey:           config.Spec.SSHAuthorizedKey,
	}
	return infraEnv
}
