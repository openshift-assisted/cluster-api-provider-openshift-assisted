package v1alpha2

import clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"

const (
	// ControlPlaneReadyCondition documents that the OpenshiftAssistedControlplane is ready.
	ControlPlaneReadyCondition clusterv1.ConditionType = "ControlPlaneReady"

	// KubeconfigAvailableCondition documents that the kubeconfig for the workload cluster is available.
	KubeconfigAvailableCondition clusterv1.ConditionType = "KubeconfigAvailable"

	// UpgradeCompletedCondition documents wether an upgrade run successfully
	UpgradeCompletedCondition clusterv1.ConditionType = "UpgradeCompleted"

	// UpgradeAvailableCondition documents wether an upgrade is available
	UpgradeAvailableCondition clusterv1.ConditionType = "UpgradeAvailable"

	// MachinesCreatedCondition documents that the machines controlled by the OpenshiftAssistedControlplane are created.
	// When this condition is false, it indicates that there was an error when cloning the infrastructure/bootstrap template or
	// when generating the machine object.
	MachinesCreatedCondition clusterv1.ConditionType = "MachinesCreated"

	// KubernetesVersionAvailableCondition documents that the Kubernetes version could be extracted from the OpenShift version.
	KubernetesVersionAvailableCondition clusterv1.ConditionType = "KubernetesVersionAvailableCondition"

	// ControlPlaneInstallingCOndition (Severity=Info) documents that the OpenshiftAssistedControlplane is installing.
	ControlPlaneInstallingReason = "ControlPlaneInstalling"

	// KubernetesVersionUnavailable (Severity=Warning) documents that the Kubernetes version could not be extracted
	// from the OpenShift version.
	KubernetesVersionUnavailableFailedReason = "KubernetesVersionUnavailable"

	// ControlPlaneInstallingCOndition (Severity=Info) documents that the workload cluster kubeconfig is not yet available.
	KubeconfigUnavailableFailedReason = "KubeconfigUnavailable"

	// UpgradeInProgressReason (Severity=Info) documents that an upgrade is in progress.
	UpgradeInProgressReason = "UpgradeInProgress"

	// UpgradeImageUnavailableReason (Severity=Error) documents whether an upgrade image is available
	UpgradeImageUnavailableReason = "UpgradeImageUnavailable"

	// InfrastructureTemplateCloningFailedReason (Severity=Error) documents a OpenshiftAssistedControlplane failing to
	// clone the infrastructure template.
	InfrastructureTemplateCloningFailedReason = "InfrastructureTemplateCloningFailed"

	// BootstrapTemplateCloningFailedReason (Severity=Error) documents a OpenshiftAssistedControlplane failing to
	// clone the bootstrap template.
	BootstrapTemplateCloningFailedReason = "BootstrapTemplateCloningFailed"

	// MachineGenerationFailedReason (Severity=Error) documents a OpenshiftAssistedControlplane failing to
	// generate a machine object.
	MachineGenerationFailedReason = "MachineGenerationFailed"
)
