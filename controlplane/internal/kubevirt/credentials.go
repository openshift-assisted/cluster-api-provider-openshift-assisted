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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	InfraCredentialsCMName = "kubevirt-infra-credentials-manifests"
)

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
	c client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
) error {
	needsCredentials := (oacp.Spec.Config.CSIDriver != nil && oacp.Spec.Config.CSIDriver.Enabled) ||
		(oacp.Spec.Config.CloudControllerManager != nil && oacp.Spec.Config.CloudControllerManager.Enabled)
	if !needsCredentials {
		return nil
	}

	credSecretName := "infra-cluster-credentials"
	if oacp.Spec.Config.InfraClusterRef != nil {
		credSecretName = oacp.Spec.Config.InfraClusterRef.Name
	}

	// Read the source secret from the OACP namespace
	sourceSecret := &corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{
		Name:      credSecretName,
		Namespace: oacp.Namespace,
	}, sourceSecret); err != nil {
		return fmt.Errorf("failed to get infra credentials secret %s/%s: %w",
			oacp.Namespace, credSecretName, err)
	}

	kubeconfigData := string(sourceSecret.Data["kubeconfig"])
	if kubeconfigData == "" {
		return fmt.Errorf("infra credentials secret %s does not contain 'kubeconfig' key", credSecretName)
	}

	infraNS := oacp.Namespace

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
`, csiCredSecretName, infraNS, indentMultiline(kubeconfigData)),
		})
	}

	if len(manifests) == 0 {
		// Even when CCM/CSI are not enabled, if InfraClusterCredentials is set,
		// create an empty ConfigMap so that ACI's manifestsConfigMapRefs doesn't
		// fail with "ConfigMap not found".
		return ensureManifestsConfigMap(ctx, c, oacp, InfraCredentialsCMName, []ManifestEntry{
			{Filename: "placeholder.yaml", Content: "# No infra credentials manifests needed\n"},
		})
	}

	return ensureManifestsConfigMap(ctx, c, oacp, InfraCredentialsCMName, manifests)
}
