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
	"k8s.io/utils/ptr"
	clusterv1beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	utilconversion "sigs.k8s.io/cluster-api/util/conversion"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	bootstrapv1alpha2 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/bootstrap/api/v1alpha2"
)

// ConvertTo converts this OpenshiftAssistedConfig (v1alpha1) to the Hub version (v1alpha2).
func (src *OpenshiftAssistedConfig) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*bootstrapv1alpha2.OpenshiftAssistedConfig)

	dst.ObjectMeta = src.ObjectMeta

	dst.Spec = bootstrapv1alpha2.OpenshiftAssistedConfigSpec{
		Proxy:                      src.Spec.Proxy,
		PullSecretRef:              src.Spec.PullSecretRef,
		AdditionalNTPSources:       src.Spec.AdditionalNTPSources,
		SSHAuthorizedKey:           src.Spec.SSHAuthorizedKey,
		NMStateConfigLabelSelector: src.Spec.NMStateConfigLabelSelector,
		CpuArchitecture:            src.Spec.CpuArchitecture,
		KernelArguments:            src.Spec.KernelArguments,
		AdditionalTrustBundle:      src.Spec.AdditionalTrustBundle,
		OSImageVersion:             src.Spec.OSImageVersion,
		NodeRegistration: bootstrapv1alpha2.NodeRegistrationOptions{
			Name:               src.Spec.NodeRegistration.Name,
			KubeletExtraLabels: src.Spec.NodeRegistration.KubeletExtraLabels,
		},
	}

	// Use MarshalData to preserve any additional fields and handle conditions conversion
	if err := utilconversion.MarshalData(src, dst); err != nil {
		return err
	}

	// Convert DataSecretName from *string to string
	if src.Status.DataSecretName != nil {
		dst.Status.DataSecretName = *src.Status.DataSecretName
		// Set initialization status based on DataSecretName presence
		dst.Status.Initialization.DataSecretCreated = ptr.To(true)
	}

	// Convert Ready field to DataSecretCreated
	if src.Status.Ready {
		if dst.Status.Initialization.DataSecretCreated == nil {
			dst.Status.Initialization.DataSecretCreated = ptr.To(true)
		}
	}

	dst.Status.ObservedGeneration = src.Status.ObservedGeneration

	return nil
}

// ConvertFrom converts from the Hub version (v1alpha2) to this OpenshiftAssistedConfig (v1alpha1).
func (dst *OpenshiftAssistedConfig) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*bootstrapv1alpha2.OpenshiftAssistedConfig)

	dst.ObjectMeta = src.ObjectMeta
	dst.Spec = OpenshiftAssistedConfigSpec{
		Proxy:                      src.Spec.Proxy,
		PullSecretRef:              src.Spec.PullSecretRef,
		AdditionalNTPSources:       src.Spec.AdditionalNTPSources,
		SSHAuthorizedKey:           src.Spec.SSHAuthorizedKey,
		NMStateConfigLabelSelector: src.Spec.NMStateConfigLabelSelector,
		CpuArchitecture:            src.Spec.CpuArchitecture,
		KernelArguments:            src.Spec.KernelArguments,
		AdditionalTrustBundle:      src.Spec.AdditionalTrustBundle,
		OSImageVersion:             src.Spec.OSImageVersion,
		NodeRegistration: NodeRegistrationOptions{
			Name:               src.Spec.NodeRegistration.Name,
			KubeletExtraLabels: src.Spec.NodeRegistration.KubeletExtraLabels,
		},
	}

	// Use UnmarshalData to restore any additional fields and handle conditions conversion
	if _, err := utilconversion.UnmarshalData(src, dst); err != nil {
		return err
	}

	// Convert DataSecretName from string to *string
	if src.Status.DataSecretName != "" {
		dst.Status.DataSecretName = &src.Status.DataSecretName
	}

	// Convert DataSecretCreated to Ready
	if src.Status.Initialization.DataSecretCreated != nil && *src.Status.Initialization.DataSecretCreated {
		dst.Status.Ready = true
	}

	dst.Status.ObservedGeneration = src.Status.ObservedGeneration

	return nil
}

// ConvertTo converts this OpenshiftAssistedConfigTemplate (v1alpha1) to the Hub version (v1alpha2).
func (src *OpenshiftAssistedConfigTemplate) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*bootstrapv1alpha2.OpenshiftAssistedConfigTemplate)

	dst.ObjectMeta = src.ObjectMeta
	convertObjectMetaToV1Beta2(&src.Spec.Template.ObjectMeta, &dst.Spec.Template.ObjectMeta)
	dst.Spec.Template.Spec = bootstrapv1alpha2.OpenshiftAssistedConfigSpec{
		Proxy:                      src.Spec.Template.Spec.Proxy,
		PullSecretRef:              src.Spec.Template.Spec.PullSecretRef,
		AdditionalNTPSources:       src.Spec.Template.Spec.AdditionalNTPSources,
		SSHAuthorizedKey:           src.Spec.Template.Spec.SSHAuthorizedKey,
		NMStateConfigLabelSelector: src.Spec.Template.Spec.NMStateConfigLabelSelector,
		CpuArchitecture:            src.Spec.Template.Spec.CpuArchitecture,
		KernelArguments:            src.Spec.Template.Spec.KernelArguments,
		AdditionalTrustBundle:      src.Spec.Template.Spec.AdditionalTrustBundle,
		OSImageVersion:             src.Spec.Template.Spec.OSImageVersion,
		NodeRegistration: bootstrapv1alpha2.NodeRegistrationOptions{
			Name:               src.Spec.Template.Spec.NodeRegistration.Name,
			KubeletExtraLabels: src.Spec.Template.Spec.NodeRegistration.KubeletExtraLabels,
		},
	}

	return utilconversion.MarshalData(src, dst)
}

// ConvertFrom converts from the Hub version (v1alpha2) to this OpenshiftAssistedConfigTemplate (v1alpha1).
func (dst *OpenshiftAssistedConfigTemplate) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*bootstrapv1alpha2.OpenshiftAssistedConfigTemplate)

	dst.ObjectMeta = src.ObjectMeta
	convertObjectMetaFromV1Beta2(&src.Spec.Template.ObjectMeta, &dst.Spec.Template.ObjectMeta)
	dst.Spec.Template.Spec = OpenshiftAssistedConfigSpec{
		Proxy:                      src.Spec.Template.Spec.Proxy,
		PullSecretRef:              src.Spec.Template.Spec.PullSecretRef,
		AdditionalNTPSources:       src.Spec.Template.Spec.AdditionalNTPSources,
		SSHAuthorizedKey:           src.Spec.Template.Spec.SSHAuthorizedKey,
		NMStateConfigLabelSelector: src.Spec.Template.Spec.NMStateConfigLabelSelector,
		CpuArchitecture:            src.Spec.Template.Spec.CpuArchitecture,
		KernelArguments:            src.Spec.Template.Spec.KernelArguments,
		AdditionalTrustBundle:      src.Spec.Template.Spec.AdditionalTrustBundle,
		OSImageVersion:             src.Spec.Template.Spec.OSImageVersion,
		NodeRegistration: NodeRegistrationOptions{
			Name:               src.Spec.Template.Spec.NodeRegistration.Name,
			KubeletExtraLabels: src.Spec.Template.Spec.NodeRegistration.KubeletExtraLabels,
		},
	}

	_, err := utilconversion.UnmarshalData(src, dst)
	return err
}

// convertObjectMetaToV1Beta2 converts v1beta1 ObjectMeta to v1beta2 ObjectMeta.
// ObjectMeta in both versions contains only Labels and Annotations.
func convertObjectMetaToV1Beta2(in *clusterv1beta1.ObjectMeta, out *clusterv1.ObjectMeta) {
	out.Labels = in.Labels
	out.Annotations = in.Annotations
}

// convertObjectMetaFromV1Beta2 converts v1beta2 ObjectMeta back to v1beta1 ObjectMeta.
// ObjectMeta in both versions contains only Labels and Annotations.
func convertObjectMetaFromV1Beta2(in *clusterv1.ObjectMeta, out *clusterv1beta1.ObjectMeta) {
	out.Labels = in.Labels
	out.Annotations = in.Annotations
}
