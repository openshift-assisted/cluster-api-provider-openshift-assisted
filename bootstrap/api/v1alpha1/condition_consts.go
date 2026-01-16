package v1alpha1

import clusterv1beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1"

const (
	InfraEnvFailedReason                                               = "InfraEnvFailed"
	CreatingSecretFailedReason                                         = "CreatingSecretFailed"
	WaitingForLiveISOURLReason                                         = "WaitingForLiveISOURL"
	WaitingForInstallCompleteReason                                    = "WaitingForInstallComplete"
	WaitingForAssistedInstallerReason                                  = "WaitingForAssistedInstaller"
	WaitingForClusterInfrastructureReason                              = "WaitingForClusterInfrastructure"
	DataSecretAvailableCondition          clusterv1beta1.ConditionType = "DataSecretAvailable"
	PullSecretAvailableCondition          clusterv1beta1.ConditionType = "PullSecretAvailable"
	OpenshiftAssistedConfigLabel                                       = "bootstrap.cluster.x-k8s.io/openshiftAssistedConfig"
	InfraEnvNotReadyReason                                             = "WaitingForInfraEnvToBeReady"
)
