package v1alpha2

import clusterv1beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1"

const (
	// ControlPlaneAvailableCondition documents that the OpenshiftAssistedControlPlane is available.
	ControlPlaneAvailableCondition clusterv1beta1.ConditionType = "Available"

	// KubeconfigAvailableCondition documents that the kubeconfig for the workload cluster is available.
	KubeconfigAvailableCondition clusterv1beta1.ConditionType = "KubeconfigAvailable"

	// UpgradeCompletedCondition documents whether an upgrade ran successfully
	UpgradeCompletedCondition clusterv1beta1.ConditionType = "UpgradeCompleted"

	// UpgradeAvailableCondition documents whether an upgrade is available
	UpgradeAvailableCondition clusterv1beta1.ConditionType = "UpgradeAvailable"

	// MachinesCreatedCondition documents that the machines controlled by the OpenshiftAssistedControlPlane are created.
	// When this condition is false, it indicates that there was an error when cloning the infrastructure/bootstrap template or
	// when generating the machine object.
	MachinesCreatedCondition clusterv1beta1.ConditionType = "MachinesCreated"

	// KubernetesVersionAvailableCondition documents that the Kubernetes version could be extracted from the OpenShift version.
	KubernetesVersionAvailableCondition clusterv1beta1.ConditionType = "KubernetesVersionAvailableCondition"

	// ControlPlaneInstallingReason (Severity=Info) documents that the OpenshiftAssistedControlPlane is installing.
	ControlPlaneInstallingReason = "ControlPlaneInstalling"

	// KubernetesVersionUnavailableFailedReason (Severity=Warning) documents that the Kubernetes version could not be extracted
	// from the OpenShift version.
	KubernetesVersionUnavailableFailedReason = "KubernetesVersionUnavailable"

	// KubeconfigUnavailableFailedReason (Severity=Info) documents that the workload cluster kubeconfig is not yet available.
	KubeconfigUnavailableFailedReason = "KubeconfigUnavailable"

	// UpgradeInProgressReason (Severity=Info) documents that an upgrade is in progress.
	UpgradeInProgressReason = "UpgradeInProgress"

	// UpgradeFailedReason (Severity=Error) documents that an upgrade has failed.
	UpgradeFailedReason = "UpgradeFailed"

	// UpgradeImageUnavailableReason (Severity=Error) documents whether an upgrade image is available
	UpgradeImageUnavailableReason = "UpgradeImageUnavailable"

	// InfrastructureTemplateCloningFailedReason (Severity=Error) documents an OpenshiftAssistedControlPlane failing to
	// clone the infrastructure template.
	InfrastructureTemplateCloningFailedReason = "InfrastructureTemplateCloningFailed"

	// BootstrapTemplateCloningFailedReason (Severity=Error) documents an OpenshiftAssistedControlPlane failing to
	// clone the bootstrap template.
	BootstrapTemplateCloningFailedReason = "BootstrapTemplateCloningFailed"

	// MachineGenerationFailedReason (Severity=Error) documents an OpenshiftAssistedControlPlane failing to
	// generate a machine object.
	MachineGenerationFailedReason = "MachineGenerationFailed"
)
