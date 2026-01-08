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
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	clusterv1beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	utilconversion "sigs.k8s.io/cluster-api/util/conversion"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	bootstrapv1alpha1 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/bootstrap/api/v1alpha1"
	bootstrapv1alpha2 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/bootstrap/api/v1alpha2"
	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
)

// ConvertTo converts this OpenshiftAssistedControlPlane (v1alpha2) to the Hub version (v1alpha3).
func (src *OpenshiftAssistedControlPlane) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*controlplanev1alpha3.OpenshiftAssistedControlPlane)

	dst.ObjectMeta = src.ObjectMeta

	dst.Spec.Config = controlplanev1alpha3.OpenshiftAssistedControlPlaneConfigSpec{
		APIVIPs:                src.Spec.Config.APIVIPs,
		IngressVIPs:            src.Spec.Config.IngressVIPs,
		ManifestsConfigMapRefs: src.Spec.Config.ManifestsConfigMapRefs,
		DiskEncryption:         src.Spec.Config.DiskEncryption,
		Proxy:                  src.Spec.Config.Proxy,
		MastersSchedulable:     src.Spec.Config.MastersSchedulable,
		SSHAuthorizedKey:       src.Spec.Config.SSHAuthorizedKey,
		ClusterName:            src.Spec.Config.ClusterName,
		BaseDomain:             src.Spec.Config.BaseDomain,
		PullSecretRef:          src.Spec.Config.PullSecretRef,
		ImageRegistryRef:       src.Spec.Config.ImageRegistryRef,
		Capabilities: controlplanev1alpha3.Capabilities{
			BaselineCapability:            src.Spec.Config.Capabilities.BaselineCapability,
			AdditionalEnabledCapabilities: src.Spec.Config.Capabilities.AdditionalEnabledCapabilities,
		},
	}
	convertBootstrapConfigSpecToV1Alpha2(&src.Spec.OpenshiftAssistedConfigSpec, &dst.Spec.OpenshiftAssistedConfigSpec)
	dst.Spec.Replicas = src.Spec.Replicas
	dst.Spec.DistributionVersion = src.Spec.DistributionVersion

	convertObjectMetaToV1Beta2(&src.Spec.MachineTemplate.ObjectMeta, &dst.Spec.MachineTemplate.ObjectMeta)
	dst.Spec.MachineTemplate.InfrastructureRef = convertObjectRefToContractVersionedRef(&src.Spec.MachineTemplate.InfrastructureRef)

	// v1alpha2 uses separate Duration fields, v1alpha3 uses seconds in MachineDeletionSpec
	dst.Spec.MachineTemplate.Deletion = clusterv1.MachineDeletionSpec{
		NodeDrainTimeoutSeconds:        durationToSeconds(src.Spec.MachineTemplate.NodeDrainTimeout),
		NodeVolumeDetachTimeoutSeconds: durationToSeconds(src.Spec.MachineTemplate.NodeVolumeDetachTimeout),
		NodeDeletionTimeoutSeconds:     durationToSeconds(src.Spec.MachineTemplate.NodeDeletionTimeout),
	}

	// Use MarshalData to preserve additional fields during round-trip conversion
	if err := utilconversion.MarshalData(src, dst); err != nil {
		return err
	}

	if src.Status.Conditions != nil {
		dst.Status.Conditions = convertV1Beta1ConditionsToMetav1(src.Status.Conditions)
	}

	dst.Status.Selector = src.Status.Selector
	if src.Status.Replicas > 0 {
		dst.Status.Replicas = &src.Status.Replicas
	}
	if src.Status.ReadyReplicas > 0 {
		dst.Status.ReadyReplicas = &src.Status.ReadyReplicas
	}
	if src.Status.UpdatedReplicas > 0 {
		dst.Status.UpToDateReplicas = &src.Status.UpdatedReplicas
	}
	dst.Status.Version = ptr.Deref(src.Status.Version, "")
	dst.Status.DistributionVersion = src.Status.DistributionVersion

	if src.Status.Initialized {
		dst.Status.Initialization.ControlPlaneInitialized = ptr.To(true)
	}

	return nil
}

// ConvertFrom converts from the Hub version (v1alpha3) to this OpenshiftAssistedControlPlane (v1alpha2).
func (dst *OpenshiftAssistedControlPlane) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*controlplanev1alpha3.OpenshiftAssistedControlPlane)

	dst.ObjectMeta = src.ObjectMeta

	// Config structs are identical but in different packages
	dst.Spec.Config = OpenshiftAssistedControlPlaneConfigSpec{
		APIVIPs:                src.Spec.Config.APIVIPs,
		IngressVIPs:            src.Spec.Config.IngressVIPs,
		ManifestsConfigMapRefs: src.Spec.Config.ManifestsConfigMapRefs,
		DiskEncryption:         src.Spec.Config.DiskEncryption,
		Proxy:                  src.Spec.Config.Proxy,
		MastersSchedulable:     src.Spec.Config.MastersSchedulable,
		SSHAuthorizedKey:       src.Spec.Config.SSHAuthorizedKey,
		ClusterName:            src.Spec.Config.ClusterName,
		BaseDomain:             src.Spec.Config.BaseDomain,
		PullSecretRef:          src.Spec.Config.PullSecretRef,
		ImageRegistryRef:       src.Spec.Config.ImageRegistryRef,
		Capabilities: Capabilities{
			BaselineCapability:            src.Spec.Config.Capabilities.BaselineCapability,
			AdditionalEnabledCapabilities: src.Spec.Config.Capabilities.AdditionalEnabledCapabilities,
		},
	}
	convertBootstrapConfigSpecFromV1Alpha2(&src.Spec.OpenshiftAssistedConfigSpec, &dst.Spec.OpenshiftAssistedConfigSpec)
	dst.Spec.Replicas = src.Spec.Replicas
	dst.Spec.DistributionVersion = src.Spec.DistributionVersion

	convertObjectMetaFromV1Beta2(&src.Spec.MachineTemplate.ObjectMeta, &dst.Spec.MachineTemplate.ObjectMeta)
	dst.Spec.MachineTemplate.InfrastructureRef = convertContractVersionedRefToObjectRef(&src.Spec.MachineTemplate.InfrastructureRef)

	// v1alpha3 uses seconds in MachineDeletionSpec, v1alpha2 uses separate Duration fields
	dst.Spec.MachineTemplate.NodeDrainTimeout = secondsToDuration(src.Spec.MachineTemplate.Deletion.NodeDrainTimeoutSeconds)
	dst.Spec.MachineTemplate.NodeVolumeDetachTimeout = secondsToDuration(src.Spec.MachineTemplate.Deletion.NodeVolumeDetachTimeoutSeconds)
	dst.Spec.MachineTemplate.NodeDeletionTimeout = secondsToDuration(src.Spec.MachineTemplate.Deletion.NodeDeletionTimeoutSeconds)

	// Use UnmarshalData to restore additional fields during round-trip conversion
	if _, err := utilconversion.UnmarshalData(src, dst); err != nil {
		return err
	}

	dst.Status.Conditions = make(clusterv1beta1.Conditions, 0, len(src.Status.Conditions))
	for _, c := range src.Status.Conditions {
		dst.Status.Conditions = append(dst.Status.Conditions, clusterv1beta1.Condition{
			Type:               clusterv1beta1.ConditionType(c.Type),
			Status:             corev1.ConditionStatus(c.Status),
			LastTransitionTime: c.LastTransitionTime,
			Reason:             c.Reason,
			Message:            c.Message,
			Severity:           clusterv1beta1.ConditionSeverityInfo,
		})
	}

	dst.Status.Selector = src.Status.Selector
	if src.Status.Replicas != nil {
		dst.Status.Replicas = *src.Status.Replicas
	}
	if src.Status.ReadyReplicas != nil {
		dst.Status.ReadyReplicas = *src.Status.ReadyReplicas
	}
	if src.Status.UpToDateReplicas != nil {
		dst.Status.UpdatedReplicas = *src.Status.UpToDateReplicas
	}
	if src.Status.Version != "" {
		dst.Status.Version = &src.Status.Version
	}
	dst.Status.DistributionVersion = src.Status.DistributionVersion

	// v1alpha2 has UnavailableReplicas, v1alpha3 doesn't - compute it
	if src.Status.AvailableReplicas != nil {
		unavailable := src.Spec.Replicas - *src.Status.AvailableReplicas
		if unavailable < 0 {
			unavailable = 0
		}
		dst.Status.UnavailableReplicas = unavailable
	}

	if src.Status.Initialization.ControlPlaneInitialized != nil && *src.Status.Initialization.ControlPlaneInitialized {
		dst.Status.Initialized = true
	}

	// v1alpha2 has Ready field that must be derived from ControlPlaneReady condition
	for _, condition := range src.Status.Conditions {
		if condition.Type == string(ControlPlaneReadyCondition) && condition.Status == metav1.ConditionTrue {
			dst.Status.Ready = true
			break
		}
	}

	return nil
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

// durationToSeconds converts a metav1.Duration to seconds as *int32.
// Returns nil if input is nil.
func durationToSeconds(d *metav1.Duration) *int32 {
	if d == nil {
		return nil
	}
	seconds := int32(d.Seconds())
	return &seconds
}

// secondsToDuration converts seconds as *int32 to a metav1.Duration.
// Returns nil if input is nil.
func secondsToDuration(seconds *int32) *metav1.Duration {
	if seconds == nil {
		return nil
	}
	return &metav1.Duration{Duration: time.Duration(*seconds) * time.Second}
}

// convertBootstrapConfigSpecToV1Alpha2 converts OpenshiftAssistedConfigSpec from bootstrap v1alpha1 to v1alpha2.
// The specs are identical except they're in different packages.
func convertBootstrapConfigSpecToV1Alpha2(in *bootstrapv1alpha1.OpenshiftAssistedConfigSpec, out *bootstrapv1alpha2.OpenshiftAssistedConfigSpec) {
	out.Proxy = in.Proxy
	out.PullSecretRef = in.PullSecretRef
	out.AdditionalNTPSources = in.AdditionalNTPSources
	out.SSHAuthorizedKey = in.SSHAuthorizedKey
	out.NMStateConfigLabelSelector = in.NMStateConfigLabelSelector
	out.CpuArchitecture = in.CpuArchitecture
	out.KernelArguments = in.KernelArguments
	out.AdditionalTrustBundle = in.AdditionalTrustBundle
	out.OSImageVersion = in.OSImageVersion
	out.NodeRegistration = bootstrapv1alpha2.NodeRegistrationOptions{
		Name:               in.NodeRegistration.Name,
		KubeletExtraLabels: in.NodeRegistration.KubeletExtraLabels,
	}
}

// convertBootstrapConfigSpecFromV1Alpha2 converts OpenshiftAssistedConfigSpec from bootstrap v1alpha2 to v1alpha1.
// The specs are identical except they're in different packages.
func convertBootstrapConfigSpecFromV1Alpha2(in *bootstrapv1alpha2.OpenshiftAssistedConfigSpec, out *bootstrapv1alpha1.OpenshiftAssistedConfigSpec) {
	out.Proxy = in.Proxy
	out.PullSecretRef = in.PullSecretRef
	out.AdditionalNTPSources = in.AdditionalNTPSources
	out.SSHAuthorizedKey = in.SSHAuthorizedKey
	out.NMStateConfigLabelSelector = in.NMStateConfigLabelSelector
	out.CpuArchitecture = in.CpuArchitecture
	out.KernelArguments = in.KernelArguments
	out.AdditionalTrustBundle = in.AdditionalTrustBundle
	out.OSImageVersion = in.OSImageVersion
	out.NodeRegistration = bootstrapv1alpha1.NodeRegistrationOptions{
		Name:               in.NodeRegistration.Name,
		KubeletExtraLabels: in.NodeRegistration.KubeletExtraLabels,
	}
}

// convertObjectRefToContractVersionedRef converts corev1.ObjectReference (with APIVersion)
// to ContractVersionedObjectReference (with APIGroup).
// APIVersion format: "group/version" -> APIGroup: "group"
func convertObjectRefToContractVersionedRef(in *corev1.ObjectReference) clusterv1.ContractVersionedObjectReference {
	apiGroup := ""
	if in.APIVersion != "" {
		// Extract group from apiVersion (e.g., "infrastructure.cluster.x-k8s.io/v1beta1" -> "infrastructure.cluster.x-k8s.io")
		parts := strings.Split(in.APIVersion, "/")
		if len(parts) >= 1 {
			// If there's no slash, the whole string is the group (for core types it might just be "v1")
			// If there's a slash, the first part is the group
			if len(parts) == 2 {
				apiGroup = parts[0]
			}
			// For core types like "v1", there's no group
		}
	}
	return clusterv1.ContractVersionedObjectReference{
		Kind:     in.Kind,
		Name:     in.Name,
		APIGroup: apiGroup,
	}
}

// convertContractVersionedRefToObjectRef converts ContractVersionedObjectReference (with APIGroup)
// to corev1.ObjectReference (with APIVersion).
// Note: We lose the version information, so we need to restore it from annotations or use a default.
func convertContractVersionedRefToObjectRef(in *clusterv1.ContractVersionedObjectReference) corev1.ObjectReference {
	// When converting back, we need to reconstruct the APIVersion.
	// Since ContractVersionedObjectReference only has APIGroup, we use a convention:
	// - For infrastructure.cluster.x-k8s.io, assume v1beta1
	// - This matches the cluster-api contract version approach
	apiVersion := ""
	if in.APIGroup != "" {
		// Default to v1beta1 for infrastructure group, as that's the common case
		apiVersion = in.APIGroup + "/v1beta1"
	}
	return corev1.ObjectReference{
		Kind:       in.Kind,
		Name:       in.Name,
		APIVersion: apiVersion,
	}
}

// convertV1Beta1ConditionsToMetav1 converts clusterv1beta1.Conditions to []metav1.Condition.
// This is needed when converting from v1alpha2 to v1alpha3 to populate the new conditions.
func convertV1Beta1ConditionsToMetav1(in clusterv1beta1.Conditions) []metav1.Condition {
	out := make([]metav1.Condition, 0, len(in))
	for _, c := range in {
		reason := c.Reason
		// metav1.Condition requires reason to be non-empty (minLength: 1)
		if reason == "" {
			reason = "Unknown"
		}
		out = append(out, metav1.Condition{
			Type:               string(c.Type),
			Status:             metav1.ConditionStatus(c.Status),
			LastTransitionTime: c.LastTransitionTime,
			Reason:             reason,
			Message:            c.Message,
			// Note: Severity from v1beta1 is lost as metav1.Condition doesn't have it
		})
	}
	return out
}
