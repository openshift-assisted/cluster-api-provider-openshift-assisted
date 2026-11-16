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
	"time"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	kubevirt "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/kubevirt"
)

const (
	ensureACIClusterName = "ensure-aci-cluster"
	ensureACINamespace   = "ensure-aci"
	testSSHKey           = "test-ssh-key"
	validCACertificate1  = `-----BEGIN CERTIFICATE-----
MIIBfDCCASOgAwIBAgIUHt2d+kRGeQe/SUAgn/k8O6j7YfMwCgYIKoZIzj0EAwIw
FDESMBAGA1UEAwwJdGVzdC1jYS0xMB4XDTI2MTEwOTE4NDkwNFoXDTM2MTEwNjE4
NDkwNFowFDESMBAGA1UEAwwJdGVzdC1jYS0xMFkwEwYHKoZIzj0CAQYIKoZIzj0D
AQcDQgAEuISy+jYMrYTZSpvWl8vvqaXLfFPhjtN2DyXyQM5XBaJ2QE9oZVtd/1pW
TEtXthRWIFZia/ruyUxCfAZh45CwzqNTMFEwHQYDVR0OBBYEFO8rd9K6BJGftS3z
fJUQNOfEIDhHMB8GA1UdIwQYMBaAFO8rd9K6BJGftS3zfJUQNOfEIDhHMA8GA1Ud
EwEB/wQFMAMBAf8wCgYIKoZIzj0EAwIDRwAwRAIgYOmgtgJewd1sGve2yfNtZhdX
7j4kIbsT9RTh2JNOQ+cCIHYF5DsHgxS+cC0SfbbjXeDgRhu4jPvYJRb2I9WBL5da
-----END CERTIFICATE-----`
	validCACertificate2 = `-----BEGIN CERTIFICATE-----
MIIBfjCCASOgAwIBAgIUFGhAIVlDzbj+n5thSc1uKbS2V6owCgYIKoZIzj0EAwIw
FDESMBAGA1UEAwwJdGVzdC1jYS0yMB4XDTI2MTEwOTE4NDkwNFoXDTM2MTEwNjE4
NDkwNFowFDESMBAGA1UEAwwJdGVzdC1jYS0yMFkwEwYHKoZIzj0CAQYIKoZIzj0D
AQcDQgAEvbtbupvZ+O7BSj8tRX6oiTuI3aVoNjKlF8rw50Iw4A9WoA6H3ID7F0XZ
xXTt6XPyKFiywjKMfjpv0nWpw/n6dqNTMFEwHQYDVR0OBBYEFIWIGCp6ZN3zjyIL
ueaiSWXNlMvUMB8GA1UdIwQYMBaAFIWIGCp6ZN3zjyILueaiSWXNlMvUMA8GA1Ud
EwEB/wQFMAMBAf8wCgYIKoZIzj0EAwIDSQAwRgIhAK6SAXeV1OtJ7s2PwobdCnyr
DdnR2WpqJohtp140wLgaAiEA0/ZcDBrMieRVQb3c/bXQGIAMIrkCW5oG2R1X39U+
0f0=
-----END CERTIFICATE-----`
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
			UID:       "oacp-uid",
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
	unlabeledClusterDeployment := &hivev1.ClusterDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unlabeled-cluster-deployment",
			Namespace: ensureACINamespace,
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
			serviceCAConfigMapKey: validCACertificate1,
		},
	}
	rootCAConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rootCAConfigMapName,
			Namespace: ensureACINamespace,
		},
		Data: map[string]string{
			rootCAConfigMapKey: validCACertificate1,
		},
	}

	k8sClient := fakeclient.NewClientBuilder().WithScheme(scheme).
		WithObjects(oacp, clusterDeployment, unlabeledClusterDeployment, cluster, aci, serviceCAConfigMap, rootCAConfigMap).Build()
	reconciler := &ClusterDeploymentReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	result, err := reconciler.ensureAgentClusterInstall(context.Background(), clusterDeployment, *oacp, cluster, nil)
	if err != nil {
		t.Fatalf("ensureAgentClusterInstall() error = %v", err)
	}
	if !result.IsZero() {
		t.Fatalf("expected no requeue, got %#v", result)
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
	if string(caSecret.Data[ignitionCASecretKey]) != validCACertificate1 {
		t.Fatalf("expected service CA to be copied, got %q", caSecret.Data[ignitionCASecretKey])
	}

	serviceCAConfigMap.Data[serviceCAConfigMapKey] = validCACertificate2
	if err := k8sClient.Update(context.Background(), serviceCAConfigMap); err != nil {
		t.Fatalf("failed to update service CA ConfigMap: %v", err)
	}

	requests := reconciler.serviceCAConfigMapToClusterDeployments(context.Background(), serviceCAConfigMap)
	if len(requests) != 1 {
		t.Fatalf("expected one ClusterDeployment request, got %#v", requests)
	}
	if requests[0].NamespacedName != (types.NamespacedName{
		Name:      clusterDeployment.Name,
		Namespace: clusterDeployment.Namespace,
	}) {
		t.Fatalf("unexpected ClusterDeployment request: %#v", requests[0])
	}

	for _, request := range requests {
		rotatedClusterDeployment := &hivev1.ClusterDeployment{}
		if err := k8sClient.Get(context.Background(), request.NamespacedName, rotatedClusterDeployment); err != nil {
			t.Fatalf("failed to get ClusterDeployment from service CA mapping: %v", err)
		}
		rotatedOACP := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
		if err := k8sClient.Get(context.Background(), request.NamespacedName, rotatedOACP); err != nil {
			t.Fatalf("failed to get OpenshiftAssistedControlPlane from service CA mapping: %v", err)
		}
		rotatedCluster := &clusterv1.Cluster{}
		if err := k8sClient.Get(context.Background(), request.NamespacedName, rotatedCluster); err != nil {
			t.Fatalf("failed to get Cluster from service CA mapping: %v", err)
		}
		if _, err := reconciler.ensureAgentClusterInstall(
			context.Background(),
			rotatedClusterDeployment,
			*rotatedOACP,
			rotatedCluster,
			nil,
		); err != nil {
			t.Fatalf("ensureAgentClusterInstall() after CA rotation error = %v", err)
		}
	}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{
		Name:      caReference.Name,
		Namespace: caReference.Namespace,
	}, caSecret); err != nil {
		t.Fatalf("failed to get rotated ignition CA Secret: %v", err)
	}
	if string(caSecret.Data[ignitionCASecretKey]) != validCACertificate2 {
		t.Fatalf("expected rotated service CA, got %q", caSecret.Data[ignitionCASecretKey])
	}

	unrelatedConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unrelated-config-map",
			Namespace: ensureACINamespace,
		},
	}
	if requests := reconciler.serviceCAConfigMapToClusterDeployments(
		context.Background(),
		unrelatedConfigMap,
	); len(requests) != 0 {
		t.Fatalf("expected no requests for an unrelated ConfigMap, got %#v", requests)
	}
}

func TestEnsureAgentClusterInstallKubeVirtRootCAFallback(t *testing.T) {
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
			UID:       "oacp-uid",
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
	rootCAConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rootCAConfigMapName,
			Namespace: ensureACINamespace,
		},
		Data: map[string]string{
			rootCAConfigMapKey: validCACertificate1,
		},
	}

	k8sClient := fakeclient.NewClientBuilder().WithScheme(scheme).
		WithObjects(oacp, clusterDeployment, cluster, aci, rootCAConfigMap).Build()
	reconciler := &ClusterDeploymentReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	result, err := reconciler.ensureAgentClusterInstall(context.Background(), clusterDeployment, *oacp, cluster, nil)
	if err != nil {
		t.Fatalf("ensureAgentClusterInstall() error = %v", err)
	}
	if !result.IsZero() {
		t.Fatalf("expected no requeue, got %#v", result)
	}

	updatedACI := &hiveext.AgentClusterInstall{}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{
		Name:      ensureACIClusterName,
		Namespace: ensureACINamespace,
	}, updatedACI); err != nil {
		t.Fatalf("failed to get updated AgentClusterInstall: %v", err)
	}
	if updatedACI.Spec.IgnitionEndpoint == nil || updatedACI.Spec.IgnitionEndpoint.CaCertificateReference == nil {
		t.Fatalf("expected IgnitionEndpoint CA reference to be set, got %#v", updatedACI.Spec.IgnitionEndpoint)
	}

	caSecret := &corev1.Secret{}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{
		Name:      ensureACIClusterName + ignitionCASecretSuffix,
		Namespace: ensureACINamespace,
	}, caSecret); err != nil {
		t.Fatalf("failed to get ignition CA Secret: %v", err)
	}
	if string(caSecret.Data[ignitionCASecretKey]) != validCACertificate1 {
		t.Fatalf("expected root CA to be copied, got %q", caSecret.Data[ignitionCASecretKey])
	}

	rootCAConfigMap.Data[rootCAConfigMapKey] = validCACertificate2
	if err := k8sClient.Update(context.Background(), rootCAConfigMap); err != nil {
		t.Fatalf("failed to update root CA ConfigMap: %v", err)
	}
	requests := reconciler.serviceCAConfigMapToClusterDeployments(context.Background(), rootCAConfigMap)
	if len(requests) != 1 {
		t.Fatalf("expected one ClusterDeployment request for root CA rotation, got %#v", requests)
	}
	if _, err := reconciler.ensureAgentClusterInstall(context.Background(), clusterDeployment, *oacp, cluster, nil); err != nil {
		t.Fatalf("ensureAgentClusterInstall() after root CA rotation error = %v", err)
	}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{
		Name:      ensureACIClusterName + ignitionCASecretSuffix,
		Namespace: ensureACINamespace,
	}, caSecret); err != nil {
		t.Fatalf("failed to get rotated ignition CA Secret: %v", err)
	}
	if string(caSecret.Data[ignitionCASecretKey]) != validCACertificate2 {
		t.Fatalf("expected rotated root CA, got %q", caSecret.Data[ignitionCASecretKey])
	}
}

func TestEnsureAgentClusterInstallKubeVirtInvalidCA(t *testing.T) {
	tests := []struct {
		name       string
		configMaps []client.Object
	}{
		{
			name: "invalid service CA",
			configMaps: []client.Object{&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceCAConfigMapName,
					Namespace: ensureACINamespace,
				},
				Data: map[string]string{
					serviceCAConfigMapKey: "not-a-certificate",
				},
			}},
		},
		{
			name: "invalid root CA",
			configMaps: []client.Object{&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rootCAConfigMapName,
					Namespace: ensureACINamespace,
				},
				Data: map[string]string{
					rootCAConfigMapKey: "not-a-certificate",
				},
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
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
					UID:       "oacp-uid",
				},
				Spec: controlplanev1alpha3.OpenshiftAssistedControlPlaneSpec{
					Config: controlplanev1alpha3.OpenshiftAssistedControlPlaneConfigSpec{
						SSHAuthorizedKey: testSSHKey,
					},
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

			k8sClient := fakeclient.NewClientBuilder().WithScheme(scheme).
				WithObjects(append([]client.Object{oacp, clusterDeployment, cluster, aci}, test.configMaps...)...).Build()
			reconciler := &ClusterDeploymentReconciler{
				Client: k8sClient,
				Scheme: scheme,
			}

			result, err := reconciler.ensureAgentClusterInstall(
				context.Background(),
				clusterDeployment,
				*oacp,
				cluster,
				nil,
			)
			if err != nil {
				t.Fatalf("ensureAgentClusterInstall() error = %v", err)
			}
			if result.RequeueAfter != 30*time.Second {
				t.Fatalf("expected 30s pending-CA requeue, got %#v", result)
			}

			updatedACI := &hiveext.AgentClusterInstall{}
			if err := k8sClient.Get(context.Background(), types.NamespacedName{
				Name:      ensureACIClusterName,
				Namespace: ensureACINamespace,
			}, updatedACI); err != nil {
				t.Fatalf("failed to get updated AgentClusterInstall: %v", err)
			}
			if updatedACI.Spec.SSHPublicKey != testSSHKey {
				t.Fatalf("expected unrelated ACI field to be updated, got %q", updatedACI.Spec.SSHPublicKey)
			}
			if updatedACI.Spec.IgnitionEndpoint != nil {
				t.Fatalf("expected IgnitionEndpoint not to be set, got %#v", updatedACI.Spec.IgnitionEndpoint)
			}

			caSecret := &corev1.Secret{}
			err = k8sClient.Get(context.Background(), types.NamespacedName{
				Name:      ensureACIClusterName + ignitionCASecretSuffix,
				Namespace: ensureACINamespace,
			}, caSecret)
			if err == nil {
				t.Fatal("expected invalid CA not to be published in an ignition CA Secret")
			}
		})
	}
}

func TestEnsureAgentClusterInstallKubeVirtRemoteInfraRootCA(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(controlplanev1alpha3.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(hivev1.AddToScheme(scheme))
	utilruntime.Must(hiveext.AddToScheme(scheme))

	const infraNamespace = "infra-namespace"
	oacp := &controlplanev1alpha3.OpenshiftAssistedControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ensureACIClusterName,
			Namespace: ensureACINamespace,
			UID:       "oacp-uid",
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
	rootCAConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rootCAConfigMapName,
			Namespace: infraNamespace,
		},
		Data: map[string]string{
			rootCAConfigMapKey: validCACertificate1,
		},
	}

	managementClient := fakeclient.NewClientBuilder().WithScheme(scheme).
		WithObjects(oacp, clusterDeployment, cluster, aci).Build()
	infraClient := fakeclient.NewClientBuilder().WithScheme(scheme).
		WithObjects(rootCAConfigMap).Build()
	reconciler := &ClusterDeploymentReconciler{
		Client: managementClient,
		Scheme: scheme,
	}
	infraResult := &kubevirt.InfraClientResult{
		Client:    infraClient,
		Namespace: infraNamespace,
		IsRemote:  true,
	}

	result, err := reconciler.ensureAgentClusterInstall(
		context.Background(),
		clusterDeployment,
		*oacp,
		cluster,
		infraResult,
	)
	if err != nil {
		t.Fatalf("ensureAgentClusterInstall() error = %v", err)
	}
	if result.RequeueAfter != remoteIgnitionCARefreshInterval {
		t.Fatalf("expected remote CA refresh requeue, got %#v", result)
	}

	updatedACI := &hiveext.AgentClusterInstall{}
	if err := managementClient.Get(context.Background(), types.NamespacedName{
		Name:      ensureACIClusterName,
		Namespace: ensureACINamespace,
	}, updatedACI); err != nil {
		t.Fatalf("failed to get updated AgentClusterInstall: %v", err)
	}
	if updatedACI.Spec.IgnitionEndpoint == nil {
		t.Fatal("expected IgnitionEndpoint to be set")
	}
	expectedURL := "http://ensure-aci-cluster-mcs-proxy.infra-namespace.svc.cluster.local:8624/config/worker"
	if updatedACI.Spec.IgnitionEndpoint.Url != expectedURL {
		t.Fatalf("expected ignition URL %q, got %q", expectedURL, updatedACI.Spec.IgnitionEndpoint.Url)
	}

	caSecret := &corev1.Secret{}
	if err := managementClient.Get(context.Background(), types.NamespacedName{
		Name:      ensureACIClusterName + ignitionCASecretSuffix,
		Namespace: ensureACINamespace,
	}, caSecret); err != nil {
		t.Fatalf("failed to get ignition CA Secret: %v", err)
	}
	if string(caSecret.Data[ignitionCASecretKey]) != validCACertificate1 {
		t.Fatalf("expected infra root CA to be copied, got %q", caSecret.Data[ignitionCASecretKey])
	}

	rootCAConfigMap.Data[rootCAConfigMapKey] = validCACertificate2
	if err := infraClient.Update(context.Background(), rootCAConfigMap); err != nil {
		t.Fatalf("failed to update infra root CA ConfigMap: %v", err)
	}
	result, err = reconciler.ensureAgentClusterInstall(
		context.Background(),
		clusterDeployment,
		*oacp,
		cluster,
		infraResult,
	)
	if err != nil {
		t.Fatalf("ensureAgentClusterInstall() after remote CA rotation error = %v", err)
	}
	if result.RequeueAfter != remoteIgnitionCARefreshInterval {
		t.Fatalf("expected remote CA refresh requeue after rotation, got %#v", result)
	}
	if err := managementClient.Get(context.Background(), types.NamespacedName{
		Name:      ensureACIClusterName + ignitionCASecretSuffix,
		Namespace: ensureACINamespace,
	}, caSecret); err != nil {
		t.Fatalf("failed to get rotated ignition CA Secret: %v", err)
	}
	if string(caSecret.Data[ignitionCASecretKey]) != validCACertificate2 {
		t.Fatalf("expected rotated infra root CA, got %q", caSecret.Data[ignitionCASecretKey])
	}
}

func TestIgnitionCASecretToClusterDeployments(t *testing.T) {
	reconciler := &ClusterDeploymentReconciler{}
	ownerReference := metav1.OwnerReference{
		APIVersion: controlplanev1alpha3.GroupVersion.String(),
		Kind:       openshiftAssistedControlPlaneKind,
		Name:       ensureACIClusterName,
		UID:        "oacp-uid",
	}
	ignitionCASecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            ensureACIClusterName + ignitionCASecretSuffix,
			Namespace:       ensureACINamespace,
			OwnerReferences: []metav1.OwnerReference{ownerReference},
		},
	}

	requests := reconciler.ignitionCASecretToClusterDeployments(context.Background(), ignitionCASecret)
	if len(requests) != 1 {
		t.Fatalf("expected one ClusterDeployment request, got %#v", requests)
	}
	expectedRequest := types.NamespacedName{
		Name:      ensureACIClusterName,
		Namespace: ensureACINamespace,
	}
	if requests[0].NamespacedName != expectedRequest {
		t.Fatalf("unexpected ClusterDeployment request: %#v", requests[0])
	}

	unrelatedSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unrelated-ignition-ca",
			Namespace: ensureACINamespace,
		},
	}
	if requests := reconciler.ignitionCASecretToClusterDeployments(
		context.Background(),
		unrelatedSecret,
	); len(requests) != 0 {
		t.Fatalf("expected no requests for an unrelated Secret, got %#v", requests)
	}

	foreignSecret := ignitionCASecret.DeepCopy()
	foreignSecret.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: "example.com/v1",
		Kind:       "Example",
		Name:       "foreign-owner",
		UID:        "foreign-owner-uid",
	}}
	if requests := reconciler.ignitionCASecretToClusterDeployments(
		context.Background(),
		foreignSecret,
	); len(requests) != 0 {
		t.Fatalf("expected no requests for a foreign Secret, got %#v", requests)
	}
}

func TestEnsureAgentClusterInstallKubeVirtIgnitionCAPending(t *testing.T) {
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
		Spec: controlplanev1alpha3.OpenshiftAssistedControlPlaneSpec{
			Config: controlplanev1alpha3.OpenshiftAssistedControlPlaneConfigSpec{
				SSHAuthorizedKey: testSSHKey,
			},
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

	k8sClient := fakeclient.NewClientBuilder().WithScheme(scheme).WithObjects(oacp, aci).Build()
	reconciler := &ClusterDeploymentReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	result, err := reconciler.ensureAgentClusterInstall(context.Background(), clusterDeployment, *oacp, cluster, nil)
	if err != nil {
		t.Fatalf("ensureAgentClusterInstall() error = %v", err)
	}
	if result.RequeueAfter != 30*time.Second {
		t.Fatalf("expected 30s requeue, got %#v", result)
	}

	updatedACI := &hiveext.AgentClusterInstall{}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{
		Name:      ensureACIClusterName,
		Namespace: ensureACINamespace,
	}, updatedACI); err != nil {
		t.Fatalf("failed to get updated AgentClusterInstall: %v", err)
	}
	if updatedACI.Spec.SSHPublicKey != testSSHKey {
		t.Fatalf("expected unrelated ACI field to be updated, got %q", updatedACI.Spec.SSHPublicKey)
	}
	if updatedACI.Spec.IgnitionEndpoint != nil {
		t.Fatalf("expected IgnitionEndpoint not to be set, got %#v", updatedACI.Spec.IgnitionEndpoint)
	}

	serviceCAConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceCAConfigMapName,
			Namespace: ensureACINamespace,
		},
		Data: map[string]string{
			serviceCAConfigMapKey: validCACertificate1,
		},
	}
	if err := k8sClient.Create(context.Background(), serviceCAConfigMap); err != nil {
		t.Fatalf("failed to create service CA ConfigMap: %v", err)
	}

	result, err = reconciler.ensureAgentClusterInstall(context.Background(), clusterDeployment, *oacp, cluster, nil)
	if err != nil {
		t.Fatalf("ensureAgentClusterInstall() after CA recovery error = %v", err)
	}
	if !result.IsZero() {
		t.Fatalf("expected no requeue after CA recovery, got %#v", result)
	}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{
		Name:      ensureACIClusterName,
		Namespace: ensureACINamespace,
	}, updatedACI); err != nil {
		t.Fatalf("failed to get recovered AgentClusterInstall: %v", err)
	}
	if updatedACI.Spec.IgnitionEndpoint == nil || updatedACI.Spec.IgnitionEndpoint.CaCertificateReference == nil {
		t.Fatalf("expected IgnitionEndpoint CA reference to be set, got %#v", updatedACI.Spec.IgnitionEndpoint)
	}
}

func TestEnsureAgentClusterInstallKubeVirtLegacyIgnitionEndpointPending(t *testing.T) {
	tests := []struct {
		name             string
		existingEndpoint *hiveext.IgnitionEndpoint
		expectNil        bool
	}{
		{
			name: "clears the legacy CAPOA URL-only endpoint",
			existingEndpoint: &hiveext.IgnitionEndpoint{
				Url: "http://ensure-aci-cluster-mcs-proxy.ensure-aci.svc.cluster.local:8624/config/worker",
			},
			expectNil: true,
		},
		{
			name: "preserves a configured URL-only backend endpoint",
			existingEndpoint: &hiveext.IgnitionEndpoint{
				Url: "http://configured-mcs-proxy.ensure-aci.svc.cluster.local:8624/config/worker",
			},
			expectNil: false,
		},
		{
			name: "preserves an endpoint with a CA reference",
			existingEndpoint: &hiveext.IgnitionEndpoint{
				Url: "https://configured-mcs-proxy.ensure-aci.svc.cluster.local:8624/config/worker",
				CaCertificateReference: &hiveext.CaCertificateReference{
					Name:      "configured-ca",
					Namespace: ensureACINamespace,
				},
			},
			expectNil: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
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
				Spec: controlplanev1alpha3.OpenshiftAssistedControlPlaneSpec{
					Config: controlplanev1alpha3.OpenshiftAssistedControlPlaneConfigSpec{
						SSHAuthorizedKey: testSSHKey,
					},
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
					IgnitionEndpoint: test.existingEndpoint,
				},
			}

			k8sClient := fakeclient.NewClientBuilder().WithScheme(scheme).
				WithObjects(oacp, aci).Build()
			reconciler := &ClusterDeploymentReconciler{
				Client: k8sClient,
				Scheme: scheme,
			}

			result, err := reconciler.ensureAgentClusterInstall(context.Background(), clusterDeployment, *oacp, cluster, nil)
			if err != nil {
				t.Fatalf("ensureAgentClusterInstall() error = %v", err)
			}
			if result.RequeueAfter != 30*time.Second {
				t.Fatalf("expected 30s requeue, got %#v", result)
			}

			updatedACI := &hiveext.AgentClusterInstall{}
			if err := k8sClient.Get(context.Background(), types.NamespacedName{
				Name:      ensureACIClusterName,
				Namespace: ensureACINamespace,
			}, updatedACI); err != nil {
				t.Fatalf("failed to get updated AgentClusterInstall: %v", err)
			}
			if updatedACI.Spec.SSHPublicKey != testSSHKey {
				t.Fatalf("expected unrelated ACI field to be updated, got %q", updatedACI.Spec.SSHPublicKey)
			}
			if test.expectNil && updatedACI.Spec.IgnitionEndpoint != nil {
				t.Fatalf("expected legacy IgnitionEndpoint to be removed, got %#v", updatedACI.Spec.IgnitionEndpoint)
			}
			if !test.expectNil {
				updatedEndpoint := updatedACI.Spec.IgnitionEndpoint
				expectedEndpoint := test.existingEndpoint
				if updatedEndpoint == nil {
					t.Fatal("expected configured IgnitionEndpoint to be preserved, got nil")
				}
				updatedCAReference := updatedEndpoint.CaCertificateReference
				expectedCAReference := expectedEndpoint.CaCertificateReference
				if updatedEndpoint.Url != expectedEndpoint.Url ||
					(updatedCAReference == nil) != (expectedCAReference == nil) ||
					(updatedCAReference != nil && expectedCAReference != nil &&
						(updatedCAReference.Name != expectedCAReference.Name ||
							updatedCAReference.Namespace != expectedCAReference.Namespace)) {
					t.Fatalf(
						"expected configured IgnitionEndpoint to be preserved, got %#v",
						updatedEndpoint,
					)
				}
			}
		})
	}
}

func TestEnsureKubeVirtIgnitionCARejectsForeignSecret(t *testing.T) {
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
			UID:       "oacp-uid",
		},
		Spec: controlplanev1alpha3.OpenshiftAssistedControlPlaneSpec{
			Config: controlplanev1alpha3.OpenshiftAssistedControlPlaneConfigSpec{
				SSHAuthorizedKey: testSSHKey,
			},
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
			serviceCAConfigMapKey: validCACertificate1,
		},
	}
	foreignOwnerReference := metav1.OwnerReference{
		APIVersion: controlplanev1alpha3.GroupVersion.String(),
		Kind:       openshiftAssistedControlPlaneKind,
		Name:       ensureACIClusterName,
		UID:        "stale-oacp-uid",
	}
	foreignSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            ensureACIClusterName + "-ignition-ca",
			Namespace:       ensureACINamespace,
			UID:             "foreign-secret-uid",
			OwnerReferences: []metav1.OwnerReference{foreignOwnerReference},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			ignitionCASecretKey: []byte("foreign-ca"),
		},
	}

	k8sClient := fakeclient.NewClientBuilder().WithScheme(scheme).
		WithObjects(oacp, aci, serviceCAConfigMap, foreignSecret).Build()
	reconciler := &ClusterDeploymentReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	result, err := reconciler.ensureAgentClusterInstall(context.Background(), clusterDeployment, *oacp, cluster, nil)
	if err != nil {
		t.Fatalf("ensureAgentClusterInstall() error = %v", err)
	}
	if result.RequeueAfter != 30*time.Second {
		t.Fatalf("expected 30s requeue, got %#v", result)
	}

	updatedACI := &hiveext.AgentClusterInstall{}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{
		Name:      ensureACIClusterName,
		Namespace: ensureACINamespace,
	}, updatedACI); err != nil {
		t.Fatalf("failed to get updated AgentClusterInstall: %v", err)
	}
	if updatedACI.Spec.SSHPublicKey != testSSHKey {
		t.Fatalf("expected unrelated ACI field to be updated, got %q", updatedACI.Spec.SSHPublicKey)
	}
	if updatedACI.Spec.IgnitionEndpoint != nil {
		t.Fatalf("expected IgnitionEndpoint not to be set, got %#v", updatedACI.Spec.IgnitionEndpoint)
	}

	updatedSecret := &corev1.Secret{}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{
		Name:      foreignSecret.Name,
		Namespace: foreignSecret.Namespace,
	}, updatedSecret); err != nil {
		t.Fatalf("failed to get foreign Secret: %v", err)
	}
	if string(updatedSecret.Data[ignitionCASecretKey]) != "foreign-ca" {
		t.Fatalf("expected foreign Secret data to be preserved, got %q", updatedSecret.Data[ignitionCASecretKey])
	}
	if updatedSecret.Type != corev1.SecretTypeTLS {
		t.Fatalf("expected foreign Secret type to be preserved, got %q", updatedSecret.Type)
	}
	if len(updatedSecret.OwnerReferences) != 1 || updatedSecret.OwnerReferences[0] != foreignOwnerReference {
		t.Fatalf("expected foreign Secret owner to be preserved, got %#v", updatedSecret.OwnerReferences)
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

	result, err := reconciler.ensureAgentClusterInstall(context.Background(), clusterDeployment, *oacp, cluster, nil)
	if err != nil {
		t.Fatalf("ensureAgentClusterInstall() error = %v", err)
	}
	if !result.IsZero() {
		t.Fatalf("expected no requeue, got %#v", result)
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
