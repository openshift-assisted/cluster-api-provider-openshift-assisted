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

package capabilities

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	configv1 "github.com/openshift/api/config/v1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	"k8s.io/apimachinery/pkg/api/equality"
)

const (
	defaultBaremetalBaselineCapability = "None"
	defaultBaselineCapability          = "vCurrent"
)

var (
	baselineCapabilityRegexp = regexp.MustCompile(`^v4\.[0-9]+$`)

	defaultBaremetalAdditionalCapabilities = []configv1.ClusterVersionCapability{
		"baremetal", "Console", "Insights", "OperatorLifecycleManager",
		"Ingress", "marketplace", "NodeTuning", "DeploymentConfig",
	}
)

// InstallConfigOverride represents the capabilities section of OpenShift install-config.yaml.
type InstallConfigOverride struct {
	Capability configv1.ClusterVersionCapabilitiesSpec `json:"capabilities,omitempty"`
}

// GetInstallConfigOverride merges install config override from annotations with capabilities-based overrides.
// It returns the final merged install config override as a JSON string, or empty string if no overrides are needed.
// The function combines user-provided install config overrides from annotations with automatically
// generated capabilities configuration based on the cluster platform type.
func GetInstallConfigOverride(
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	aci *hiveext.AgentClusterInstall,
) (string, error) {
	installConfigOverrideStr := oacp.Annotations[controlplanev1alpha3.InstallConfigOverrideAnnotation]

	capabilitiesCfgOverride, err := getInstallConfigOverrideForCapabilities(oacp, aci)
	if err != nil {
		return "", err
	}

	// Return early if either is empty
	if capabilitiesCfgOverride == "" {
		return installConfigOverrideStr, nil
	}
	if installConfigOverrideStr == "" {
		return capabilitiesCfgOverride, nil
	}

	// Both are non-empty, merge them
	return mergeJSON(installConfigOverrideStr, capabilitiesCfgOverride)
}

// getInstallConfigOverrideForCapabilities generates install config override JSON for OpenShift capabilities configuration.
// It automatically sets appropriate capabilities based on the platform type:
// - Baremetal platforms: Sets baseline to "None" and includes default baremetal capabilities
// - Non-baremetal platforms: Sets baseline to "vCurrent" and only includes user-specified capabilities
// Returns empty string if no capabilities configuration is needed (non-baremetal with empty capabilities).
func getInstallConfigOverrideForCapabilities(
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	aci *hiveext.AgentClusterInstall,
) (string, error) {
	if isCapabilitiesEmpty(oacp.Spec.Config.Capabilities) && !isBaremetalPlatform(aci) {
		return "", nil
	}

	var installCfgOverride InstallConfigOverride

	baselineCapability, err := getBaselineCapability(
		oacp.Spec.Config.Capabilities.BaselineCapability,
		isBaremetalPlatform(aci),
	)
	if err != nil {
		return "", err
	}
	installCfgOverride.Capability.BaselineCapabilitySet = configv1.ClusterVersionCapabilitySet(baselineCapability)

	additionalEnabledCapabilities := getAdditionalCapabilities(
		oacp.Spec.Config.Capabilities.AdditionalEnabledCapabilities,
		isBaremetalPlatform(aci),
	)
	installCfgOverride.Capability.AdditionalEnabledCapabilities = additionalEnabledCapabilities

	installCfgOverrideBytes, err := json.Marshal(installCfgOverride)
	if err != nil {
		return "", err
	}

	return string(installCfgOverrideBytes), nil
}

// mergeJSON merges two JSON strings by unmarshaling them into maps and combining them.
// The second JSON takes precedence over the first in case of key conflicts.
// Returns the merged JSON as a string, error if unmarshalling or marshalling fails.
func mergeJSON(json1, json2 string) (string, error) {
	var merged map[string]interface{}
	if err := json.Unmarshal([]byte(json1), &merged); err != nil {
		return "", fmt.Errorf("failed to unmarshal json1: %w", err)
	}
	if err := json.Unmarshal([]byte(json2), &merged); err != nil {
		return "", fmt.Errorf("failed to unmarshal json2: %w", err)
	}
	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		return "", fmt.Errorf("failed to marshal merged json: %w", err)
	}
	return string(mergedJSON), nil
}

func getBaselineCapability(capability string, isBaremetalPlatform bool) (string, error) {
	if capability == "None" || capability == "vCurrent" {
		return capability, nil
	}

	if capability == "" {
		if isBaremetalPlatform {
			return defaultBaremetalBaselineCapability, nil
		}
		return defaultBaselineCapability, nil
	}

	if !baselineCapabilityRegexp.MatchString(capability) {
		return "", fmt.Errorf(
			"invalid baseline capability set, must be one of: None, vCurrent, or v4.x. Got: [%s]",
			capability,
		)
	}
	return capability, nil
}

func getAdditionalCapabilities(
	specifiedAdditionalCapabilities []string,
	isBaremetalPlatform bool,
) []configv1.ClusterVersionCapability {
	var additionalCapabilitiesList []configv1.ClusterVersionCapability
	if isBaremetalPlatform {
		additionalCapabilitiesList = append(
			[]configv1.ClusterVersionCapability{},
			defaultBaremetalAdditionalCapabilities...,
		)
	}

	for _, capability := range specifiedAdditionalCapabilities {
		// Ignore MAPI for baremetal MNO clusters and ignore duplicates
		if (strings.EqualFold(capability, "MachineAPI") && isBaremetalPlatform) ||
			slices.Contains(additionalCapabilitiesList, configv1.ClusterVersionCapability(capability)) {
			continue
		}
		additionalCapabilitiesList = append(
			additionalCapabilitiesList,
			configv1.ClusterVersionCapability(capability),
		)
	}

	if len(additionalCapabilitiesList) < 1 {
		return nil
	}

	return additionalCapabilitiesList
}

func isCapabilitiesEmpty(capabilities controlplanev1alpha3.Capabilities) bool {
	return equality.Semantic.DeepEqual(capabilities, controlplanev1alpha3.Capabilities{})
}

// isBaremetalPlatform checks if the AgentClusterInstall is configured for a baremetal platform.
// Returns true if the platform type is BareMetalPlatformType, false otherwise.
func isBaremetalPlatform(aci *hiveext.AgentClusterInstall) bool {
	return aci.Spec.PlatformType == hiveext.BareMetalPlatformType
}
