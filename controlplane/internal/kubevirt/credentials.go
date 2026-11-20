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

package kubevirt

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	InfraCredentialsCMName = "kubevirt-infra-credentials-manifests"
)

type proxyEndpoint struct {
	CIDR string
	Port int
}

// EnsureInfraCredentialsManifests creates a ConfigMap containing the Secret manifests
// that provide the infra cluster kubeconfig to the CCM and CSI operators on the tenant
// cluster. These manifests are injected during installation via AgentClusterInstall's
// ManifestsConfigMapRefs API.
//
// NOTE: The kubeconfig data is embedded in Secret manifest YAML stored in a ConfigMap.
// This is a constraint of the assisted-service manifest injection API, which only
// supports ConfigMap-based references (ManifestsConfigMapRefs). The source Secret
// already exists in the same namespace with equivalent access scope. CAPK creates
// this Secret with a narrowly-scoped ServiceAccount token.
func EnsureInfraCredentialsManifests(
	ctx context.Context,
	localClient client.Client,
	infraClient client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	infraNamespace string,
) error {
	needsCredentials := (oacp.Spec.Config.CSIDriver != nil && oacp.Spec.Config.CSIDriver.Enabled) ||
		(oacp.Spec.Config.CloudControllerManager != nil && oacp.Spec.Config.CloudControllerManager.Enabled)
	if !needsCredentials {
		return nil
	}

	kubeconfigData, err := infraKubeconfigData(ctx, localClient, oacp)
	if err != nil {
		return err
	}

	if IsPodNetworking(oacp.Spec.Config.APIVIPs, oacp.Spec.Config.IngressVIPs) {
		internalServer, err := getInfraAPIServer(ctx, infraClient)
		if err != nil {
			return fmt.Errorf("failed to resolve infra API server: %w", err)
		}
		kubeconfigData, err = rewriteKubeconfigForInternalAPI(kubeconfigData, internalServer)
		if err != nil {
			return fmt.Errorf("failed to rewrite infra kubeconfig for internal API: %w", err)
		}
	}

	var manifests []ManifestEntry

	// CCM credentials secret (if CCM enabled)
	if oacp.Spec.Config.CloudControllerManager != nil && oacp.Spec.Config.CloudControllerManager.Enabled {
		manifests = append(manifests, ManifestEntry{
			Filename: "01-ccm-credentials-secret.yaml",
			Content: fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: openshift-cloud-controller-manager
type: Opaque
stringData:
  kubeconfig: |
%s
`, ccmCredSecretName, indentMultiline(kubeconfigData)),
		})
	}

	// CSI credentials secret (if CSI enabled)
	if oacp.Spec.Config.CSIDriver != nil && oacp.Spec.Config.CSIDriver.Enabled {
		manifests = append(manifests, ManifestEntry{
			Filename: "02-csi-credentials-secret.yaml",
			Content: fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: openshift-cluster-csi-drivers
type: Opaque
stringData:
  infraClusterNamespace: %s
  kubeconfig: |
%s
`, csiCredSecretName, infraNamespace, indentMultiline(kubeconfigData)),
		})
	}

	if len(manifests) == 0 {
		// Even when CCM/CSI are not enabled, if InfraClusterCredentials is set,
		// create an empty ConfigMap so that ACI's manifestsConfigMapRefs doesn't
		// fail with "ConfigMap not found".
		return ensureManifestsConfigMap(ctx, localClient, oacp, InfraCredentialsCMName, []ManifestEntry{
			{Filename: "placeholder.yaml", Content: "# No infra credentials manifests needed\n"},
		})
	}

	return ensureManifestsConfigMap(ctx, localClient, oacp, InfraCredentialsCMName, manifests)
}

func infraKubeconfigData(
	ctx context.Context,
	c client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
) (string, error) {
	credSecretName := "infra-cluster-credentials"
	if oacp.Spec.Config.InfraClusterRef != nil {
		credSecretName = oacp.Spec.Config.InfraClusterRef.Name
	}

	sourceSecret := &corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{
		Name:      credSecretName,
		Namespace: oacp.Namespace,
	}, sourceSecret); err != nil {
		return "", fmt.Errorf("failed to get infra credentials secret %s/%s: %w",
			oacp.Namespace, credSecretName, err)
	}

	kubeconfigData := string(sourceSecret.Data["kubeconfig"])
	if kubeconfigData == "" {
		return "", fmt.Errorf("infra credentials secret %s does not contain 'kubeconfig' key", credSecretName)
	}
	return kubeconfigData, nil
}

func infraProxyEndpoints(
	ctx context.Context,
	c client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
) ([]proxyEndpoint, error) {
	kubeconfigData, err := infraKubeconfigData(ctx, c, oacp)
	if err != nil {
		return nil, err
	}
	config, err := clientcmd.Load([]byte(kubeconfigData))
	if err != nil {
		return nil, fmt.Errorf("failed to parse infra kubeconfig: %w", err)
	}

	proxyURL := ""
	for _, cluster := range config.Clusters {
		if cluster.ProxyURL != "" {
			proxyURL = cluster.ProxyURL
			break
		}
	}
	if proxyURL == "" {
		return nil, nil
	}

	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse infra proxy URL %q: %w", proxyURL, err)
	}
	host := parsed.Hostname()
	if host == "" {
		return nil, fmt.Errorf("infra proxy URL %q has no host", proxyURL)
	}

	port := 3128
	if parsed.Port() != "" {
		port, err = strconv.Atoi(parsed.Port())
		if err != nil || port <= 0 {
			return nil, fmt.Errorf("infra proxy URL %q has an invalid port", proxyURL)
		}
	} else if parsed.Scheme == "https" {
		port = 443
	} else if parsed.Scheme == "http" {
		port = 80
	}

	if ip := net.ParseIP(host); ip != nil {
		return []proxyEndpoint{{CIDR: ipCIDR(ip), Port: port}}, nil
	}

	addresses, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve infra proxy host %q: %w", host, err)
	}
	endpoints := make([]proxyEndpoint, 0, len(addresses))
	for _, address := range addresses {
		if ip := net.ParseIP(address); ip != nil {
			endpoints = append(endpoints, proxyEndpoint{CIDR: ipCIDR(ip), Port: port})
		}
	}
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("infra proxy host %q resolved to no IP addresses", host)
	}
	return endpoints, nil
}

func ipCIDR(ip net.IP) string {
	if ip.To4() != nil {
		return ip.String() + "/32"
	}
	return ip.String() + "/128"
}

func getInfraAPIServer(ctx context.Context, infraClient client.Client) (string, error) {
	apiService := &corev1.Service{}
	if err := infraClient.Get(ctx, client.ObjectKey{Name: "kubernetes", Namespace: "default"}, apiService); err != nil {
		return "", fmt.Errorf("failed to get infra Kubernetes Service: %w", err)
	}
	if apiService.Spec.ClusterIP == "" || len(apiService.Spec.Ports) == 0 {
		return "", fmt.Errorf("infra Kubernetes Service has no ClusterIP or ports")
	}

	port := apiService.Spec.Ports[0].Port
	for _, servicePort := range apiService.Spec.Ports {
		if servicePort.Name == "https" {
			port = servicePort.Port
			break
		}
	}

	return "https://" + net.JoinHostPort(apiService.Spec.ClusterIP, strconv.Itoa(int(port))), nil
}

func rewriteKubeconfigForInternalAPI(kubeconfigData, internalServer string) (string, error) {
	config, err := clientcmd.Load([]byte(kubeconfigData))
	if err != nil {
		return "", err
	}
	if len(config.Clusters) == 0 {
		return "", fmt.Errorf("kubeconfig has no clusters")
	}

	for _, cluster := range config.Clusters {
		if cluster.Server == "" {
			return "", fmt.Errorf("kubeconfig cluster has no server")
		}
		serverURL, err := url.Parse(cluster.Server)
		if err != nil {
			return "", fmt.Errorf("failed to parse kubeconfig server %q: %w", cluster.Server, err)
		}
		if serverURL.Hostname() == "" {
			return "", fmt.Errorf("kubeconfig server %q has no hostname", cluster.Server)
		}
		if cluster.TLSServerName == "" {
			cluster.TLSServerName = serverURL.Hostname()
		}
		cluster.Server = internalServer
	}

	rewritten, err := clientcmd.Write(*config)
	if err != nil {
		return "", err
	}
	return string(rewritten), nil
}
