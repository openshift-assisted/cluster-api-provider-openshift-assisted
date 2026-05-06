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
	"testing"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	configv1 "github.com/openshift/api/config/v1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCapabilities(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Capabilities Suite")
}

var _ = Describe("GetInstallConfigOverride", func() {
	var (
		oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane
		aci  *hiveext.AgentClusterInstall
	)

	BeforeEach(func() {
		oacp = &controlplanev1alpha3.OpenshiftAssistedControlPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "test-cluster",
				Namespace:   "test-namespace",
				Annotations: map[string]string{},
			},
			Spec: controlplanev1alpha3.OpenshiftAssistedControlPlaneSpec{
				Config: controlplanev1alpha3.OpenshiftAssistedControlPlaneConfig{
					Capabilities: controlplanev1alpha3.Capabilities{},
				},
			},
		}
		aci = &hiveext.AgentClusterInstall{
			Spec: hiveext.AgentClusterInstallSpec{},
		}
	})

	Context("Non-baremetal platform with empty capabilities", func() {
		It("returns empty string when no capabilities or annotations are set", func() {
			aci.Spec.PlatformType = hiveext.VSpherePlatformType
			result, err := GetInstallConfigOverride(oacp, aci)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(""))
		})
	})

	Context("Baremetal platform with default capabilities", func() {
		It("sets baseline to None and includes default baremetal capabilities", func() {
			aci.Spec.PlatformType = hiveext.BareMetalPlatformType
			result, err := GetInstallConfigOverride(oacp, aci)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeEmpty())

			var override InstallConfigOverride
			err = json.Unmarshal([]byte(result), &override)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(override.Capability.BaselineCapabilitySet)).To(Equal("None"))
			Expect(override.Capability.AdditionalEnabledCapabilities).To(ContainElement(configv1.ClusterVersionCapability("baremetal")))
			Expect(override.Capability.AdditionalEnabledCapabilities).To(ContainElement(configv1.ClusterVersionCapability("Console")))
		})
	})

	Context("Non-baremetal platform with custom capabilities", func() {
		It("sets baseline to vCurrent and includes user-specified capabilities", func() {
			aci.Spec.PlatformType = hiveext.VSpherePlatformType
			oacp.Spec.Config.Capabilities.AdditionalEnabledCapabilities = []string{"Storage", "CSISnapshot"}
			result, err := GetInstallConfigOverride(oacp, aci)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeEmpty())

			var override InstallConfigOverride
			err = json.Unmarshal([]byte(result), &override)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(override.Capability.BaselineCapabilitySet)).To(Equal("vCurrent"))
			Expect(override.Capability.AdditionalEnabledCapabilities).To(ConsistOf(
				configv1.ClusterVersionCapability("Storage"),
				configv1.ClusterVersionCapability("CSISnapshot"),
			))
		})
	})

	Context("Baremetal platform with MachineAPI capability requested", func() {
		It("filters out MachineAPI for baremetal platforms", func() {
			aci.Spec.PlatformType = hiveext.BareMetalPlatformType
			oacp.Spec.Config.Capabilities.AdditionalEnabledCapabilities = []string{"MachineAPI", "Storage"}
			result, err := GetInstallConfigOverride(oacp, aci)
			Expect(err).NotTo(HaveOccurred())

			var override InstallConfigOverride
			err = json.Unmarshal([]byte(result), &override)
			Expect(err).NotTo(HaveOccurred())
			Expect(override.Capability.AdditionalEnabledCapabilities).NotTo(ContainElement(configv1.ClusterVersionCapability("MachineAPI")))
			Expect(override.Capability.AdditionalEnabledCapabilities).To(ContainElement(configv1.ClusterVersionCapability("Storage")))
		})
	})

	Context("Custom baseline capability", func() {
		It("accepts valid version-based baseline capability", func() {
			oacp.Spec.Config.Capabilities.BaselineCapability = "v4.16"
			oacp.Spec.Config.Capabilities.AdditionalEnabledCapabilities = []string{"Storage"}
			result, err := GetInstallConfigOverride(oacp, aci)
			Expect(err).NotTo(HaveOccurred())

			var override InstallConfigOverride
			err = json.Unmarshal([]byte(result), &override)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(override.Capability.BaselineCapabilitySet)).To(Equal("v4.16"))
		})

		It("accepts None as baseline capability", func() {
			oacp.Spec.Config.Capabilities.BaselineCapability = "None"
			oacp.Spec.Config.Capabilities.AdditionalEnabledCapabilities = []string{"Storage"}
			result, err := GetInstallConfigOverride(oacp, aci)
			Expect(err).NotTo(HaveOccurred())

			var override InstallConfigOverride
			err = json.Unmarshal([]byte(result), &override)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(override.Capability.BaselineCapabilitySet)).To(Equal("None"))
		})

		It("rejects invalid baseline capability format", func() {
			oacp.Spec.Config.Capabilities.BaselineCapability = "invalid"
			oacp.Spec.Config.Capabilities.AdditionalEnabledCapabilities = []string{"Storage"}
			_, err := GetInstallConfigOverride(oacp, aci)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid baseline capability set"))
		})
	})

	Context("Merging with existing install config override annotation", func() {
		It("merges capabilities with existing install config override", func() {
			aci.Spec.PlatformType = hiveext.BareMetalPlatformType
			existingOverride := `{"networking":{"networkType":"OVNKubernetes"}}`
			oacp.Annotations[controlplanev1alpha3.InstallConfigOverrideAnnotation] = existingOverride

			result, err := GetInstallConfigOverride(oacp, aci)
			Expect(err).NotTo(HaveOccurred())

			var merged map[string]interface{}
			err = json.Unmarshal([]byte(result), &merged)
			Expect(err).NotTo(HaveOccurred())
			Expect(merged).To(HaveKey("networking"))
			Expect(merged).To(HaveKey("capabilities"))
		})

		It("returns existing annotation when capabilities config is empty for non-baremetal", func() {
			aci.Spec.PlatformType = hiveext.VSpherePlatformType
			existingOverride := `{"networking":{"networkType":"OVNKubernetes"}}`
			oacp.Annotations[controlplanev1alpha3.InstallConfigOverrideAnnotation] = existingOverride

			result, err := GetInstallConfigOverride(oacp, aci)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(existingOverride))
		})

		It("handles malformed JSON in annotation", func() {
			aci.Spec.PlatformType = hiveext.BareMetalPlatformType
			oacp.Annotations[controlplanev1alpha3.InstallConfigOverrideAnnotation] = `{invalid json`

			_, err := GetInstallConfigOverride(oacp, aci)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to unmarshal json1"))
		})
	})

	Context("Duplicate capabilities handling", func() {
		It("deduplicates user-specified capabilities", func() {
			aci.Spec.PlatformType = hiveext.VSpherePlatformType
			oacp.Spec.Config.Capabilities.AdditionalEnabledCapabilities = []string{"Storage", "Storage", "CSISnapshot"}
			result, err := GetInstallConfigOverride(oacp, aci)
			Expect(err).NotTo(HaveOccurred())

			var override InstallConfigOverride
			err = json.Unmarshal([]byte(result), &override)
			Expect(err).NotTo(HaveOccurred())
			Expect(override.Capability.AdditionalEnabledCapabilities).To(HaveLen(2))
		})

		It("deduplicates capabilities that overlap with defaults on baremetal", func() {
			aci.Spec.PlatformType = hiveext.BareMetalPlatformType
			oacp.Spec.Config.Capabilities.AdditionalEnabledCapabilities = []string{"Console", "Storage"}
			result, err := GetInstallConfigOverride(oacp, aci)
			Expect(err).NotTo(HaveOccurred())

			var override InstallConfigOverride
			err = json.Unmarshal([]byte(result), &override)
			Expect(err).NotTo(HaveOccurred())
			// Should have defaults + Storage, with Console appearing only once
			consoleCount := 0
			for _, cap := range override.Capability.AdditionalEnabledCapabilities {
				if cap == "Console" {
					consoleCount++
				}
			}
			Expect(consoleCount).To(Equal(1))
			Expect(override.Capability.AdditionalEnabledCapabilities).To(ContainElement(configv1.ClusterVersionCapability("Storage")))
		})
	})
})

var _ = Describe("mergeJSON", func() {
	It("merges two simple JSON objects", func() {
		json1 := `{"key1":"value1"}`
		json2 := `{"key2":"value2"}`
		result, err := mergeJSON(json1, json2)
		Expect(err).NotTo(HaveOccurred())

		var merged map[string]interface{}
		err = json.Unmarshal([]byte(result), &merged)
		Expect(err).NotTo(HaveOccurred())
		Expect(merged).To(HaveKeyWithValue("key1", "value1"))
		Expect(merged).To(HaveKeyWithValue("key2", "value2"))
	})

	It("overwrites first JSON with second JSON when keys conflict", func() {
		json1 := `{"key":"value1"}`
		json2 := `{"key":"value2"}`
		result, err := mergeJSON(json1, json2)
		Expect(err).NotTo(HaveOccurred())

		var merged map[string]interface{}
		err = json.Unmarshal([]byte(result), &merged)
		Expect(err).NotTo(HaveOccurred())
		Expect(merged).To(HaveKeyWithValue("key", "value2"))
	})

	It("returns error for invalid first JSON", func() {
		_, err := mergeJSON(`{invalid`, `{"key":"value"}`)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to unmarshal json1"))
	})

	It("returns error for invalid second JSON", func() {
		_, err := mergeJSON(`{"key":"value"}`, `{invalid`)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to unmarshal json2"))
	})
})

var _ = Describe("getBaselineCapability", func() {
	It("accepts None", func() {
		result, err := getBaselineCapability("None", false)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("None"))
	})

	It("accepts vCurrent", func() {
		result, err := getBaselineCapability("vCurrent", false)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("vCurrent"))
	})

	It("accepts valid version format v4.X", func() {
		result, err := getBaselineCapability("v4.16", false)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("v4.16"))
	})

	It("defaults to vCurrent for non-baremetal when empty", func() {
		result, err := getBaselineCapability("", false)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("vCurrent"))
	})

	It("defaults to None for baremetal when empty", func() {
		result, err := getBaselineCapability("", true)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("None"))
	})

	It("rejects invalid format", func() {
		_, err := getBaselineCapability("v5.0", false)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid baseline capability set"))
	})

	It("rejects malformed version", func() {
		_, err := getBaselineCapability("v4", false)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("getAdditionalCapabilities", func() {
	It("returns nil for empty input on non-baremetal", func() {
		result := getAdditionalCapabilities([]string{}, false)
		Expect(result).To(BeNil())
	})

	It("returns default capabilities for baremetal with empty input", func() {
		result := getAdditionalCapabilities([]string{}, true)
		Expect(result).To(ContainElement(configv1.ClusterVersionCapability("baremetal")))
		Expect(result).To(ContainElement(configv1.ClusterVersionCapability("Console")))
		Expect(result).To(ContainElement(configv1.ClusterVersionCapability("Insights")))
	})

	It("filters MachineAPI for baremetal platforms", func() {
		result := getAdditionalCapabilities([]string{"MachineAPI", "Storage"}, true)
		Expect(result).NotTo(ContainElement(configv1.ClusterVersionCapability("MachineAPI")))
		Expect(result).To(ContainElement(configv1.ClusterVersionCapability("Storage")))
	})

	It("includes MachineAPI for non-baremetal platforms", func() {
		result := getAdditionalCapabilities([]string{"MachineAPI", "Storage"}, false)
		Expect(result).To(ContainElement(configv1.ClusterVersionCapability("MachineAPI")))
		Expect(result).To(ContainElement(configv1.ClusterVersionCapability("Storage")))
	})

	It("handles case-insensitive MachineAPI", func() {
		result := getAdditionalCapabilities([]string{"machineapi", "Storage"}, true)
		Expect(result).NotTo(ContainElement(configv1.ClusterVersionCapability("machineapi")))
		Expect(result).To(ContainElement(configv1.ClusterVersionCapability("Storage")))
	})
})

var _ = Describe("isBaremetalPlatform", func() {
	It("returns true for BareMetalPlatformType", func() {
		aci := &hiveext.AgentClusterInstall{
			Spec: hiveext.AgentClusterInstallSpec{
				PlatformType: hiveext.BareMetalPlatformType,
			},
		}
		Expect(isBaremetalPlatform(aci)).To(BeTrue())
	})

	It("returns false for VSpherePlatformType", func() {
		aci := &hiveext.AgentClusterInstall{
			Spec: hiveext.AgentClusterInstallSpec{
				PlatformType: hiveext.VSpherePlatformType,
			},
		}
		Expect(isBaremetalPlatform(aci)).To(BeFalse())
	})

	It("returns false for empty platform type", func() {
		aci := &hiveext.AgentClusterInstall{
			Spec: hiveext.AgentClusterInstallSpec{},
		}
		Expect(isBaremetalPlatform(aci)).To(BeFalse())
	})
})
