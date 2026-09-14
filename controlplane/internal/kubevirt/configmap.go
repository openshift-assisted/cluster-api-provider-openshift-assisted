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
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/pkg/containers"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// EnsureKubeVirtManifests creates the ConfigMaps containing the operator manifests
// that will be injected into the tenant cluster during installation, and returns
// the manifest references to include in AgentClusterInstall.
//
// serviceIPs provides the ClusterIPs of the API and Ingress services, used to
// configure the DNS proxy so api-int resolves to the service ClusterIP (enabling
// MCS access during installation through the service).
//
// releaseImage and pullSecret are used to resolve component images from the OCP
// release payload (CCM, CSI, ose-cli).
func EnsureKubeVirtManifests(
	ctx context.Context,
	c client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	infraNamespace string,
	serviceIPs *ServiceIPs,
	oseCliImage string,
	releaseImage string,
	pullSecret []byte,
	remoteImage containers.RemoteImage,
	infraCoreDNSIP string,
) ([]hiveext.ManifestsConfigMapReference, error) {
	var allRefs []hiveext.ManifestsConfigMapReference

	if oacp.Spec.Config.CloudControllerManager != nil && oacp.Spec.Config.CloudControllerManager.Enabled {
		ccmImage, err := ResolveImageFromPayload(releaseImage, pullSecret, "kubevirt-cloud-controller-manager", remoteImage)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve CCM image from release payload: %w", err)
		}
		if ccmImage == "" {
			return nil, fmt.Errorf("CCM image resolved to empty string from release payload")
		}
		ccmManifests := GenerateCCMManifests(oacp, infraNamespace, ccmImage, oseCliImage)
		if len(ccmManifests) > 0 {
			if err := ensureManifestsConfigMap(ctx, c, oacp, CCMManifestsConfigMapName, ccmManifests); err != nil {
				return nil, fmt.Errorf("failed to create CCM manifests ConfigMap: %w", err)
			}
			allRefs = append(allRefs, hiveext.ManifestsConfigMapReference{Name: CCMManifestsConfigMapName})
		}
	}

	if oacp.Spec.Config.CSIDriver != nil && oacp.Spec.Config.CSIDriver.Enabled {
		csiImages := CSIImages{}
		csiPayloadComponents := map[string]*string{
			PayloadCSIDriver:        &csiImages.Driver,
			PayloadCSIProvisioner:   &csiImages.Provisioner,
			PayloadCSIAttacher:      &csiImages.Attacher,
			PayloadCSISnapshotter:   &csiImages.Snapshotter,
			PayloadCSIResizer:       &csiImages.Resizer,
			PayloadCSILivenessProbe: &csiImages.LivenessProbe,
			PayloadCSINodeRegistrar: &csiImages.NodeRegistrar,
		}
		for component, target := range csiPayloadComponents {
			img, err := ResolveImageFromPayload(releaseImage, pullSecret, component, remoteImage)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve CSI image %s from release payload: %w", component, err)
			}
			*target = img
		}

		csiManifests := GenerateCSIManifests(oacp, infraNamespace, csiImages, oseCliImage)
		if len(csiManifests) > 0 {
			if err := ensureManifestsConfigMap(ctx, c, oacp, CSIManifestsConfigMapName, csiManifests); err != nil {
				return nil, fmt.Errorf("failed to create CSI manifests ConfigMap: %w", err)
			}
			allRefs = append(allRefs, hiveext.ManifestsConfigMapReference{Name: CSIManifestsConfigMapName})
		}
	}

	if IsBridgeNetworking(oacp.Spec.Config.APIVIPs, oacp.Spec.Config.IngressVIPs) {
		resolvFixManifests := GenerateResolvFixManifests()
		if len(resolvFixManifests) > 0 {
			if err := ensureManifestsConfigMap(ctx, c, oacp, ResolvFixManifestsConfigMapName, resolvFixManifests); err != nil {
				return nil, fmt.Errorf("failed to create resolv fix manifests ConfigMap: %w", err)
			}
			allRefs = append(allRefs, hiveext.ManifestsConfigMapReference{Name: ResolvFixManifestsConfigMapName})
		}
	}

	clusterName := oacp.Spec.Config.ClusterName
	if clusterName == "" {
		clusterName = oacp.Name
	}

	refs, err := ensurePodNetworkingManifests(ctx, c, oacp, serviceIPs, infraCoreDNSIP, clusterName)
	if err != nil {
		return nil, err
	}
	allRefs = append(allRefs, refs...)

	return allRefs, nil
}

// ensurePodNetworkingManifests creates ConfigMaps for pod-networking specific
// manifests (DNS fix, MTU, DNS forwarder, DNS proxy, MCS NodePort).
func ensurePodNetworkingManifests(
	ctx context.Context,
	c client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	serviceIPs *ServiceIPs,
	infraCoreDNSIP string,
	clusterName string,
) ([]hiveext.ManifestsConfigMapReference, error) {
	var refs []hiveext.ManifestsConfigMapReference
	podNet := IsPodNetworking(oacp.Spec.Config.APIVIPs, oacp.Spec.Config.IngressVIPs)

	if podNet {
		apiClusterIP := ""
		if serviceIPs != nil {
			apiClusterIP = serviceIPs.APIClusterIP
		}
		podNetDNSFixManifests := GeneratePodNetworkDNSFixManifests(clusterName, oacp.Spec.Config.BaseDomain, apiClusterIP, infraCoreDNSIP)
		if len(podNetDNSFixManifests) > 0 {
			if err := ensureManifestsConfigMap(ctx, c, oacp, PodNetDNSFixManifestsConfigMapName, podNetDNSFixManifests); err != nil {
				return nil, fmt.Errorf("failed to create pod network DNS fix manifests ConfigMap: %w", err)
			}
			refs = append(refs, hiveext.ManifestsConfigMapReference{Name: PodNetDNSFixManifestsConfigMapName})
		}
	}

	networkMTUManifests := GenerateNetworkMTUManifests()
	if len(networkMTUManifests) > 0 {
		if err := ensureManifestsConfigMap(ctx, c, oacp, NetworkMTUConfigMapName, networkMTUManifests); err != nil {
			return nil, fmt.Errorf("failed to create network MTU manifests ConfigMap: %w", err)
		}
		refs = append(refs, hiveext.ManifestsConfigMapReference{Name: NetworkMTUConfigMapName})
	}

	var apiIPs, ingressIPs []string
	if serviceIPs != nil {
		if serviceIPs.APIClusterIP != "" {
			apiIPs = []string{serviceIPs.APIClusterIP}
		}
		if serviceIPs.IngressClusterIP != "" {
			ingressIPs = []string{serviceIPs.IngressClusterIP}
		}
	}

	dnsProxyManifests := GenerateDNSProxyManifestsWithIPs(
		clusterName,
		oacp.Spec.Config.BaseDomain,
		oacp.Namespace,
		infraCoreDNSIP,
		apiIPs,
		ingressIPs,
	)
	if len(dnsProxyManifests) > 0 {
		if err := ensureManifestsConfigMap(ctx, c, oacp, DNSProxyConfigMapName, dnsProxyManifests); err != nil {
			return nil, fmt.Errorf("failed to create DNS proxy manifests ConfigMap: %w", err)
		}
	}

	if podNet {
		tenantDNSManifests := GenerateTenantDNSForwarderManifests(
			fmt.Sprintf("%s.%s", clusterName, oacp.Spec.Config.BaseDomain),
			nil,
		)
		if len(tenantDNSManifests) > 0 {
			if err := ensureManifestsConfigMap(ctx, c, oacp, TenantDNSFwdConfigName, tenantDNSManifests); err != nil {
				return nil, fmt.Errorf("failed to create tenant DNS forwarder manifests ConfigMap: %w", err)
			}
			refs = append(refs, hiveext.ManifestsConfigMapReference{Name: TenantDNSFwdConfigName})
		}
	}

	if podNet {
		mcsManifests := GenerateMCSNodePortManifests()
		if len(mcsManifests) > 0 {
			if err := ensureManifestsConfigMap(ctx, c, oacp, MCSManifestsConfigName, mcsManifests); err != nil {
				return nil, fmt.Errorf("failed to create MCS manifests ConfigMap: %w", err)
			}
			refs = append(refs, hiveext.ManifestsConfigMapReference{Name: MCSManifestsConfigName})
		}
	}

	return refs, nil
}

func ensureManifestsConfigMap(
	ctx context.Context,
	c client.Client,
	owner *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	name string,
	manifests []ManifestEntry,
) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: owner.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, c, cm, func() error {
		if cm.Data == nil {
			cm.Data = make(map[string]string)
		}
		for _, m := range manifests {
			cm.Data[m.Filename] = m.Content
		}
		return controllerutil.SetOwnerReference(owner, cm, c.Scheme())
	})
	return err
}
