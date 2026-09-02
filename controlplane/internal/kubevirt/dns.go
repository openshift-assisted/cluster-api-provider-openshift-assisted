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
	"fmt"
	"hash/fnv"
	"net"
	"strings"
)

const (
	DNSProxyConfigMapName  = "kubevirt-dns-proxy-manifests"
	TenantDNSFwdConfigName = "kubevirt-tenant-dns-fwd-manifests"
)

// GenerateDNSProxyManifests generates the manifests for the DNS proxy DaemonSet
// that runs on the infra cluster to resolve tenant cluster domains (api, api-int, *.apps).
// This is the zero-IP variant that creates a forward-only DNS proxy.
func GenerateDNSProxyManifests(
	clusterName string,
	baseDomain string,
	namespace string,
	infraClusterDNSServiceIP string,
) []ManifestEntry {
	return GenerateDNSProxyManifestsWithIPs(clusterName, baseDomain, namespace, infraClusterDNSServiceIP, nil, nil)
}

// GenerateDNSProxyManifestsWithIPs generates the manifests for the DNS proxy DaemonSet
// that runs on the infra cluster to resolve tenant cluster domains (api, api-int, *.apps).
//
// When apiIPs are provided (typically the API service ClusterIP), the DNS proxy resolves
// api and api-int to those IPs. This is critical for the MCS (Machine Config Server)
// during installation: non-bootstrap nodes resolve api-int to the service ClusterIP,
// connect on port 22623, and the service routes to the bootstrap node running MCS.
//
// Lessons learned from production debugging:
// - CoreDNS template plugin wildcard entries MUST use Go template syntax {{ .Name }}
//   (NOT {[.Name]} which produces malformed DNS responses that resolvers silently ignore)
// - The dns-proxy pods must use the infra cluster DNS service IP (typically 172.30.0.10)
//   as their upstream, not node IPs (which may be unreachable from pods in bridge mode)
// - api-int MUST resolve to the API service ClusterIP (not individual VM IPs) to ensure
//   MCS traffic reaches the bootstrap node during installation via Kubernetes service routing
func GenerateDNSProxyManifestsWithIPs(
	clusterName string,
	baseDomain string,
	namespace string,
	infraClusterDNSServiceIP string,
	apiIPs []string,
	ingressIPs []string,
) []ManifestEntry {
	fqdn := fmt.Sprintf("%s.%s", clusterName, baseDomain)

	manifests := make([]ManifestEntry, 0, 2)

	corefile := generateCorefile(fqdn, infraClusterDNSServiceIP, apiIPs, ingressIPs)
	// Always include the apps server block so CoreDNS loads the file plugin at
	// startup. When IngressIPs become available later, the file plugin's built-in
	// zone reload (triggered by SOA serial change) picks up new records without
	// requiring a pod restart.
	corefileContent := generateAppsCorefile(fqdn) + corefile
	appsDbContent := generateAppsZoneFile(fqdn, ingressIPs)

	cmData := fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: tenant-dns-corefile
  namespace: %s
data:
  Corefile: |
%s
  apps.db: |
%s
`, namespace, indentMultiline(corefileContent, 4), indentMultiline(appsDbContent, 4))

	manifests = append(manifests, ManifestEntry{
		Filename: "01-dns-proxy-configmap.yaml",
		Content:  cmData,
	})

	// DNS proxy DaemonSet
	manifests = append(manifests, ManifestEntry{
		Filename: "02-dns-proxy-daemonset.yaml",
		Content: fmt.Sprintf(`apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: tenant-dns-proxy
  namespace: %s
  labels:
    app: tenant-dns-proxy
spec:
  selector:
    matchLabels:
      app: tenant-dns-proxy
  template:
    metadata:
      labels:
        app: tenant-dns-proxy
    spec:
      hostNetwork: true
      dnsPolicy: ClusterFirstWithHostNet
      tolerations:
        - operator: Exists
      containers:
        - name: coredns
          image: %s
          args: ["-conf", "/etc/coredns/Corefile"]
          ports:
            - containerPort: 5353
              protocol: UDP
              hostPort: 5353
            - containerPort: 5353
              protocol: TCP
              hostPort: 5353
          volumeMounts:
            - name: config
              mountPath: /etc/coredns
              readOnly: true
          resources:
            requests:
              cpu: 10m
              memory: 32Mi
            limits:
              memory: 64Mi
      volumes:
        - name: config
          configMap:
            name: tenant-dns-corefile
`, namespace, GetCoreDNSImage("")),
	})

	return manifests
}

// generateCorefile generates a CoreDNS Corefile for the DNS proxy.
//
// For *.apps wildcard resolution, we use the CoreDNS "file" plugin with a wildcard
// zone file instead of the "template" plugin. The template plugin's Go template
// syntax ({{ .Name }}, {{ index .Match 1 }}) silently produces empty responses
// (NOERROR with 0 answers) in CoreDNS v1.11.x, making it unsuitable for dynamic
// wildcard resolution.
//
// The file plugin with a standard DNS wildcard record (*.apps) works reliably
// across all CoreDNS versions.
//
// apiIPs: IP addresses for api/api-int resolution
// ingressIPs: IP addresses for *.apps ingress resolution
func generateCorefile(fqdn string, upstreamDNS string, apiIPs []string, ingressIPs []string) string {
	escapedFQDN := strings.ReplaceAll(fqdn, ".", "[.]")

	var apiAnswers string
	for _, ip := range apiIPs {
		apiAnswers += fmt.Sprintf("        answer \"api.%s 60 IN %s %s\"\n", fqdn, dnsRecordType(ip), ip)
	}

	// If no IPs are known yet, use forward-only mode
	if len(apiIPs) == 0 && len(ingressIPs) == 0 {
		return fmt.Sprintf(`%s:5353 {
    errors
    log
    reload 10s

    template IN AAAA {
        match ".*"
        rcode NOERROR
    }

    template IN ANY {
        match ".*"
        rcode NOERROR
    }

    forward . %s
    cache 30
}
`, fqdn, upstreamDNS)
	}

	var apiIntAnswers string
	for _, ip := range apiIPs {
		apiIntAnswers += fmt.Sprintf("        answer \"api-int.%s 60 IN %s %s\"\n", fqdn, dnsRecordType(ip), ip)
	}

	// *.apps is handled by a separate server block using the file plugin (see generateAppsZoneFile).
	// The proxy is authoritative for the tenant zone - unknown subdomains get NXDOMAIN.
	// This is important for the assisted-service DNS wildcard validation: it queries a
	// nonsensical subdomain (e.g. zzzzz.<fqdn>) and expects NXDOMAIN. If we forwarded
	// to upstream DNS, the infra cluster's wildcard *.apps record would catch it and the
	// validation would fail, blocking installation.
	return fmt.Sprintf(`%s:5353 {
    errors
    log
    reload 10s

    template IN A api.%s {
        match "^api[.]%s[.]$"
%s        fallthrough
    }

    template IN A api-int.%s {
        match "^api-int[.]%s[.]$"
%s        fallthrough
    }

    template IN AAAA {
        match ".*"
        rcode NOERROR
    }

    template IN ANY {
        match ".*"
        rcode NOERROR
    }

    template IN A {
        match ".*"
        rcode NXDOMAIN
    }

    cache 30
}
`, fqdn, fqdn, escapedFQDN, apiAnswers, fqdn, escapedFQDN, apiIntAnswers)
}

// generateAppsCorefile generates a separate CoreDNS server block for the *.apps
// subdomain using the file plugin with a wildcard zone file.
func generateAppsCorefile(fqdn string) string {
	return fmt.Sprintf(`apps.%s:5353 {
    file /etc/coredns/apps.db
    reload 10s
    errors
    log
}
`, fqdn)
}

// generateAppsZoneFile generates a DNS zone file with wildcard A records for the
// *.apps subdomain. This is used by the CoreDNS file plugin.
// The SOA serial is derived from the IP list so that the file plugin detects
// zone changes and reloads automatically.
func generateAppsZoneFile(fqdn string, ingressIPs []string) string {
	var records string
	for _, ip := range ingressIPs {
		records += fmt.Sprintf("*  60   IN %s   %s\n", dnsRecordType(ip), ip)
	}
	serial := zoneSerial(ingressIPs)
	return fmt.Sprintf(`$ORIGIN apps.%s.
@  3600 IN SOA ns.tenant-dns.svc.cluster.local. admin.tenant-dns.svc.cluster.local. %d 3600 900 604800 30
@  3600 IN NS  ns.tenant-dns.svc.cluster.local.
%s`, fqdn, serial, records)
}

// zoneSerial returns a deterministic SOA serial derived from the given IPs.
// When IPs change the serial changes, triggering CoreDNS file plugin reload.
func zoneSerial(ips []string) uint32 {
	h := fnv.New32a()
	for _, ip := range ips {
		h.Write([]byte(ip))
	}
	s := h.Sum32()
	if s == 0 {
		return 1
	}
	return s
}

// GenerateTenantDNSForwarderManifests generates a day-0 manifest that configures
// the tenant cluster's DNS operator to forward queries for the tenant's own FQDN
// (clusterName.baseDomain) back to the infra cluster's DNS.
//
// The zone MUST be the full FQDN (e.g., "kubevirt-tenant.apps.example.com"),
// NOT just the baseDomain — otherwise the DNS operator won't match queries for
// api.kubevirt-tenant.apps.example.com or *.apps.kubevirt-tenant.apps.example.com.
//
// In KubeVirt deployments, the tenant VMs are pods on the infra cluster's network
// and can reach the infra cluster's DNS service ClusterIP (172.30.0.10). Forwarding
// to this IP works because the infra DNS operator has a forwarding rule (configured
// by EnsureDNSProxy) that routes tenant domain queries to the tenant-dns proxy.
//
// This ConfigMap MUST always be created (even before node IPs are known) because
// it is referenced in AgentClusterInstall.spec.manifestsConfigMapRefs. If the
// ConfigMap doesn't exist, AgentClusterInstall reports SpecSynced: False and
// the installation cannot proceed.
func GenerateTenantDNSForwarderManifests(fqdn string, dnsProxyNodeIPs []string) []ManifestEntry {
	if len(dnsProxyNodeIPs) == 0 {
		// Forward to the infra cluster's DNS service. This works for KubeVirt
		// because tenant VMs can reach infra ClusterIPs directly (they are pods
		// on the infra network). The infra DNS has a forwarding rule that routes
		// these queries to the tenant-dns proxy.
		return []ManifestEntry{
			{
				Filename: "99-tenant-dns-forwarder.yaml",
				Content: fmt.Sprintf(`apiVersion: operator.openshift.io/v1
kind: DNS
metadata:
  name: default
spec:
  servers:
    - name: infra-domain-forwarder
      zones:
        - "%s"
      forwardPlugin:
        upstreams:
          - "172.30.0.10:53"
        policy: Random
`, fqdn),
			},
		}
	}

	upstreams := ""
	for _, ip := range dnsProxyNodeIPs {
		upstreams += fmt.Sprintf("          - \"%s:5353\"\n", ip)
	}

	return []ManifestEntry{
		{
			Filename: "99-tenant-dns-forwarder.yaml",
			Content: fmt.Sprintf(`apiVersion: operator.openshift.io/v1
kind: DNS
metadata:
  name: default
spec:
  servers:
    - name: infra-domain-forwarder
      zones:
        - "%s"
      forwardPlugin:
        upstreams:
%s        policy: Random
`, fqdn, upstreams),
		},
	}
}

// dnsRecordType returns "A" for IPv4 addresses and "AAAA" for IPv6.
func dnsRecordType(ip string) string {
	if net.ParseIP(ip) != nil && net.ParseIP(ip).To4() == nil {
		return "AAAA"
	}
	return "A"
}
