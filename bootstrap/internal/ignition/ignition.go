package ignition

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/coreos/ignition/v2/config/v3_1"
	config_types "github.com/coreos/ignition/v2/config/v3_1/types"
	"github.com/go-logr/logr"
	logutil "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util/log"
)

const expectedIgnitionVersion = "3.1.0"

type ignitionVersionInfo struct {
	Ignition struct {
		Version string `json:"version"`
	} `json:"ignition"`
}

// configdriveMetadataScript reads metadata from the config-2 drive and writes
// environment variables to /etc/metadata_env in the format METADATA_<KEY>=<value>.
const configdriveMetadataScript = `#!/bin/bash

env_file=/etc/metadata_env
config_dir=$(mktemp -d)
sudo mount -L config-2 $config_dir
cat $config_dir/openstack/latest/meta_data.json | jq -r '. | keys[]' | while read key; do value=$(jq -r ".[\"$key\"]" $config_dir/openstack/latest/meta_data.json); echo "METADATA_$(echo ${key} | tr a-z A-Z | tr - _)=${value}"; done | sort | uniq | sudo tee $env_file
`

func getConfigdriveMetadataFile() config_types.File {
	return CreateIgnitionFile("/usr/local/bin/configdrive_metadata",
		"root", "data:text/plain;charset=utf-8;base64,"+base64Encode(configdriveMetadataScript), 493, true)
}

func getConfigdriveMetadataSystemdUnit() config_types.Unit {
	contents := `[Unit]
Description=Configdrive Metadata
Before=kubelet-customlabels.service
After=ostree-finalize-staged.service

[Service]
Type=oneshot

ExecStart=/usr/local/bin/configdrive_metadata
[Install]
WantedBy=multi-user.target
`
	enabled := true
	return config_types.Unit{
		Contents: &contents,
		Enabled:  &enabled,
		Name:     "configdrive-metadata.service",
	}
}

func getKubeletCustomLabelsSystemdUnit() config_types.Unit {
	contents := `[Unit]
Description=Kubelet Custom Labels
Before=kubelet.service
After=ostree-finalize-staged.service

[Service]
Type=oneshot
EnvironmentFile=/etc/metadata_env

ExecStart=/usr/local/bin/kubelet_custom_labels
[Install]
WantedBy=multi-user.target
`
	enabled := true
	return config_types.Unit{
		Contents: &contents,
		Enabled:  &enabled,
		Name:     "kubelet-customlabels.service",
	}

}

func getSystemdUnits() []config_types.Unit {
	units := make([]config_types.Unit, 0, 2)
	units = append(units, getConfigdriveMetadataSystemdUnit())
	units = append(units, getKubeletCustomLabelsSystemdUnit())
	return units
}

// IgnitionOptions contains optional components to include in the ignition config.
type IgnitionOptions struct {
	// NodeNameEnvVar is the environment variable reference (e.g., "$METADATA_HOSTNAME")
	// to use for setting the hostname. If empty, no hostname unit is added.
	NodeNameEnvVar string
}

func GetIgnitionConfigOverrides(opts IgnitionOptions, files ...config_types.File) (string, error) {
	files = append(files, getConfigdriveMetadataFile())
	units := getSystemdUnits()

	// Add hostname unit if NodeNameEnvVar is specified
	if opts.NodeNameEnvVar != "" {
		hostnameUnit, hostnameFile := getSetHostnameUnit(opts.NodeNameEnvVar)
		units = append(units, hostnameUnit)
		files = append(files, hostnameFile)
	}

	config := config_types.Config{
		Ignition: config_types.Ignition{
			Version: "3.1.0",
		},
		Storage: config_types.Storage{
			Files: files,
		},
	}
	if len(units) > 0 {
		config.Systemd.Units = units
	}

	ignition, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return string(ignition), nil
}

// getSetHostnameUnit creates a systemd unit and script for setting hostname from an env var.
// envVarRef can be in the form "$METADATA_NAME" or "${METADATA_NAME}".
func getSetHostnameUnit(envVarRef string) (config_types.Unit, config_types.File) {
	// Extract the variable name, handling both $VAR and ${VAR} formats
	varName := strings.TrimPrefix(envVarRef, "$")
	varName = strings.TrimPrefix(varName, "{")
	varName = strings.TrimSuffix(varName, "}")

	scriptContent := `#!/bin/bash
# Safely resolve node name from metadata environment
ENV_VAR_NAME="` + varName + `"
if [ -f /etc/metadata_env ]; then
    # Use grep to find the line, cut to extract value - no shell expansion
    NODE_NAME=$(/usr/bin/grep "^${ENV_VAR_NAME}=" /etc/metadata_env | /usr/bin/cut -d'=' -f2-)
fi
if [ -n "$NODE_NAME" ]; then
    # Use hostnamectl to set both transient and static hostname
    /usr/bin/hostnamectl set-hostname "$NODE_NAME"
fi
`

	unitContents := `[Unit]
Description=Set Hostname from Metadata
Before=kubelet.service
After=configdrive-metadata.service
Wants=configdrive-metadata.service

[Service]
Type=oneshot
RemainAfterExit=true
ExecStart=/bin/bash /usr/local/bin/set_hostname

[Install]
WantedBy=multi-user.target
`
	enabled := true
	unit := config_types.Unit{
		Contents: &unitContents,
		Enabled:  &enabled,
		Name:     "set-hostname.service",
	}

	file := CreateIgnitionFile("/usr/local/bin/set_hostname",
		"root", "data:text/plain;charset=utf-8;base64,"+base64Encode(scriptContent), 493, true)

	return unit, file
}

func base64Encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// MergeIgnitionConfig merges additional units and files into an existing ignition config.
// This is used to add the set-hostname unit to the ignition config from Assisted Installer
// for the installed OS (reboot phase).
func MergeIgnitionConfig(log logr.Logger, baseIgnition []byte, opts IgnitionOptions) ([]byte, error) {
	if opts.NodeNameEnvVar == "" {
		return baseIgnition, nil
	}

	var versionInfo ignitionVersionInfo
	if err := json.Unmarshal(baseIgnition, &versionInfo); err == nil {
		if versionInfo.Ignition.Version != expectedIgnitionVersion {
			log.V(logutil.WarningLevel).Info(
				"base ignition config has different version than expected, fields may be silently dropped",
				"expectedVersion", expectedIgnitionVersion,
				"actualVersion", versionInfo.Ignition.Version,
			)
		}
	}

	var config config_types.Config
	if err := json.Unmarshal(baseIgnition, &config); err != nil {
		return nil, err
	}

	// Add configdrive-metadata service and script (required to populate /etc/metadata_env)
	config.Storage.Files = append(config.Storage.Files, getConfigdriveMetadataFile())
	config.Systemd.Units = append(config.Systemd.Units, getConfigdriveMetadataSystemdUnit())

	// Add set-hostname service and script
	hostnameUnit, hostnameFile := getSetHostnameUnit(opts.NodeNameEnvVar)
	config.Systemd.Units = append(config.Systemd.Units, hostnameUnit)
	config.Storage.Files = append(config.Storage.Files, hostnameFile)

	return json.Marshal(config)
}

func CreateIgnitionFile(path, user, content string, mode int, overwrite bool) config_types.File {
	return config_types.File{
		Node: config_types.Node{
			Path:      path,
			Overwrite: &overwrite,
			User:      config_types.NodeUser{Name: &user},
		},
		FileEmbedded1: config_types.FileEmbedded1{
			Append: []config_types.Resource{},
			Contents: config_types.Resource{
				Source: &content,
			},
			Mode: &mode,
		},
	}
}

// MergeIgnitionConfigStrings merges overrideIgnition into baseIgnition.
// Both arguments must be valid Ignition config JSON (e.g. v3.1.0). The result is base plus
// override merged: the override appends or overrides fields per Ignition merge semantics.
// Returns the merged config as a JSON string. If overrideIgnition is empty, baseIgnition
// is returned unchanged.
func MergeIgnitionConfigStrings(baseIgnition, overrideIgnition string) (string, error) {
	if overrideIgnition == "" {
		return baseIgnition, nil
	}
	baseConfig, _, err := v3_1.ParseCompatibleVersion([]byte(baseIgnition))
	if err != nil {
		return "", err
	}
	overrideConfig, _, err := v3_1.ParseCompatibleVersion([]byte(overrideIgnition))
	if err != nil {
		return "", err
	}
	merged := v3_1.Merge(baseConfig, overrideConfig)
	out, err := json.Marshal(merged)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
