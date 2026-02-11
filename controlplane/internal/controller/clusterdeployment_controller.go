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

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/imageregistry"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/release"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util"
	logutil "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util/log"

	configv1 "github.com/openshift/api/config/v1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	capiutil "sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	InstallConfigOverrides             = aiv1beta1.Group + "/install-config-overrides"
	defaultBaremetalBaselineCapability = "None"
	defaultBaselineCapability          = "vCurrent"
)

var (
	defaultBaremetalAdditionalCapabilities = []configv1.ClusterVersionCapability{"baremetal", "Console", "Insights", "OperatorLifecycleManager", "Ingress", "marketplace", "NodeTuning", "DeploymentConfig"}
)

// ClusterDeploymentReconciler reconciles a ClusterDeployment object
type ClusterDeploymentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Only watch ClusterDeployments that have the CAPI cluster label
	clusterLabelPredicate, err := predicate.LabelSelectorPredicate(metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      clusterv1.ClusterNameLabel,
				Operator: metav1.LabelSelectorOpExists,
			},
		},
	})
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&hivev1.ClusterDeployment{}).
		WithEventFilter(clusterLabelPredicate).
		Watches(&controlplanev1alpha3.OpenshiftAssistedControlPlane{}, &handler.EnqueueRequestForObject{}).
		Watches(&clusterv1.MachineDeployment{}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}

func (r *ClusterDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	clusterDeployment := &hivev1.ClusterDeployment{}
	if err := r.Get(ctx, req.NamespacedName, clusterDeployment); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	log.WithValues("cluster_deployment", clusterDeployment.Name, "cluster_deployment_namespace", clusterDeployment.Namespace)
	log.V(logutil.DebugLevel).Info("reconciling ClusterDeployment")

	acp := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: clusterDeployment.Namespace, Name: clusterDeployment.Name}, acp); err != nil {
		log.V(logutil.DebugLevel).Info("OpenshiftAssistedControlPlane not found for ClusterDeployment", "clusterDeployment", clusterDeployment.Name)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	log.WithValues("openshiftassisted_control_plane", acp.Name, "openshiftassisted_control_plane_namespace", acp.Namespace)

	arch, err := getArchitectureFromBootstrapConfigs(ctx, r.Client, acp)
	if err != nil {
		return ctrl.Result{}, err
	}
	if err = ensureClusterImageSet(ctx, r.Client, clusterDeployment.Name, getReleaseImage(*acp, arch)); err != nil {
		log.Error(err, "failed creating ClusterImageSet")
		return ctrl.Result{}, err
	}

	if acp.Spec.Config.ImageRegistryRef != nil {
		if err := r.createImageRegistry(ctx, acp.Spec.Config.ImageRegistryRef.Name, acp.Namespace); err != nil {
			log.Error(err, "failed to create image registry config manifest")
			return ctrl.Result{}, err
		}
	}

	if err := r.ensureAgentClusterInstall(ctx, clusterDeployment, *acp); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, r.updateClusterDeploymentRef(ctx, clusterDeployment)
}

func (r *ClusterDeploymentReconciler) ensureAgentClusterInstall(
	ctx context.Context,
	clusterDeployment *hivev1.ClusterDeployment,
	oacp controlplanev1alpha3.OpenshiftAssistedControlPlane,
) error {
	log := ctrl.LoggerFrom(ctx)

	cluster, err := capiutil.GetOwnerCluster(ctx, r.Client, oacp.ObjectMeta)
	if err != nil {
		log.Error(err, "failed to retrieve owner Cluster from the API Server")
		return err
	}

	workerNodes := r.getWorkerNodesCount(ctx, cluster)
	clusterNetwork, serviceNetwork := getClusterNetworks(cluster)
	additionalManifests := getClusterAdditionalManifestRefs(oacp)

	aci := &hiveext.AgentClusterInstall{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterDeployment.Name,
			Namespace: clusterDeployment.Namespace,
		},
	}
	mutate := func() error {
		aci.Labels = util.ControlPlaneMachineLabelsForCluster(
			&oacp,
			clusterDeployment.Labels[clusterv1.ClusterNameLabel],
		)
		aci.Labels[hiveext.ClusterConsumerLabel] = openshiftAssistedControlPlaneKind

		if err := controllerutil.SetOwnerReference(&oacp, aci, r.Scheme); err != nil {
			log.V(logutil.WarningLevel).Info("failed to set owner reference on AgentClusterInstall", "error", err.Error())
		}

		aci.Spec.ClusterDeploymentRef = corev1.LocalObjectReference{Name: clusterDeployment.Name}
		aci.Spec.PlatformType = hiveext.PlatformType(configv1.NonePlatformType)
		aci.Spec.ProvisionRequirements = hiveext.ProvisionRequirements{
			ControlPlaneAgents: int(oacp.Spec.Replicas),
			WorkerAgents:       workerNodes,
		}
		aci.Spec.DiskEncryption = oacp.Spec.Config.DiskEncryption
		aci.Spec.MastersSchedulable = oacp.Spec.Config.MastersSchedulable
		aci.Spec.Proxy = oacp.Spec.Config.Proxy
		aci.Spec.SSHPublicKey = oacp.Spec.Config.SSHAuthorizedKey
		aci.Spec.ImageSetRef = &hivev1.ClusterImageSetReference{Name: clusterDeployment.Name}
		aci.Spec.Networking.ClusterNetwork = clusterNetwork
		aci.Spec.Networking.ServiceNetwork = serviceNetwork
		aci.Spec.ManifestsConfigMapRefs = additionalManifests

		if len(oacp.Spec.Config.APIVIPs) > 0 && len(oacp.Spec.Config.IngressVIPs) > 0 {
			aci.Spec.APIVIPs = oacp.Spec.Config.APIVIPs
			aci.Spec.IngressVIPs = oacp.Spec.Config.IngressVIPs
			aci.Spec.PlatformType = hiveext.PlatformType(configv1.BareMetalPlatformType)
		}
		installConfigOverride, err := getInstallConfigOverride(&oacp, aci)
		if err != nil {
			return err
		}
		if installConfigOverride != "" {
			if aci.Annotations == nil {
				aci.Annotations = make(map[string]string)
			}
			aci.Annotations[InstallConfigOverrides] = installConfigOverride
		}

		return nil
	}

	if _, err = controllerutil.CreateOrPatch(ctx, r.Client, aci, mutate); err != nil {
		log.Error(err, "failed to create or update AgentClusterInstall")
		return err
	}

	return nil
}

// Returns release image from OpenshiftAssistedControlPlane. It will compute it starting from Spec.DistributionVersion and
// possibly cluster.x-k8s.io/release-image-repository-override annotation.
// Expected patterns:
// quay.io/openshift-release-dev/ocp-release:4.17.0-rc.2-x86_64
// quay.io/okd/scos-release:4.18.0-okd-scos.ec.1
// Can be overridden with annotation: cluster.x-k8s.io/release-image-repository-override=quay.io/myorg/myrepo
func getReleaseImage(oacp controlplanev1alpha3.OpenshiftAssistedControlPlane, architecture string) string {
	releaseImageRepository, ok := oacp.Annotations[release.ReleaseImageRepositoryOverrideAnnotation]
	if !ok {
		releaseImageRepository = ""
	}
	return release.GetReleaseImage(oacp.Spec.DistributionVersion, releaseImageRepository, architecture)
}

func (r *ClusterDeploymentReconciler) getWorkerNodesCount(ctx context.Context, cluster *clusterv1.Cluster) int {
	log := ctrl.LoggerFrom(ctx)
	count := 0

	mdList := clusterv1.MachineDeploymentList{}
	if err := r.List(ctx, &mdList, client.MatchingLabels{clusterv1.ClusterNameLabel: cluster.Name}); err != nil {
		log.Error(err, "failed to list MachineDeployments", "cluster", cluster.Name)
		return count
	}

	for _, md := range mdList.Items {
		count += int(*md.Spec.Replicas)
	}
	return count
}

func (r *ClusterDeploymentReconciler) updateClusterDeploymentRef(
	ctx context.Context,
	cd *hivev1.ClusterDeployment,
) error {
	expectedRef := &hivev1.ClusterInstallLocalReference{
		Group:   hiveext.Group,
		Version: hiveext.Version,
		Kind:    "AgentClusterInstall",
		Name:    cd.Name,
	}

	// Skip update if ClusterInstallRef is already correctly set.
	// This avoids triggering Hive's immutability validation for spec.clusterMetadata
	// which rejects any update to ClusterDeployment after installation.
	if cd.Spec.ClusterInstallRef != nil &&
		cd.Spec.ClusterInstallRef.Group == expectedRef.Group &&
		cd.Spec.ClusterInstallRef.Version == expectedRef.Version &&
		cd.Spec.ClusterInstallRef.Kind == expectedRef.Kind &&
		cd.Spec.ClusterInstallRef.Name == expectedRef.Name {
		return nil
	}

	cd.Spec.ClusterInstallRef = expectedRef
	return r.Update(ctx, cd)
}

func ensureClusterImageSet(ctx context.Context, c client.Client, imageSetName string, releaseImage string) error {
	imageSet := &hivev1.ClusterImageSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: imageSetName,
		},
	}

	_, err := controllerutil.CreateOrPatch(ctx, c, imageSet, func() error {
		imageSet.Spec.ReleaseImage = releaseImage
		return nil
	})

	return err
}

func getClusterNetworks(cluster *clusterv1.Cluster) ([]hiveext.ClusterNetworkEntry, []string) {
	clusterNetwork := make([]hiveext.ClusterNetworkEntry, 0, len(cluster.Spec.ClusterNetwork.Pods.CIDRBlocks))
	for _, cidrBlock := range cluster.Spec.ClusterNetwork.Pods.CIDRBlocks {
		clusterNetwork = append(clusterNetwork, hiveext.ClusterNetworkEntry{CIDR: cidrBlock, HostPrefix: 23})
	}

	return clusterNetwork, cluster.Spec.ClusterNetwork.Services.CIDRBlocks
}

func getClusterAdditionalManifestRefs(acp controlplanev1alpha3.OpenshiftAssistedControlPlane) []hiveext.ManifestsConfigMapReference {
	var additionalManifests []hiveext.ManifestsConfigMapReference
	if len(acp.Spec.Config.ManifestsConfigMapRefs) > 0 {
		additionalManifests = append(additionalManifests, acp.Spec.Config.ManifestsConfigMapRefs...)
	}

	if acp.Spec.Config.ImageRegistryRef != nil {
		additionalManifests = append(additionalManifests, hiveext.ManifestsConfigMapReference{Name: imageregistry.ImageConfigMapName})
	}

	return additionalManifests
}

func (r *ClusterDeploymentReconciler) createImageRegistry(ctx context.Context, registryName, registryNamespace string) error {
	registryConfigmap := &corev1.ConfigMap{}
	if err := r.Get(ctx, client.ObjectKey{Name: registryName, Namespace: registryNamespace}, registryConfigmap); err != nil {
		return err
	}

	spokeImageRegistryData, err := imageregistry.GenerateImageRegistryData(registryConfigmap, registryNamespace)
	if err != nil {
		return err
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      imageregistry.ImageConfigMapName,
			Namespace: registryNamespace,
		},
	}
	_, err = controllerutil.CreateOrPatch(ctx, r.Client, cm, func() error {
		cm.Data = spokeImageRegistryData
		return nil
	})

	return err
}

type InstallConfigOverride struct {
	Capability configv1.ClusterVersionCapabilitiesSpec `json:"capabilities,omitempty"`
}

// isBaremetalPlatform checks if the AgentClusterInstall is configured for a baremetal platform.
// Returns true if the platform type is BareMetalPlatformType, false otherwise.
func isBaremetalPlatform(aci *hiveext.AgentClusterInstall) bool {
	return aci.Spec.PlatformType == hiveext.BareMetalPlatformType
}

// getInstallConfigOverride merges install config override from annotations with capabilities-based overrides.
// It returns the final merged install config override as a JSON string, or empty string if no overrides are needed.
// The function combines user-provided install config overrides from annotations with automatically
// generated capabilities configuration based on the cluster platform type.
func getInstallConfigOverride(oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane, aci *hiveext.AgentClusterInstall) (string, error) {
	installConfigOverrideStr := oacp.Annotations[controlplanev1alpha3.InstallConfigOverrideAnnotation]

	capabilitiesCfgOverride, err := getInstallConfigOverrideForCapabilities(oacp, aci)
	if err != nil {
		return "", err
	}
	// if no override and no capabilities, no need to merge install config override
	if installConfigOverrideStr == "" && capabilitiesCfgOverride == "" {
		return "", nil
	}
	if capabilitiesCfgOverride == "" {
		return installConfigOverrideStr, nil
	}
	if installConfigOverrideStr == "" {
		return capabilitiesCfgOverride, nil
	}

	installConfigOverrideStr, err = mergeJson(installConfigOverrideStr, capabilitiesCfgOverride)
	if err != nil {
		return "", err
	}
	return installConfigOverrideStr, nil
}

// getInstallConfigOverrideForCapabilities generates install config override JSON for OpenShift capabilities configuration.
// It automatically sets appropriate capabilities based on the platform type:
// - Baremetal platforms: Sets baseline to "None" and includes default baremetal capabilities
// - Non-baremetal platforms: Sets baseline to "vCurrent" and only includes user-specified capabilities
// Returns empty string if no capabilities configuration is needed (non-baremetal with empty capabilities).
func getInstallConfigOverrideForCapabilities(oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane, aci *hiveext.AgentClusterInstall) (string, error) {
	var installCfgOverride InstallConfigOverride
	if isCapabilitiesEmpty(oacp.Spec.Config.Capabilities) && !isBaremetalPlatform(aci) {
		return "", nil
	}
	baselineCapability, err := getBaselineCapability(oacp.Spec.Config.Capabilities.BaselineCapability, isBaremetalPlatform(aci))
	if err != nil {
		return "", err
	}
	installCfgOverride.Capability.BaselineCapabilitySet = configv1.ClusterVersionCapabilitySet(baselineCapability)

	additionalEnabledCapabilities := getAdditionalCapabilities(oacp.Spec.Config.Capabilities.AdditionalEnabledCapabilities, isBaremetalPlatform(aci))
	installCfgOverride.Capability.AdditionalEnabledCapabilities = additionalEnabledCapabilities

	installCfgOverrideBytes, err := json.Marshal(installCfgOverride)
	if err != nil {
		return "", err
	}
	installCfgOverrideStr := string(installCfgOverrideBytes)
	return installCfgOverrideStr, nil
}

// mergeJson merges two JSON strings by unmarshaling them into maps and combining them.
// The second JSON takes precedence over the first in case of key conflicts.
// Returns the merged JSON as a string, error if unmarshalling or marshalling fails.
func mergeJson(json1, json2 string) (string, error) {
	var merged map[string]interface{}
	if err := json.Unmarshal([]byte(json1), &merged); err != nil {
		return "", fmt.Errorf("failed to unmarshal json1: %w", err)
	}
	if err := json.Unmarshal([]byte(json2), &merged); err != nil {
		return "", fmt.Errorf("failed to unmarshal json2: %w", err)
	}
	mergedJson, err := json.Marshal(merged)
	if err != nil {
		return "", fmt.Errorf("failed to marshal merged json: %w", err)
	}
	return string(mergedJson), nil
}

func getBaselineCapability(capability string, isBaremetalPlatform bool) (string, error) {
	baselineCapability := capability
	if baselineCapability == "None" || baselineCapability == "vCurrent" {
		return baselineCapability, nil
	}

	if baselineCapability == "" {
		baselineCapability = defaultBaselineCapability
		if isBaremetalPlatform {
			baselineCapability = defaultBaremetalBaselineCapability
		}
		return baselineCapability, nil
	}

	var baselineCapabilityRegexp = regexp.MustCompile(`v4\.[0-9]+`)
	if !baselineCapabilityRegexp.MatchString(baselineCapability) {
		return "",
			fmt.Errorf("invalid baseline capability set, must be one of: None, vCurrent, or v4.x. Got: [%s]", baselineCapability)
	}
	return baselineCapability, nil
}

func getAdditionalCapabilities(specifiedAdditionalCapabilities []string, isBaremetalPlatform bool) []configv1.ClusterVersionCapability {
	additionalCapabilitiesList := []configv1.ClusterVersionCapability{}
	if isBaremetalPlatform {
		additionalCapabilitiesList = append([]configv1.ClusterVersionCapability{}, defaultBaremetalAdditionalCapabilities...)
	}

	for _, capability := range specifiedAdditionalCapabilities {
		// Ignore MAPI for baremetal MNO clusters and ignore duplicates
		if (strings.EqualFold(capability, "MachineAPI") && isBaremetalPlatform) || slices.Contains(additionalCapabilitiesList, configv1.ClusterVersionCapability(capability)) {
			continue
		}
		additionalCapabilitiesList = append(additionalCapabilitiesList, configv1.ClusterVersionCapability(capability))
	}

	if len(additionalCapabilitiesList) < 1 {
		return nil
	}

	return additionalCapabilitiesList
}

func isCapabilitiesEmpty(capabilities controlplanev1alpha3.Capabilities) bool {
	return equality.Semantic.DeepEqual(capabilities, controlplanev1alpha3.Capabilities{})
}
