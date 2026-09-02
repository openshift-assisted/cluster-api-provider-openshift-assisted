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
	"os"
	"regexp"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	TenantDNSCorefileName   = "tenant-dns-corefile"
	TenantDNSDeploymentName = "tenant-dns"
	TenantDNSServiceName    = "tenant-dns"
	TenantDNSPort           = 5353
	TenantDNSAppLabel       = "tenant-dns"

	DefaultCoreDNSImage    = "quay.io/openshift/origin-coredns:latest"
	RelatedImageCoreDNSEnv = "RELATED_IMAGE_COREDNS"
)

// GetCoreDNSImage returns the image to use for the DNS proxy.
// Priority: resolved image from release payload > RELATED_IMAGE_COREDNS env var > default.
func GetCoreDNSImage(resolvedCoreDNSImage string) string {
	if resolvedCoreDNSImage != "" {
		return resolvedCoreDNSImage
	}
	if img := os.Getenv(RelatedImageCoreDNSEnv); img != "" {
		return img
	}
	return DefaultCoreDNSImage
}

// DNSProxyConfig holds the IPs used for DNS resolution in the tenant cluster.
type DNSProxyConfig struct {
	APIIP      string
	IngressIPs []string
}

// EnsureDNSProxy deploys a CoreDNS-based DNS proxy on port 5353 that resolves
// tenant cluster domains (api, api-int, *.apps) to the correct ClusterIPs.
// Uses port 5353 to avoid needing anyuid SCC (no cluster-admin required).
func EnsureDNSProxy(
	ctx context.Context,
	c client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	config *DNSProxyConfig,
	namespace string,
) error {
	log := ctrl.LoggerFrom(ctx)
	if config == nil || config.APIIP == "" {
		log.V(1).Info("skipping DNS proxy deployment: API service IP not yet available")
		return nil
	}

	clusterName := oacp.Spec.Config.ClusterName
	if clusterName == "" {
		clusterName = oacp.Name
	}
	baseDomain := oacp.Spec.Config.BaseDomain

	if !isValidDNSName(clusterName) {
		return fmt.Errorf("invalid clusterName %q: must be a valid DNS label", clusterName)
	}
	if !isValidDNSName(baseDomain) {
		return fmt.Errorf("invalid baseDomain %q: must be a valid DNS name", baseDomain)
	}

	fqdn := fmt.Sprintf("%s.%s", clusterName, baseDomain)

	// 1. CoreDNS ConfigMap
	corefile := generateCorefile(fqdn, DefaultInfraClusterDNSIP, []string{config.APIIP}, config.IngressIPs)
	cmData := map[string]string{
		"Corefile": generateAppsCorefile(fqdn) + corefile,
		"apps.db":  generateAppsZoneFile(fqdn, config.IngressIPs),
	}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: TenantDNSCorefileName, Namespace: namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, c, cm, func() error {
		cm.Data = cmData
		return nil
	}); err != nil {
		return fmt.Errorf("failed to ensure corefile: %w", err)
	}

	// 2. Deployment (port 5353 - no SCC needed)
	deploy := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: TenantDNSDeploymentName, Namespace: namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, c, deploy, func() error {
		labels := map[string]string{"app": TenantDNSAppLabel}
		deploy.Spec.Replicas = ptr.To(int32(2))
		deploy.Spec.Selector = &metav1.LabelSelector{MatchLabels: labels}
		deploy.Spec.Template = corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: labels},
			Spec: corev1.PodSpec{
				Affinity: &corev1.Affinity{
					PodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{{
							Weight: 100,
							PodAffinityTerm: corev1.PodAffinityTerm{
								LabelSelector: &metav1.LabelSelector{MatchLabels: labels},
								TopologyKey:   "kubernetes.io/hostname",
							},
						}},
					},
				},
				Containers: []corev1.Container{{
					Name:  "coredns",
					Image: GetCoreDNSImage(""),
					Args:  []string{"-conf", "/etc/coredns/Corefile"},
					Ports: []corev1.ContainerPort{
						{ContainerPort: int32(TenantDNSPort), Protocol: corev1.ProtocolUDP},
						{ContainerPort: int32(TenantDNSPort), Protocol: corev1.ProtocolTCP},
					},
					VolumeMounts: []corev1.VolumeMount{{
						Name: "config", MountPath: "/etc/coredns", ReadOnly: true,
					}},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("10m"),
							corev1.ResourceMemory: resource.MustParse("32Mi"),
						},
					},
				}},
				Volumes: []corev1.Volume{{
					Name: "config",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: TenantDNSCorefileName},
						},
					},
				}},
			},
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to ensure deployment: %w", err)
	}

	// 3. Service
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: TenantDNSServiceName, Namespace: namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, c, svc, func() error {
		svc.Spec.Selector = map[string]string{"app": TenantDNSAppLabel}
		svc.Spec.Ports = []corev1.ServicePort{
			{Name: "dns-udp", Port: int32(TenantDNSPort), TargetPort: intstr.FromInt(TenantDNSPort), Protocol: corev1.ProtocolUDP},
			{Name: "dns-tcp", Port: int32(TenantDNSPort), TargetPort: intstr.FromInt(TenantDNSPort), Protocol: corev1.ProtocolTCP},
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to ensure service: %w", err)
	}

	return nil
}

// GetDNSProxyClusterIP returns the ClusterIP of the DNS proxy service.
func GetDNSProxyClusterIP(ctx context.Context, c client.Client, namespace string) string {
	svc := &corev1.Service{}
	if err := c.Get(ctx, client.ObjectKey{Name: TenantDNSServiceName, Namespace: namespace}, svc); err != nil {
		return ""
	}
	return svc.Spec.ClusterIP
}

// GetAPIServiceClusterIP returns the ClusterIP of the tenant's API service.
func GetAPIServiceClusterIP(ctx context.Context, c client.Client, clusterName, namespace string) string {
	svc := &corev1.Service{}
	if err := c.Get(ctx, client.ObjectKey{Name: clusterName + APIServiceNameSuffix, Namespace: namespace}, svc); err != nil {
		return ""
	}
	return svc.Spec.ClusterIP
}

// EnsureDNSForwardingRule ensures the cluster DNS operator forwards queries for
// the tenant cluster's zone to the DNS proxy service. Uses retry-on-conflict to
// safely handle concurrent updates from multiple tenant cluster controllers.
func EnsureDNSForwardingRule(ctx context.Context, c client.Client, clusterName, baseDomain, dnsProxyClusterIP, namespace string) error {
	if dnsProxyClusterIP == "" {
		return nil
	}

	fqdn := fmt.Sprintf("%s.%s", clusterName, baseDomain)
	ruleName := clusterName + "-dns"
	upstream := net.JoinHostPort(dnsProxyClusterIP, fmt.Sprintf("%d", TenantDNSPort))

	dnsGVK := schema.GroupVersionKind{
		Group:   "operator.openshift.io",
		Version: "v1",
		Kind:    "DNS",
	}

	return retryOnConflict(3, func() error {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(dnsGVK)
		if err := c.Get(ctx, types.NamespacedName{Name: "default"}, obj); err != nil {
			return fmt.Errorf("failed to get dns.operator.openshift.io/default: %w", err)
		}

		spec, _ := obj.Object["spec"].(map[string]interface{})
		if spec == nil {
			spec = map[string]interface{}{}
			obj.Object["spec"] = spec
		}

		servers, _ := spec["servers"].([]interface{})

		desiredServer := map[string]interface{}{
			"name":  ruleName,
			"zones": []interface{}{fqdn},
			"forwardPlugin": map[string]interface{}{
				"upstreams": []interface{}{upstream},
				"policy":    "Random",
			},
		}

		found := false
		for i, s := range servers {
			srv, ok := s.(map[string]interface{})
			if !ok {
				continue
			}
			if srv["name"] == ruleName {
				servers[i] = desiredServer
				found = true
				break
			}
		}
		if !found {
			servers = append(servers, desiredServer)
		}

		spec["servers"] = servers
		return c.Update(ctx, obj)
	})
}

// RemoveDNSForwardingRule removes the tenant's DNS forwarding rule from the cluster
// DNS operator. Called during tenant cluster cleanup. Uses retry-on-conflict.
func RemoveDNSForwardingRule(ctx context.Context, c client.Client, clusterName string) error {
	ruleName := clusterName + "-dns"

	dnsGVK := schema.GroupVersionKind{
		Group:   "operator.openshift.io",
		Version: "v1",
		Kind:    "DNS",
	}

	return retryOnConflict(3, func() error {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(dnsGVK)
		if err := c.Get(ctx, types.NamespacedName{Name: "default"}, obj); err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}

		spec, _ := obj.Object["spec"].(map[string]interface{})
		if spec == nil {
			return nil
		}

		servers, _ := spec["servers"].([]interface{})
		if len(servers) == 0 {
			return nil
		}

		filtered := make([]interface{}, 0, len(servers))
		for _, s := range servers {
			srv, ok := s.(map[string]interface{})
			if !ok {
				filtered = append(filtered, s)
				continue
			}
			if srv["name"] != ruleName {
				filtered = append(filtered, s)
			}
		}

		if len(filtered) == len(servers) {
			return nil
		}

		spec["servers"] = filtered
		return c.Update(ctx, obj)
	})
}

// GetRouterNodeIPs returns the pod IPs of virt-launcher pods for the cluster's
// control plane VMs.
func GetRouterNodeIPs(ctx context.Context, c client.Reader, clusterName, namespace string) ([]string, error) {
	pods := &corev1.PodList{}
	if err := c.List(ctx, pods,
		client.InNamespace(namespace),
		client.MatchingLabels{
			"cluster.x-k8s.io/cluster-name": clusterName,
			"cluster.x-k8s.io/role":         "control-plane",
		},
	); err != nil {
		return nil, err
	}

	var ips []string
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning && pod.Status.PodIP != "" {
			ips = append(ips, pod.Status.PodIP)
		}
	}
	return ips, nil
}

// retryOnConflict retries fn up to maxRetries times when it returns a conflict error.
func retryOnConflict(maxRetries int, fn func() error) error {
	var err error
	for i := 0; i <= maxRetries; i++ {
		err = fn()
		if err == nil || !errors.IsConflict(err) {
			return err
		}
	}
	return err
}

// dnsNameRegex matches valid DNS names (labels separated by dots, each 1-63 chars).
var dnsNameRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)

func isValidDNSName(name string) bool {
	return len(name) > 0 && len(name) <= 253 && dnsNameRegex.MatchString(name)
}
