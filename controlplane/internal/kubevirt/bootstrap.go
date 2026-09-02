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
	"strings"

	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetBootstrapPodIP finds the pod IP of the bootstrap node for the cluster.
//
// In assisted-installer 3-node clusters, one node is designated as the bootstrap/rendezvous
// node. During installation, ONLY this node runs the API server (port 6443) and Machine
// Config Server (port 22623). The other control plane nodes need to reach these services
// to pull their configuration and join the cluster.
//
// The bootstrap node is identified by the Agent resource's status.bootstrap=true field,
// which is set by Assisted Service. We then find the corresponding virt-launcher pod
// to get the actual pod IP.
//
// If we use the API service's ClusterIP for DNS resolution, kube-proxy load-balances
// across ALL control plane VM pods. Since only the bootstrap has the API/MCS running,
// ~2/3 of connections fail (routed to non-responsive VMs), stalling installation.
//
// After installation completes and all nodes run the API server, the DNS should be
// updated to use the service ClusterIP for proper load balancing.
func GetBootstrapPodIP(ctx context.Context, c client.Reader, clusterName, namespace string) (string, error) {
	agents := &aiv1beta1.AgentList{}
	if err := c.List(ctx, agents, client.InNamespace(namespace)); err != nil {
		return "", err
	}

	var bootstrapHostname string
	for i := range agents.Items {
		if agents.Items[i].Spec.ClusterDeploymentName == nil {
			continue
		}
		if agents.Items[i].Spec.ClusterDeploymentName.Name != clusterName {
			continue
		}
		if agents.Items[i].Status.Bootstrap {
			bootstrapHostname = agents.Items[i].Status.Inventory.Hostname
			break
		}
	}

	if bootstrapHostname == "" {
		return "", nil
	}

	// The hostname in the Agent inventory matches the VM/Machine name.
	// Find the virt-launcher pod for this VM.
	pods := &corev1.PodList{}
	if err := c.List(ctx, pods,
		client.InNamespace(namespace),
		client.MatchingLabels{
			"vm.kubevirt.io/name": bootstrapHostname,
		},
	); err != nil {
		return "", err
	}

	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning && pod.Status.PodIP != "" {
			return pod.Status.PodIP, nil
		}
	}

	// Fallback: extract IP directly from the Agent's inventory interfaces.
	// This handles edge cases where the pod label doesn't exactly match.
	for i := range agents.Items {
		if agents.Items[i].Spec.ClusterDeploymentName == nil ||
			agents.Items[i].Spec.ClusterDeploymentName.Name != clusterName {
			continue
		}
		if agents.Items[i].Status.Bootstrap {
			for _, iface := range agents.Items[i].Status.Inventory.Interfaces {
				for _, addr := range iface.IPV4Addresses {
					ip := strings.Split(addr, "/")[0]
					if ip != "" && !isNonRoutableIP(ip) {
						return ip, nil
					}
				}
			}
		}
	}

	return "", nil
}

// isNonRoutableIP returns true for IPs that are KubeVirt-internal and not
// routable from other pods (e.g., masquerade 10.0.2.x range).
func isNonRoutableIP(ip string) bool {
	return strings.HasPrefix(ip, "10.0.2.") || ip == "127.0.0.1"
}
