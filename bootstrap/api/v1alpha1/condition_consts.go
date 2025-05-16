package v1alpha1

import clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"

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
	InfraEnvCooldownReason                                        = "WaitingForInfraEnvToBeReady"
)
