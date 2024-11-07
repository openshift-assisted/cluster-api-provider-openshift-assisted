package ignition

import (
	"encoding/json"

	config_types "github.com/coreos/ignition/v2/config/v3_1/types"
)

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
	var units []config_types.Unit
	units = append(units, getConfigdriveMetadataSystemdUnit())
	units = append(units, getKubeletCustomLabelsSystemdUnit())
	return units
}

func GetIgnitionConfigOverrides(files ...config_types.File) (string, error) {
	configdriveMetadataEnv := CreateIgnitionFile("/usr/local/bin/configdrive_metadata",
		"root", "data:text/plain;charset=utf-8;base64,IyEvYmluL2Jhc2gKCmVudl9maWxlPS9ldGMvbWV0YWRhdGFfZW52CmNvbmZpZ19kaXI9JChta3RlbXAgLWQpCnN1ZG8gbW91bnQgLUwgY29uZmlnLTIgJGNvbmZpZ19kaXIKY2F0ICRjb25maWdfZGlyL29wZW5zdGFjay9sYXRlc3QvbWV0YV9kYXRhLmpzb24gfCBqcSAtciAnLiB8IGtleXNbXScgfCB3aGlsZSByZWFkIGtleTsgZG8gdmFsdWU9JChqcSAtciAiLltcIiRrZXlcIl0iICRjb25maWdfZGlyL29wZW5zdGFjay9sYXRlc3QvbWV0YV9kYXRhLmpzb24pOyBlY2hvICJNRVRBREFUQV8kKGVjaG8gJHtrZXl9IHwgdHIgYS16IEEtWiB8IHRyIC0gXyk9JHt2YWx1ZX0iOyBkb25lIHwgc29ydCB8IHVuaXEgfCBzdWRvIHRlZSAkZW52X2ZpbGUK", 493, true)
	files = append(files, configdriveMetadataEnv)
	units := getSystemdUnits()
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
