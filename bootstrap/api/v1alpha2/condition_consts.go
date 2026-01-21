package v1alpha2

import clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

const (
	InfraEnvFailedReason                                          = "InfraEnvFailed"
	CreatingSecretFailedReason                                    = "CreatingSecretFailed"
	WaitingForLiveISOURLReason                                    = "WaitingForLiveISOURL"
	WaitingForInstallCompleteReason                               = "WaitingForInstallComplete"
	WaitingForAssistedInstallerReason                             = "WaitingForAssistedInstaller"
	WaitingForClusterInfrastructureReason                         = "WaitingForClusterInfrastructure"
	DataSecretAvailableCondition          clusterv1.ConditionType = "DataSecretAvailable"
	PullSecretAvailableCondition          clusterv1.ConditionType = "PullSecretAvailable"
	OpenshiftAssistedConfigLabel                                  = "bootstrap.cluster.x-k8s.io/openshiftAssistedConfig"
	InfraEnvNotReadyReason                                        = "WaitingForInfraEnvToBeReady"
)
