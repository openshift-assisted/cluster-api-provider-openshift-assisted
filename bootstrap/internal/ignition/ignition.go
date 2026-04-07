package ignition

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/coreos/ignition/v2/config/v3_1"
	config_types "github.com/coreos/ignition/v2/config/v3_1/types"
	"github.com/go-logr/logr"
	logutil "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util/log"
)

const expectedIgnitionVersion = "3.1.0"
const defaultSentinelDirectory = "/var/lib/capoa"

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

	// PreBootstrapCommands are shell commands to run before kubelet.service on first boot.
	PreBootstrapCommands []string

	// PostBootstrapCommands are shell commands to run after kubelet.service on first boot.
	PostBootstrapCommands []string

	// SentinelDirectory is the persistent directory used for run-once sentinel files.
	// If empty, /var/lib/capoa is used.
	SentinelDirectory string

	// KubeconfigPath is the kubeconfig file path for post-bootstrap readiness checks.
	// If empty, /var/lib/kubelet/kubeconfig is used.
	KubeconfigPath string

	// ProviderID is the kubelet provider ID value (e.g., "openstack://$METADATA_UUID").
	// If set, a kubelet systemd drop-in will be added to inject the --provider-id flag.
	ProviderID string
}

func sentinelDirectory(dir string) string {
	if dir == "" {
		return defaultSentinelDirectory
	}
	return strings.TrimRight(dir, "/")
}

func kubeconfigPath(path string) string {
	if path == "" {
		return "/var/lib/kubelet/kubeconfig"
	}
	return path
}

func getBootstrapCommandUnit(name, scriptPath, sentinelPath, ordering string, commands []string) (config_types.Unit, config_types.File) {
	var sb strings.Builder
	sb.WriteString("#!/bin/bash\nset -euo pipefail\n")
	for _, cmd := range commands {
		sb.WriteString(cmd)
		sb.WriteString("\n")
	}
	sentinelDir := filepath.Dir(sentinelPath)
	sb.WriteString("mkdir -p " + sentinelDir + "\ntouch " + sentinelPath + "\n")

	unitContents := `[Unit]
Description=CAPOA ` + name + `
` + ordering + `
ConditionPathExists=!` + sentinelPath + `

[Service]
Type=oneshot
ExecStart=` + scriptPath + `

[Install]
WantedBy=multi-user.target
`
	enabled := true
	unit := config_types.Unit{
		Contents: &unitContents,
		Enabled:  &enabled,
		Name:     name + ".service",
	}

	file := CreateIgnitionFile(scriptPath,
		"root", "data:text/plain;charset=utf-8;base64,"+base64Encode(sb.String()), 0700, true)

	return unit, file
}

// getPreBootstrapUnit creates a systemd oneshot unit that runs before kubelet.service.
// The After= directive lists all CAPOA units that may exist; systemd silently ignores
// ordering against absent units, so this is safe and ensures correct sequencing when
// those units are present.
func getPreBootstrapUnit(commands []string, dir string) (config_types.Unit, config_types.File) {
	sentinelPath := sentinelDirectory(dir) + "/pre-bootstrap.done"
	return getBootstrapCommandUnit(
		"capoa-pre-bootstrap",
		"/usr/local/bin/capoa-pre-bootstrap.sh",
		sentinelPath,
		"After=network.target ostree-finalize-staged.service configdrive-metadata.service kubelet-customlabels.service set-hostname.service\nBefore=kubelet.service",
		commands,
	)
}

func getPostBootstrapUnit(commands []string, dir string, kubeconfPath string) (config_types.Unit, config_types.File) {
	sentinelPath := sentinelDirectory(dir) + "/post-bootstrap.done"
	scriptPath := "/usr/local/bin/capoa-post-bootstrap.sh"
	name := "capoa-post-bootstrap"

	// Build script with kubectl API wait before user commands
	var sb strings.Builder
	sb.WriteString("#!/bin/bash\nset -euo pipefail\n\n")

	// Resolve kubeconfig path (use default if empty)
	resolvedKubeconfigPath := kubeconfigPath(kubeconfPath)

	// Wait for kube API to be accessible (max 10 minutes)
	// This polling validates both API availability and certificate validity.
	// If the kubelet client certificate is missing or invalid, kubectl commands will fail.
	fmt.Fprintf(&sb, `# Wait for kube API to be accessible (max 10 minutes)
# This polling validates both API availability and certificate validity.
# If the kubelet client certificate is missing or invalid, kubectl commands will fail.
KUBECONFIG_PATH="%s"
MAX_WAIT=600
INTERVAL=10
ELAPSED=0

echo "Waiting for kube API accessibility via ${KUBECONFIG_PATH}..."
while ! kubectl --kubeconfig="${KUBECONFIG_PATH}" get --raw /readyz >/dev/null 2>&1; do
    if [ ${ELAPSED} -ge ${MAX_WAIT} ]; then
        echo "ERROR: Timeout waiting for kube API after ${MAX_WAIT}s"
        exit 1
    fi
    echo "Still waiting for kube API to be accessible... (${ELAPSED}s elapsed)"
    sleep ${INTERVAL}
    ELAPSED=$((ELAPSED + INTERVAL))
done
echo "Kube API accessible after ${ELAPSED}s"

# Wait for node to join cluster (shares same timeout budget)
echo "Waiting for node $(hostname) to join cluster..."
while ! kubectl --kubeconfig="${KUBECONFIG_PATH}" get node "$(hostname)" >/dev/null 2>&1; do
    if [ ${ELAPSED} -ge ${MAX_WAIT} ]; then
        echo "ERROR: Timeout waiting for node $(hostname) to join cluster after ${MAX_WAIT}s"
        exit 1
    fi
    echo "Still waiting for node to be registered in cluster... (${ELAPSED}s elapsed)"
    sleep ${INTERVAL}
    ELAPSED=$((ELAPSED + INTERVAL))
done
echo "Node registered in cluster after ${ELAPSED}s"

`, resolvedKubeconfigPath)

	// Add user commands
	sb.WriteString("# Execute user-provided post-bootstrap commands\n")
	for _, cmd := range commands {
		sb.WriteString(cmd)
		sb.WriteString("\n")
	}

	// Touch sentinel
	sentinelDir := filepath.Dir(sentinelPath)
	sb.WriteString("mkdir -p " + sentinelDir + "\ntouch " + sentinelPath + "\n")

	unitContents := `[Unit]
Description=CAPOA ` + name + `
Requires=kubelet.service
After=kubelet.service
ConditionPathExists=!` + sentinelPath + `

[Service]
Type=oneshot
ExecStart=` + scriptPath + `

[Install]
WantedBy=multi-user.target
`
	enabled := true
	unit := config_types.Unit{
		Contents: &unitContents,
		Enabled:  &enabled,
		Name:     name + ".service",
	}

	file := CreateIgnitionFile(scriptPath,
		"root", "data:text/plain;charset=utf-8;base64,"+base64Encode(sb.String()), 0700, true)

	return unit, file
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

	if len(opts.PreBootstrapCommands) > 0 {
		unit, file := getPreBootstrapUnit(opts.PreBootstrapCommands, opts.SentinelDirectory)
		units = append(units, unit)
		files = append(files, file)
	}

	if len(opts.PostBootstrapCommands) > 0 {
		unit, file := getPostBootstrapUnit(opts.PostBootstrapCommands, opts.SentinelDirectory, opts.KubeconfigPath)
		units = append(units, unit)
		files = append(files, file)
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
// This is used to add the set-hostname unit, pre/post bootstrap commands, etc. to the
// ignition config from Assisted Installer for the installed OS (reboot phase).
func MergeIgnitionConfig(log logr.Logger, baseIgnition []byte, opts IgnitionOptions) ([]byte, error) {
	hasHostname := opts.NodeNameEnvVar != ""
	hasPre := len(opts.PreBootstrapCommands) > 0
	hasPost := len(opts.PostBootstrapCommands) > 0
	hasProviderID := opts.ProviderID != ""

	if !hasHostname && !hasPre && !hasPost && !hasProviderID {
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

	if hasHostname {
		config.Storage.Files = append(config.Storage.Files, getConfigdriveMetadataFile())
		config.Systemd.Units = append(config.Systemd.Units, getConfigdriveMetadataSystemdUnit())

		hostnameUnit, hostnameFile := getSetHostnameUnit(opts.NodeNameEnvVar)
		config.Systemd.Units = append(config.Systemd.Units, hostnameUnit)
		config.Storage.Files = append(config.Storage.Files, hostnameFile)
	}

	if hasPre {
		unit, file := getPreBootstrapUnit(opts.PreBootstrapCommands, opts.SentinelDirectory)
		config.Systemd.Units = append(config.Systemd.Units, unit)
		config.Storage.Files = append(config.Storage.Files, file)
	}

	if hasPost {
		unit, file := getPostBootstrapUnit(opts.PostBootstrapCommands, opts.SentinelDirectory, opts.KubeconfigPath)
		config.Systemd.Units = append(config.Systemd.Units, unit)
		config.Storage.Files = append(config.Storage.Files, file)
	}

	if hasProviderID {
		config.Storage.Files = append(config.Storage.Files, GetKubeletProviderIDDropin())
	}

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

// GetKubeletProviderIDDropin returns a systemd drop-in file that overrides kubelet.service
// ExecStart to include the --provider-id flag. The value is read from /run/kubelet-provider-id
// which is populated by the kubelet_custom_labels script during boot.
//
// This drop-in is based on the OpenShift kubelet.service template:
// https://github.com/openshift/machine-config-operator/blob/main/templates/master/01-master-kubelet/on-prem/units/kubelet.service.yaml
func GetKubeletProviderIDDropin() config_types.File {
	dropinContent := `[Service]
ExecStart=
ExecStart=/usr/bin/kubelet \
  --config=/etc/kubernetes/kubelet.conf \
  --bootstrap-kubeconfig=/etc/kubernetes/kubeconfig \
  --kubeconfig=/var/lib/kubelet/kubeconfig \
  --container-runtime-endpoint=unix:///var/run/crio/crio.sock \
  --runtime-cgroups=/system.slice/crio.service \
  --node-labels=node-role.kubernetes.io/worker \
  --node-ip=${KUBELET_NODE_IP} \
  --minimum-container-ttl-duration=6m0s \
  --cloud-provider=external \
  --volume-plugin-dir=/etc/kubernetes/kubelet-plugins/volume/exec \
  --provider-id=$(cat /run/kubelet-provider-id)
`

	return CreateIgnitionFile(
		"/etc/systemd/system/kubelet.service.d/10-provider-id.conf",
		"root",
		"data:text/plain;charset=utf-8;base64,"+base64Encode(dropinContent),
		0644,
		true,
	)
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
