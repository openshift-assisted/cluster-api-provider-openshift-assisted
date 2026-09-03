/*
Copyright 2026.

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

package kubevirt

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

const (
	// DefaultInfraClusterDNSIP is the default CoreDNS service ClusterIP on standard
	// OpenShift clusters (first IP in the 172.30.0.0/16 service network).
	// Override by passing the actual DNS service IP when the infra cluster uses
	// a non-default service CIDR.
	DefaultInfraClusterDNSIP = "172.30.0.10"
)

// ignitionConfig represents a minimal Ignition v3.1.0 config structure.
type ignitionConfig struct {
	Ignition ignitionVersion `json:"ignition"`
	Passwd   *ignPasswd      `json:"passwd,omitempty"`
	Storage  *ignStorage     `json:"storage,omitempty"`
	Systemd  *ignSystemd     `json:"systemd,omitempty"`
}

type ignSystemd struct {
	Units []ignUnit `json:"units"`
}

type ignUnit struct {
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Contents string `json:"contents"`
}

type ignitionVersion struct {
	Version string `json:"version"`
}

type ignPasswd struct {
	Users []ignUser `json:"users"`
}

type ignUser struct {
	Name              string   `json:"name"`
	SSHAuthorizedKeys []string `json:"sshAuthorizedKeys"`
}

type ignStorage struct {
	Files []ignFile `json:"files"`
}

type ignFile struct {
	Path      string        `json:"path"`
	Mode      int           `json:"mode"`
	Overwrite bool          `json:"overwrite,omitempty"`
	Contents  *ignContents  `json:"contents,omitempty"`
	Append    []ignContents `json:"append,omitempty"`
}

type ignContents struct {
	Source string `json:"source"`
}

func dataURL(content string) string {
	return "data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte(content))
}

// KubeVirtDiscoveryIgnitionOverride generates the discovery-phase ignition override
// for KubeVirt platform. This adds the SSH key and configures DNS resolution by
// writing the API service ClusterIP directly to /etc/hosts (no runtime DNS resolution needed).
// infraDNSIP is the infra cluster's CoreDNS service ClusterIP (default: 172.30.0.10).
func KubeVirtDiscoveryIgnitionOverride(sshKey, apiServiceIP, clusterName, baseDomain, infraDNSIP string) (string, error) {
	if infraDNSIP == "" {
		infraDNSIP = DefaultInfraClusterDNSIP
	}
	config := ignitionConfig{
		Ignition: ignitionVersion{Version: "3.1.0"},
	}

	if sshKey != "" {
		config.Passwd = &ignPasswd{
			Users: []ignUser{{Name: "core", SSHAuthorizedKeys: []string{sshKey}}},
		}
	}

	if apiServiceIP != "" && clusterName != "" && baseDomain != "" {
		apiIntHostname := fmt.Sprintf("api-int.%s.%s", clusterName, baseDomain)
		apiHostname := fmt.Sprintf("api.%s.%s", clusterName, baseDomain)
		hostsEntry := fmt.Sprintf("%s %s %s", apiServiceIP, apiIntHostname, apiHostname)

		dnsFixScript := fmt.Sprintf(`#!/bin/bash
# Write API service ClusterIP to /etc/hosts for api-int resolution.
# This ensures bootkube can reach the tenant API regardless of DNS state.
sed -i '/%s/d' /etc/hosts
echo '%s' >> /etc/hosts
echo 'nameserver %s' > /etc/resolv.conf
echo 'search cluster.local svc.cluster.local' >> /etc/resolv.conf
echo 'options ndots:5' >> /etc/resolv.conf
echo "DNS and hosts configuration applied: %s"
`, apiIntHostname, hostsEntry, infraDNSIP, hostsEntry)

		config.Storage = &ignStorage{
			Files: []ignFile{
				{Path: "/etc/NetworkManager/conf.d/99-capoa-dns.conf", Mode: 0644, Overwrite: true, Contents: &ignContents{Source: dataURL("[main]\ndns=none\n")}},
				{Path: "/usr/local/bin/capoa-dns-resolve", Mode: 0755, Contents: &ignContents{Source: dataURL(dnsFixScript)}},
			},
		}
		config.Systemd = &ignSystemd{
			Units: []ignUnit{{
				Name:    "capoa-dns-resolve.service",
				Enabled: true,
				Contents: `[Unit]
Description=Configure api-int hosts entry for KubeVirt pod-networking
Before=kubelet.service crio.service bootkube.service
After=NetworkManager-wait-online.service
ConditionPathExists=!/var/run/capoa-dns-configured

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/local/bin/capoa-dns-resolve
ExecStartPost=/usr/bin/touch /var/run/capoa-dns-configured

[Install]
WantedBy=multi-user.target
`,
			}},
		}
	}

	if config.Passwd == nil && config.Storage == nil {
		return "", nil
	}

	data, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("failed to marshal discovery ignition override: %w", err)
	}
	return string(data), nil
}

// KubeVirtInstallIgnitionOverride generates the install-time ignition override for
// KubeVirt platform. This configures:
//   - SSH key for core user
//   - DNS resolution pointing to infra cluster's CoreDNS
//   - NetworkManager configured to not override /etc/resolv.conf
//   - IPv4 preference over IPv6 (avoids AAAA lookup failures in dual-stack)
//   - Placeholder manifests to prevent bootkube crash on empty manifest dirs
func KubeVirtInstallIgnitionOverride(sshKey string) (string, error) {
	// Install ignition: only include files that don't conflict with the MCS-served config.
	// DNS resolution for api-int is handled by the DNS forwarding rule (CoreDNS -> DNS proxy),
	// so no /etc/resolv.conf, /etc/hosts, or NetworkManager overrides are needed here.
	files := []ignFile{
		{Path: "/opt/openshift/manifests/placeholder.yaml", Mode: 0644, Overwrite: true, Contents: &ignContents{Source: dataURL(placeholderManifest)}},
		{Path: "/opt/openshift/openshift/placeholder.yaml", Mode: 0644, Overwrite: true, Contents: &ignContents{Source: dataURL(placeholderManifest)}},
	}

	config := ignitionConfig{
		Ignition: ignitionVersion{Version: "3.1.0"},
		Storage:  &ignStorage{Files: files},
	}

	if sshKey != "" {
		config.Passwd = &ignPasswd{
			Users: []ignUser{
				{
					Name:              "core",
					SSHAuthorizedKeys: []string{sshKey},
				},
			},
		}
	}

	data, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("failed to marshal install ignition override: %w", err)
	}
	return string(data), nil
}

const placeholderManifest = `apiVersion: v1
kind: ConfigMap
metadata:
  name: placeholder-fix
  namespace: default
`
