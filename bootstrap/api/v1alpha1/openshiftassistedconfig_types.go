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

package v1alpha1

import (
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
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
}

// NodeRegistrationOption holds fields related to registering nodes to the cluster
type NodeRegistrationOptions struct {
	// Defaults to the hostname of the node if not provided.
	// +optional
	Name string `json:"name,omitempty"`

	// KubeletExtraLabels passes extra labels to kubelet.
	// +optional
	KubeletExtraLabels []string `json:"kubeletExtraLabels,omitempty"`
}

// OpenshiftAssistedConfigStatus defines the observed state of OpenshiftAssistedConfig
type OpenshiftAssistedConfigStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// InfraEnvRef references the infra env to generate the ISO
	InfraEnvRef *corev1.ObjectReference `json:"infraEnvRef,omitempty"`

	// AgentRef references the agent this agent bootstrap config has booted
	AgentRef *corev1.LocalObjectReference `json:"agentRef,omitempty"`

	// Ready indicates the BootstrapData field is ready to be consumed
	// +optional
	Ready bool `json:"ready"`

	// DataSecretName is the name of the secret that stores the bootstrap data script.
	// +optional
	DataSecretName *string `json:"dataSecretName,omitempty"`

	// FailureReason will be set on non-retryable errors
	// +optional
	FailureReason string `json:"failureReason,omitempty"`

	// FailureMessage will be set on non-retryable errors
	// +optional
	FailureMessage string `json:"failureMessage,omitempty"`

	// ObservedGeneration is the latest generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions defines current service state of the OpenshiftAssistedConfig.
	// +optional
	Conditions clusterv1.Conditions `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:resource:shortName=oac;oacs
//+kubebuilder:subresource:status

// OpenshiftAssistedConfig is the Schema for the openshiftassistedconfig API
type OpenshiftAssistedConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OpenshiftAssistedConfigSpec   `json:"spec,omitempty"`
	Status OpenshiftAssistedConfigStatus `json:"status,omitempty"`
}

// GetConditions returns the set of conditions for this object.
func (c *OpenshiftAssistedConfig) GetConditions() clusterv1.Conditions {
	return c.Status.Conditions
}

// SetConditions sets the conditions on this object.
func (c *OpenshiftAssistedConfig) SetConditions(conditions clusterv1.Conditions) {
	c.Status.Conditions = conditions
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
