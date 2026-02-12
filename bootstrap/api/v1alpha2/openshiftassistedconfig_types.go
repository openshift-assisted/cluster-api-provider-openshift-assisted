/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha2

import (
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

const DiscoveryIgnitionOverrideAnnotation = "openshiftassistedconfig.cluster.x-k8s.io/discovery-ignition-override"

// OpenshiftAssistedConfigSpec defines the desired state of OpenshiftAssistedConfig
type OpenshiftAssistedConfigSpec struct {
	// Below some fields that would map to the InfraEnv https://github.com/openshift/assisted-service/blob/5b9d5f9197c950750f0d57dc7900a60cef255171/api/v1beta1/infraenv_types.go#L48

	// Proxy defines the proxy settings for agents and clusters that use the InfraEnv. If
	// unset, the agents and clusters will not be configured to use a proxy.
	// +optional
	Proxy *aiv1beta1.Proxy `json:"proxy,omitempty"`

	// PullSecretRef is the reference to the secret to use when pulling images.
	PullSecretRef *corev1.LocalObjectReference `json:"pullSecretRef,omitempty"`

	// AdditionalNTPSources is a list of NTP sources (hostname or IP) to be added to all cluster
	// hosts. They are added to any NTP sources that were configured through other means.
	// +optional
	AdditionalNTPSources []string `json:"additionalNTPSources,omitempty"`

	// SSHAuthorizedKey is a SSH public keys that will be added to all agents for use in debugging.
	// +optional
	SSHAuthorizedKey string `json:"sshAuthorizedKey,omitempty"`

	// NmstateConfigLabelSelector associates NMStateConfigs for hosts that are considered part
	// of this installation environment.
	// +optional
	NMStateConfigLabelSelector metav1.LabelSelector `json:"nmStateConfigLabelSelector,omitempty"`

	// CpuArchitecture specifies the target CPU architecture. Default is x86_64
	// +kubebuilder:default=x86_64
	// +optional
	CpuArchitecture string `json:"cpuArchitecture,omitempty"`

	// KernelArguments is the additional kernel arguments to be passed during boot time of the discovery image.
	// Applicable for both iPXE, and ISO streaming from Image Service.
	// +optional
	KernelArguments []aiv1beta1.KernelArgument `json:"kernelArguments,omitempty"`

	// PEM-encoded X.509 certificate bundle. Hosts discovered by this
	// infra-env will trust the certificates in this bundle. Clusters formed
	// from the hosts discovered by this infra-env will also trust the
	// certificates in this bundle.
	// +optional
	AdditionalTrustBundle string `json:"additionalTrustBundle,omitempty"`

	// OSImageVersion is the version of OS image to use when generating the InfraEnv.
	// The version should refer to an OSImage specified in the AgentServiceConfig
	// (i.e. OSImageVersion should equal to an OpenshiftVersion in OSImages list).
	// Note: OSImageVersion can't be specified along with ClusterRef.
	// +optional
	OSImageVersion string `json:"osImageVersion,omitempty"`

	// NodeRegistrationOption holds fields related to registering nodes to the cluster
	// +optional
	NodeRegistration NodeRegistrationOptions `json:"nodeRegistration,omitempty"`

	// PreInstallCommands specifies a list of commands to run on the discovery host before installation.
	// Each entry is a shell command that will be written as a line in a script and executed via a systemd
	// unit in the discovery ignition.
	// +optional
	PreInstallCommands []string `json:"preInstallCommands,omitempty"`

	// PostInstallCommands specifies a list of commands to run on the installed OCP node after installation.
	// Each entry is a shell command that will be written as a line in a script and executed via a systemd
	// unit in the node ignition.
	// +optional
	PostInstallCommands []string `json:"postInstallCommands,omitempty"`
}

// NodeRegistrationOption holds fields related to registering nodes to the cluster
type NodeRegistrationOptions struct {
	// Name specifies an environment variable reference (e.g., "$METADATA_HOSTNAME") from which
	// to read the node name. The environment variable must be available in /etc/metadata_env
	// (populated by configdrive). The value will be resolved safely and used to set the hostname.
	// +optional
	Name string `json:"name,omitempty"`

	// KubeletExtraLabels passes extra labels to kubelet.
	// +optional
	KubeletExtraLabels []string `json:"kubeletExtraLabels,omitempty"`

	// ProviderID specifies the provider ID to pass to kubelet via KUBELET_PROVIDERID environment
	// variable in /etc/kubernetes/kubelet-env. This can be a static value or an environment
	// variable reference (e.g., 'openstack://"$METADATA_UUID"') that will be resolved from /etc/metadata_env.
	// +optional
	ProviderID string `json:"providerID,omitempty"`
}

type OpenshiftAssistedConfigInitializationStatus struct {
	// dataSecretCreated is true when the Machine's bootstrap secret is created.
	// NOTE: this field is part of the Cluster API contract, and it is used to orchestrate initial Machine provisioning.
	// +optional
	DataSecretCreated *bool `json:"dataSecretCreated,omitempty"`
}

// OpenshiftAssistedConfigStatus defines the observed state of OpenshiftAssistedConfig
type OpenshiftAssistedConfigStatus struct {
	// conditions represents the observations of a OpenshiftAssistedConfig's current state.
	// Known condition types are Ready, DataSecretAvailable, CertificatesAvailable.
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=32
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// initialization provides observations of the OpenshiftAssistedConfig initialization process.
	// NOTE: Fields in this struct are part of the Cluster API contract and are used to orchestrate initial Machine provisioning.
	// +optional
	Initialization OpenshiftAssistedConfigInitializationStatus `json:"initialization,omitempty,omitzero"`

	// dataSecretName is the name of the secret that stores the bootstrap data script.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	DataSecretName string `json:"dataSecretName,omitempty"`

	// observedGeneration is the latest generation observed by the controller.
	// +optional
	// +kubebuilder:validation:Minimum=1
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// v1beta1Conditions holds the v1beta1 style conditions for backward compatibility with CAPI conditions utilities.
	// +optional
	V1Beta1Conditions clusterv1.Conditions `json:"v1beta1Conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:resource:shortName=oac;oacs
//+kubebuilder:subresource:status
//+kubebuilder:storageversion

// OpenshiftAssistedConfig is the Schema for the openshiftassistedconfig API
type OpenshiftAssistedConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OpenshiftAssistedConfigSpec   `json:"spec,omitempty"`
	Status OpenshiftAssistedConfigStatus `json:"status,omitempty"`
}

// GetConditions returns the set of conditions for this object.
func (c *OpenshiftAssistedConfig) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

// SetConditions sets the conditions on this object.
func (c *OpenshiftAssistedConfig) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// GetV1Beta1Conditions returns the v1beta1 style conditions for this object.
func (c *OpenshiftAssistedConfig) GetV1Beta1Conditions() clusterv1.Conditions {
	return c.Status.V1Beta1Conditions
}

// SetV1Beta1Conditions sets the v1beta1 style conditions on this object.
func (c *OpenshiftAssistedConfig) SetV1Beta1Conditions(conditions clusterv1.Conditions) {
	c.Status.V1Beta1Conditions = conditions
}

//+kubebuilder:object:root=true

// OpenshiftAssistedConfigList contains a list of OpenshiftAssistedConfig
type OpenshiftAssistedConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OpenshiftAssistedConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OpenshiftAssistedConfig{}, &OpenshiftAssistedConfigList{})
}
