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

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const kubevirtClusterKind = "KubevirtCluster"

// IsKubeVirtInfra returns true if the CAPI Cluster uses a KubevirtCluster
// as its infrastructure provider. This is determined by inspecting the
// Cluster's infrastructureRef — no explicit platform field is needed.
func IsKubeVirtInfra(cluster *clusterv1.Cluster) bool {
	return cluster != nil && cluster.Spec.InfrastructureRef.Kind == kubevirtClusterKind
}

// InfraClientResult holds the infra cluster client and namespace.
type InfraClientResult struct {
	Client    client.Client
	Namespace string
	IsRemote  bool
}

// GetInfraClusterClient builds a controller-runtime client for the infra cluster
// by reading the kubeconfig from the Secret referenced by spec.config.infraClusterRef.
//
// If infraClusterRef is not set, returns the local client (same-cluster topology
// where management cluster IS the infra cluster).
//
// The infra namespace is determined from the kubeconfig's current context namespace,
// falling back to the OACP's own namespace.
func GetInfraClusterClient(
	ctx context.Context,
	c client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	scheme *runtime.Scheme,
) (*InfraClientResult, error) {
	log := ctrl.LoggerFrom(ctx)
	namespace := oacp.Namespace

	// If no infra cluster ref is configured, use local client (same-cluster topology)
	if oacp.Spec.Config.InfraClusterRef == nil {
		log.V(1).Info("no infraClusterRef configured, using local client")
		return &InfraClientResult{Client: c, Namespace: namespace, IsRemote: false}, nil
	}

	// Read the kubeconfig from the referenced secret
	secretName := oacp.Spec.Config.InfraClusterRef.Name
	secret := &corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{Name: secretName, Namespace: namespace}, secret); err != nil {
		return nil, fmt.Errorf("failed to get infra cluster secret %s/%s: %w", namespace, secretName, err)
	}

	kubeconfigData, ok := secret.Data["kubeconfig"]
	if !ok {
		return nil, fmt.Errorf("infra cluster secret %s/%s does not contain 'kubeconfig' key", namespace, secretName)
	}

	// Build the REST config
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse infra kubeconfig from secret %s/%s: %w", namespace, secretName, err)
	}

	// Determine infra namespace from the kubeconfig context
	infraNamespace := namespace
	kubeConfig, err := clientcmd.Load(kubeconfigData)
	if err == nil && kubeConfig.CurrentContext != "" {
		if ctxObj, ok := kubeConfig.Contexts[kubeConfig.CurrentContext]; ok && ctxObj.Namespace != "" {
			infraNamespace = ctxObj.Namespace
		}
	}

	// Build the controller-runtime client
	infraClient, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create infra cluster client: %w", err)
	}

	log.Info("created infra cluster client from infraClusterRef",
		"secret", secretName, "infraNamespace", infraNamespace)
	return &InfraClientResult{Client: infraClient, Namespace: infraNamespace, IsRemote: true}, nil
}

// EnsurePullSecretOnInfra copies the pull secret from the management cluster to the
// infra cluster namespace so that Jobs running on the infra cluster can pull OCP release images.
func EnsurePullSecretOnInfra(
	ctx context.Context,
	localClient client.Client,
	infraClient client.Client,
	localNamespace string,
	infraNamespace string,
	secretName string,
) error {
	if secretName == "" {
		return nil
	}

	// Check if it already exists on infra
	existing := &corev1.Secret{}
	err := infraClient.Get(ctx, client.ObjectKey{Name: secretName, Namespace: infraNamespace}, existing)
	if err == nil {
		return nil
	}
	if client.IgnoreNotFound(err) != nil {
		return err
	}

	// Read from management cluster
	source := &corev1.Secret{}
	if err := localClient.Get(ctx, client.ObjectKey{Name: secretName, Namespace: localNamespace}, source); err != nil {
		return fmt.Errorf("failed to get pull secret %s/%s from management cluster: %w", localNamespace, secretName, err)
	}

	// Create on infra cluster
	target := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: infraNamespace,
			Labels: map[string]string{
				"capoa.openshift.io/managed-by": "capoa-controlplane",
			},
		},
		Type: source.Type,
		Data: source.Data,
	}
	if err := infraClient.Create(ctx, target); err != nil {
		if client.IgnoreAlreadyExists(err) == nil {
			return nil
		}
		return fmt.Errorf("failed to create pull secret on infra cluster: %w", err)
	}
	return nil
}
