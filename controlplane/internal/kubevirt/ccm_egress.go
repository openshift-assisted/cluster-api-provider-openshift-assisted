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
	"context"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"

	ctrl "sigs.k8s.io/controller-runtime"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CCMEgressDestinations contains the exact CIDRs the tenant CCM must reach.
type CCMEgressDestinations struct {
	InfraAPI      []string
	InfraAPIPorts []int32
	InfraAPIHost  string
	TenantAPI     []string
}

func (destinations CCMEgressDestinations) all() []string {
	return append(append([]string{}, destinations.InfraAPI...), destinations.TenantAPI...)
}

func (destinations CCMEgressDestinations) apiPorts() []int32 {
	ports := []int32{443, 6443}
	seen := map[int32]struct{}{443: {}, 6443: {}}
	for _, port := range destinations.InfraAPIPorts {
		if port <= 0 || port > 65535 {
			continue
		}
		if _, duplicate := seen[port]; duplicate {
			continue
		}
		seen[port] = struct{}{}
		ports = append(ports, port)
	}
	sort.Slice(ports, func(left, right int) bool { return ports[left] < ports[right] })
	return ports
}

func withTenantAPIBootstrapDestination(
	destinations CCMEgressDestinations,
	serviceIPs *ServiceIPs,
) CCMEgressDestinations {
	if len(destinations.TenantAPI) > 0 || serviceIPs == nil || serviceIPs.APIClusterIP == "" {
		return destinations
	}
	destinations.TenantAPI = []string{infraAPIHostCIDR(serviceIPs.APIClusterIP)}
	return destinations
}

// GetCCMEgressDestinations resolves API egress destinations without requiring
// the remote infra identity to read the default namespace. If that Service is
// not readable, the original kubeconfig API endpoint is resolved instead.
func GetCCMEgressDestinations(
	ctx context.Context,
	sourceClient client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	infraResult *InfraClientResult,
	clusterName string,
) (CCMEgressDestinations, error) {
	currentOACP, err := getInfraCredentialsOACP(ctx, sourceClient, oacp)
	if err != nil || currentOACP == nil {
		return CCMEgressDestinations{}, err
	}
	if err := ensureInfraCredentialsTargetCurrent(ctx, sourceClient, currentOACP, infraResult); err != nil {
		return CCMEgressDestinations{}, err
	}
	oacp = currentOACP

	destinations, err := infraAPIDestinationCIDRs(ctx, sourceClient, oacp, infraResult)
	if err != nil {
		return CCMEgressDestinations{}, err
	}
	destinations.TenantAPI, err = GetTenantAPIEndpointCIDRs(ctx, infraResult.Client, clusterName, infraResult.Namespace)
	if err != nil {
		return CCMEgressDestinations{}, err
	}
	return destinations, nil
}

func infraAPIDestinationCIDRs(
	ctx context.Context,
	sourceClient client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	infraResult *InfraClientResult,
) (CCMEgressDestinations, error) {
	serviceIP, err := GetOptionalInfraKubernetesServiceIP(ctx, infraResult.Client)
	if err != nil {
		return CCMEgressDestinations{}, err
	}
	if serviceIP != "" {
		return CCMEgressDestinations{
			InfraAPI:      []string{infraAPIHostCIDR(serviceIP)},
			InfraAPIPorts: []int32{443},
		}, nil
	}

	secretName := "infra-cluster-credentials"
	if oacp.Spec.Config.InfraClusterRef != nil {
		secretName = oacp.Spec.Config.InfraClusterRef.Name
	}
	secret := &corev1.Secret{}
	if err := sourceClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: oacp.Namespace}, secret); err != nil {
		return CCMEgressDestinations{}, fmt.Errorf("failed to get infra credentials secret %s/%s: %w", oacp.Namespace, secretName, err)
	}
	kubeconfigData := string(secret.Data["kubeconfig"])
	if kubeconfigData == "" {
		return CCMEgressDestinations{}, fmt.Errorf("infra credentials secret %s/%s does not contain 'kubeconfig' key", oacp.Namespace, secretName)
	}
	return kubeconfigServerDestinationCIDRs(kubeconfigData)
}

func kubeconfigServerDestinationCIDRs(kubeconfigData string) (CCMEgressDestinations, error) {
	kubeconfig, err := clientcmd.Load([]byte(kubeconfigData))
	if err != nil {
		return CCMEgressDestinations{}, err
	}
	currentContext, ok := kubeconfig.Contexts[kubeconfig.CurrentContext]
	if !ok {
		return CCMEgressDestinations{}, fmt.Errorf("infra credentials kubeconfig has no current context")
	}
	cluster, ok := kubeconfig.Clusters[currentContext.Cluster]
	if !ok {
		return CCMEgressDestinations{}, fmt.Errorf("infra credentials kubeconfig has no cluster for current context")
	}
	serverURL, err := url.Parse(cluster.Server)
	if err != nil || serverURL.Host == "" {
		return CCMEgressDestinations{}, fmt.Errorf("infra credentials kubeconfig has an invalid API server URL %q", cluster.Server)
	}
	transportURL := serverURL
	if cluster.ProxyURL != "" {
		proxyURL, err := url.Parse(cluster.ProxyURL)
		if err != nil || proxyURL.Host == "" {
			return CCMEgressDestinations{}, fmt.Errorf("infra credentials kubeconfig has an invalid proxy URL %q", cluster.ProxyURL)
		}
		switch proxyURL.Scheme {
		case "http", "https", "socks5":
		default:
			return CCMEgressDestinations{}, fmt.Errorf("infra credentials kubeconfig has an unsupported proxy URL scheme %q", proxyURL.Scheme)
		}
		transportURL = proxyURL
	}
	apiPort := int32(443)
	if transportURL.Port() != "" {
		parsedPort, err := strconv.ParseInt(transportURL.Port(), 10, 32)
		if err != nil || parsedPort < 1 || parsedPort > 65535 {
			return CCMEgressDestinations{}, fmt.Errorf("infra credentials kubeconfig has an invalid transport port %q", transportURL.Port())
		}
		apiPort = int32(parsedPort)
	} else {
		switch transportURL.Scheme {
		case "http":
			apiPort = 80
		case "socks5":
			apiPort = 1080
		}
	}

	host := transportURL.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		return CCMEgressDestinations{
			InfraAPI:      []string{infraAPIHostCIDR(host)},
			InfraAPIPorts: []int32{apiPort},
		}, nil
	}
	addresses, err := net.LookupIP(host)
	if err != nil {
		return CCMEgressDestinations{}, fmt.Errorf("failed to resolve infra API server %q: %w", host, err)
	}
	destinations := make([]string, 0, len(addresses))
	for _, address := range addresses {
		if ipv4 := address.To4(); ipv4 != nil {
			destinations = append(destinations, infraAPIHostCIDR(ipv4.String()))
		} else {
			destinations = append(destinations, infraAPIHostCIDR(address.String()))
		}
	}
	if len(destinations) == 0 {
		return CCMEgressDestinations{}, fmt.Errorf("infra API server %q resolved to no IP addresses", host)
	}
	return CCMEgressDestinations{
		InfraAPI:      destinations,
		InfraAPIPorts: []int32{apiPort},
		InfraAPIHost:  host,
	}, nil
}

// GetTenantAPIEndpointCIDRs returns the exact control-plane VM addresses behind
// the API Service. EndpointSlices are already readable through the namespaced
// credentials granted to the CCM.
func GetTenantAPIEndpointCIDRs(
	ctx context.Context,
	infraClient client.Client,
	clusterName string,
	namespace string,
) ([]string, error) {
	endpointSlices := &discoveryv1.EndpointSliceList{}
	if err := infraClient.List(
		ctx,
		endpointSlices,
		client.InNamespace(namespace),
		client.MatchingLabels{discoveryv1.LabelServiceName: clusterName + APIServiceNameSuffix},
	); err != nil {
		if apierrors.IsForbidden(err) {
			ctrl.LoggerFrom(ctx).Info(
				"infra identity cannot yet list tenant API endpoint slices; retrying after RBAC reconciliation",
				"error", err,
			)
			return nil, nil
		}
		return nil, fmt.Errorf("failed to list tenant API endpoint slices: %w", err)
	}

	uniqueDestinations := map[string]struct{}{}
	for _, endpointSlice := range endpointSlices.Items {
		for _, endpoint := range endpointSlice.Endpoints {
			if endpoint.Conditions.Ready != nil && !*endpoint.Conditions.Ready {
				continue
			}
			for _, address := range endpoint.Addresses {
				if ip := net.ParseIP(address); ip != nil {
					uniqueDestinations[infraAPIHostCIDR(address)] = struct{}{}
				}
			}
		}
	}
	destinations := make([]string, 0, len(uniqueDestinations))
	for destination := range uniqueDestinations {
		destinations = append(destinations, destination)
	}
	sort.Strings(destinations)
	return destinations, nil
}

// GetOptionalInfraKubernetesServiceIP reads the infra Kubernetes Service when
// permitted. A forbidden response is expected for identities limited to their
// infra namespace; the caller can then use the original API endpoint.
func GetOptionalInfraKubernetesServiceIP(ctx context.Context, infraClient client.Client) (string, error) {
	serviceIP, err := GetInfraKubernetesServiceIP(ctx, infraClient)
	if err != nil {
		if apierrors.IsForbidden(err) {
			ctrl.LoggerFrom(ctx).Info(
				"infra identity cannot read the default Kubernetes Service; using the kubeconfig API endpoint",
				"error", err,
			)
			return "", nil
		}
		return "", err
	}
	return serviceIP, nil
}
