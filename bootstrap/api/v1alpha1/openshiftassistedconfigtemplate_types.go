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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// OpenshiftAssistedConfigTemplateSpec defines the desired state of OpenshiftAssistedConfigTemplate
type OpenshiftAssistedConfigTemplateSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// OpenshiftAssistedConfig template
	Template OpenshiftAssistedConfigTemplateResource `json:"template"`
}

// OpenshiftAssistedConfigTemplateResource defines the Template structure.
type OpenshiftAssistedConfigTemplateResource struct {
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	ObjectMeta clusterv1.ObjectMeta `json:"metadata,omitempty"`

	Spec OpenshiftAssistedConfigSpec `json:"spec,omitempty"`
}

// OpenshiftAssistedConfigTemplateStatus defines the observed state of OpenshiftAssistedConfigTemplate
type OpenshiftAssistedConfigTemplateStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// OpenshiftAssistedConfigTemplate is the Schema for the agentbootstrapconfigtemplates API
type OpenshiftAssistedConfigTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OpenshiftAssistedConfigTemplateSpec   `json:"spec,omitempty"`
	Status OpenshiftAssistedConfigTemplateStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// OpenshiftAssistedConfigTemplateList contains a list of OpenshiftAssistedConfigTemplate
type OpenshiftAssistedConfigTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OpenshiftAssistedConfigTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OpenshiftAssistedConfigTemplate{}, &OpenshiftAssistedConfigTemplateList{})
}
