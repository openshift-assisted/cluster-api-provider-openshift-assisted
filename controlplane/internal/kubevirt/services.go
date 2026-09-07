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

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	APIServiceNameSuffix     = "-api"
	IngressServiceNameSuffix = "-ingress"
)

// ServiceIPs holds the ClusterIPs assigned to the tenant cluster services.
// These are used by the DNS proxy to provide stable name resolution for
// api/api-int and *.apps domains during and after installation.
type ServiceIPs struct {
	APIClusterIP     string
	IngressClusterIP string
}

// EnsureExternalAccessServices creates ClusterIP Services on the infra cluster
// to expose the tenant cluster's API and Ingress endpoints.
//
// The API service exposes ports 6443 (kube-apiserver), 22623 (MCS for bootstrap),
// 443 (for Route-based access where kubeconfig is rewritten to port 443), and
// 30624 (MCS NodePort for day-2 worker ignition via the MCS proxy).
//
// The Ingress service exposes ports 443 (HTTPS) and 80 (HTTP) for tenant
// cluster ingress traffic forwarded through the wildcard Route.
//
// The selector uses the CAPI cluster label (cluster.x-k8s.io/cluster-name) which
// is automatically set by CAPK on all virt-launcher pods for the cluster.
func EnsureExternalAccessServices(
	ctx context.Context,
	c client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	clusterName string,
	namespace string,
) (*ServiceIPs, error) {
	apiSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName + APIServiceNameSuffix,
			Namespace: namespace,
		},
	}

	if _, err := controllerutil.CreateOrUpdate(ctx, c, apiSvc, func() error {
		apiSvc.Labels = map[string]string{
			"app":                           clusterName + "-api",
			"cluster.x-k8s.io/cluster-name": clusterName,
		}
		apiSvc.Spec.Type = corev1.ServiceTypeClusterIP
		apiSvc.Spec.Selector = map[string]string{
			"cluster.x-k8s.io/cluster-name": clusterName,
			"cluster.x-k8s.io/role":         "control-plane",
		}
		apiSvc.Spec.Ports = []corev1.ServicePort{
			{
				Name:       "api",
				Port:       6443,
				TargetPort: intstr.FromInt(6443),
				Protocol:   corev1.ProtocolTCP,
			},
			{
				Name:       "mcs",
				Port:       22623,
				TargetPort: intstr.FromInt(22623),
				Protocol:   corev1.ProtocolTCP,
			},
			{
				Name:       "api-route",
				Port:       443,
				TargetPort: intstr.FromInt(6443),
				Protocol:   corev1.ProtocolTCP,
			},
			{
				Name:       "mcs-nodeport",
				Port:       MCSNodePort,
				TargetPort: intstr.FromInt(MCSNodePort),
				Protocol:   corev1.ProtocolTCP,
			},
		}
		if apiSvc.Namespace == oacp.Namespace {
			return controllerutil.SetOwnerReference(oacp, apiSvc, c.Scheme())
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to ensure API service: %w", err)
	}

	ips := &ServiceIPs{
		APIClusterIP: apiSvc.Spec.ClusterIP,
	}

	ingressSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName + IngressServiceNameSuffix,
			Namespace: namespace,
		},
	}

	if _, err := controllerutil.CreateOrUpdate(ctx, c, ingressSvc, func() error {
		ingressSvc.Labels = map[string]string{
			"app":                           clusterName + "-ingress",
			"cluster.x-k8s.io/cluster-name": clusterName,
		}
		ingressSvc.Spec.Type = corev1.ServiceTypeClusterIP
		ingressSvc.Spec.Selector = map[string]string{
			"cluster.x-k8s.io/cluster-name": clusterName,
			"cluster.x-k8s.io/role":         "control-plane",
		}
		ingressSvc.Spec.Ports = []corev1.ServicePort{
			{
				Name:       "https",
				Port:       443,
				TargetPort: intstr.FromInt(443),
				Protocol:   corev1.ProtocolTCP,
			},
			{
				Name:       "http",
				Port:       80,
				TargetPort: intstr.FromInt(80),
				Protocol:   corev1.ProtocolTCP,
			},
		}
		if ingressSvc.Namespace == oacp.Namespace {
			return controllerutil.SetOwnerReference(oacp, ingressSvc, c.Scheme())
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to ensure Ingress service: %w", err)
	}
	ips.IngressClusterIP = ingressSvc.Spec.ClusterIP

	return ips, nil
}

// GetServiceClusterIP returns the ClusterIP of a named Service, or empty string if not found.
func GetServiceClusterIP(ctx context.Context, c client.Client, name, namespace string) string {
	svc := &corev1.Service{}
	if err := c.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, svc); err != nil {
		return ""
	}
	return svc.Spec.ClusterIP
}
