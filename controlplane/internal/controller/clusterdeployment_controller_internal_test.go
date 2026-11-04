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

package controller

import (
	"context"
	"testing"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
)

const (
	ensureACIClusterName = "ensure-aci-cluster"
	ensureACINamespace   = "ensure-aci"
)

func TestGetClusterNetworks(t *testing.T) {
	tests := []struct {
		name         string
		podCIDRs     []string
		serviceCIDRs []string
		expected     []hiveext.ClusterNetworkEntry
	}{
		{
			name:         "IPv4",
			podCIDRs:     []string{"10.128.0.0/14"},
			serviceCIDRs: []string{"172.30.0.0/16"},
			expected: []hiveext.ClusterNetworkEntry{
				{CIDR: "10.128.0.0/14", HostPrefix: 23},
			},
		},
		{
			name:         "IPv6",
			podCIDRs:     []string{"fd01::/48"},
			serviceCIDRs: []string{"fd02::/112"},
			expected: []hiveext.ClusterNetworkEntry{
				{CIDR: "fd01::/48", HostPrefix: 64},
			},
		},
		{
			name:         "dual stack",
			podCIDRs:     []string{"10.128.0.0/14", "fd01::/48"},
			serviceCIDRs: []string{"172.30.0.0/16", "fd02::/112"},
			expected: []hiveext.ClusterNetworkEntry{
				{CIDR: "10.128.0.0/14", HostPrefix: 23},
				{CIDR: "fd01::/48", HostPrefix: 64},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &clusterv1.Cluster{}
			cluster.Spec.ClusterNetwork.Pods.CIDRBlocks = tt.podCIDRs
			cluster.Spec.ClusterNetwork.Services.CIDRBlocks = tt.serviceCIDRs

			clusterNetwork, serviceNetwork := getClusterNetworks(cluster)

			if len(clusterNetwork) != len(tt.expected) {
				t.Fatalf("expected %d cluster network entries, got %d", len(tt.expected), len(clusterNetwork))
			}
			for i := range tt.expected {
				if clusterNetwork[i] != tt.expected[i] {
					t.Errorf("cluster network entry %d: expected %#v, got %#v", i, tt.expected[i], clusterNetwork[i])
				}
			}
			if len(serviceNetwork) != len(tt.serviceCIDRs) {
				t.Fatalf("expected %d service network entries, got %d", len(tt.serviceCIDRs), len(serviceNetwork))
			}
			for i := range tt.serviceCIDRs {
				if serviceNetwork[i] != tt.serviceCIDRs[i] {
					t.Errorf("service network entry %d: expected %q, got %q", i, tt.serviceCIDRs[i], serviceNetwork[i])
				}
			}
		})
	}
}

func TestEnsureAgentClusterInstallKubeVirtIgnitionCA(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(controlplanev1alpha3.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(hivev1.AddToScheme(scheme))
	utilruntime.Must(hiveext.AddToScheme(scheme))

	oacp := &controlplanev1alpha3.OpenshiftAssistedControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ensureACIClusterName,
			Namespace: ensureACINamespace,
		},
	}
	clusterDeployment := &hivev1.ClusterDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ensureACIClusterName,
			Namespace: ensureACINamespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: ensureACIClusterName,
			},
		},
	}
	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ensureACIClusterName,
			Namespace: ensureACINamespace,
		},
		Spec: clusterv1.ClusterSpec{
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				Kind: "KubevirtCluster",
			},
		},
	}
	aci := &hiveext.AgentClusterInstall{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ensureACIClusterName,
			Namespace: ensureACINamespace,
		},
		Spec: hiveext.AgentClusterInstallSpec{
			ClusterMetadata: &hivev1.ClusterMetadata{
				AdminPasswordSecretRef: &corev1.LocalObjectReference{
					Name: "admin-password",
				},
			},
		},
	}
	serviceCAConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceCAConfigMapName,
			Namespace: ensureACINamespace,
		},
		Data: map[string]string{
			serviceCAConfigMapKey: "service-ca-1",
		},
	}

	k8sClient := fakeclient.NewClientBuilder().WithScheme(scheme).
		WithObjects(oacp, aci, serviceCAConfigMap).Build()
	reconciler := &ClusterDeploymentReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	err := reconciler.ensureAgentClusterInstall(context.Background(), clusterDeployment, *oacp, cluster)
	if err != nil {
		t.Fatalf("ensureAgentClusterInstall() error = %v", err)
	}

	updatedACI := &hiveext.AgentClusterInstall{}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{
		Name:      ensureACIClusterName,
		Namespace: ensureACINamespace,
	}, updatedACI); err != nil {
		t.Fatalf("failed to get updated AgentClusterInstall: %v", err)
	}
	if updatedACI.Spec.IgnitionEndpoint == nil {
		t.Fatal("expected IgnitionEndpoint to be set")
	}
	expectedURL := "http://ensure-aci-cluster-mcs-proxy.ensure-aci.svc.cluster.local:8624/config/worker"
	if updatedACI.Spec.IgnitionEndpoint.Url != expectedURL {
		t.Fatalf("expected ignition URL %q, got %q", expectedURL, updatedACI.Spec.IgnitionEndpoint.Url)
	}
	caReference := updatedACI.Spec.IgnitionEndpoint.CaCertificateReference
	if caReference == nil {
		t.Fatal("expected IgnitionEndpoint CA reference to be set")
	}
	if caReference.Name != "ensure-aci-cluster-ignition-ca" || caReference.Namespace != ensureACINamespace {
		t.Fatalf("unexpected CA reference: %#v", caReference)
	}

	caSecret := &corev1.Secret{}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{
		Name:      caReference.Name,
		Namespace: caReference.Namespace,
	}, caSecret); err != nil {
		t.Fatalf("failed to get ignition CA Secret: %v", err)
	}
	if string(caSecret.Data[ignitionCASecretKey]) != "service-ca-1" {
		t.Fatalf("expected service CA to be copied, got %q", caSecret.Data[ignitionCASecretKey])
	}

	serviceCAConfigMap.Data[serviceCAConfigMapKey] = "service-ca-2"
	if err := k8sClient.Update(context.Background(), serviceCAConfigMap); err != nil {
		t.Fatalf("failed to update service CA ConfigMap: %v", err)
	}
	if err := reconciler.ensureAgentClusterInstall(context.Background(), clusterDeployment, *oacp, cluster); err != nil {
		t.Fatalf("ensureAgentClusterInstall() after CA rotation error = %v", err)
	}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{
		Name:      caReference.Name,
		Namespace: caReference.Namespace,
	}, caSecret); err != nil {
		t.Fatalf("failed to get rotated ignition CA Secret: %v", err)
	}
	if string(caSecret.Data[ignitionCASecretKey]) != "service-ca-2" {
		t.Fatalf("expected rotated service CA, got %q", caSecret.Data[ignitionCASecretKey])
	}
}

func TestEnsureAgentClusterInstallNonKubeVirtIgnitionCA(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(controlplanev1alpha3.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(hivev1.AddToScheme(scheme))
	utilruntime.Must(hiveext.AddToScheme(scheme))

	oacp := &controlplanev1alpha3.OpenshiftAssistedControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ensureACIClusterName,
			Namespace: ensureACINamespace,
		},
	}
	clusterDeployment := &hivev1.ClusterDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ensureACIClusterName,
			Namespace: ensureACINamespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: ensureACIClusterName,
			},
		},
	}
	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ensureACIClusterName,
			Namespace: ensureACINamespace,
		},
		Spec: clusterv1.ClusterSpec{
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				Kind: "Metal3Cluster",
			},
		},
	}
	aci := &hiveext.AgentClusterInstall{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ensureACIClusterName,
			Namespace: ensureACINamespace,
		},
		Spec: hiveext.AgentClusterInstallSpec{
			ClusterMetadata: &hivev1.ClusterMetadata{
				AdminPasswordSecretRef: &corev1.LocalObjectReference{
					Name: "admin-password",
				},
			},
		},
	}

	k8sClient := fakeclient.NewClientBuilder().WithScheme(scheme).WithObjects(oacp, aci).Build()
	reconciler := &ClusterDeploymentReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	err := reconciler.ensureAgentClusterInstall(context.Background(), clusterDeployment, *oacp, cluster)
	if err != nil {
		t.Fatalf("ensureAgentClusterInstall() error = %v", err)
	}

	updatedACI := &hiveext.AgentClusterInstall{}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{
		Name:      ensureACIClusterName,
		Namespace: ensureACINamespace,
	}, updatedACI); err != nil {
		t.Fatalf("failed to get updated AgentClusterInstall: %v", err)
	}
	if updatedACI.Spec.IgnitionEndpoint != nil {
		t.Fatalf("expected IgnitionEndpoint not to be set, got %#v", updatedACI.Spec.IgnitionEndpoint)
	}

	caSecret := &corev1.Secret{}
	err = k8sClient.Get(context.Background(), types.NamespacedName{
		Name:      ensureACIClusterName + "-ignition-ca",
		Namespace: ensureACINamespace,
	}, caSecret)
	if err == nil {
		t.Fatal("expected ignition CA Secret not to be created")
	}
}
