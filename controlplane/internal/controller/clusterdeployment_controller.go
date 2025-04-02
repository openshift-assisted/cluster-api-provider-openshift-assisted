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

	controlplanev1alpha2 "github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1alpha2"
	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/imageregistry"
	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/release"
	"github.com/openshift-assisted/cluster-api-agent/util"
	logutil "github.com/openshift-assisted/cluster-api-agent/util/log"

	configv1 "github.com/openshift/api/config/v1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capiutil "sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

const (
	InstallConfigOverrides             = aiv1beta1.Group + "/install-config-overrides"
	defaultBaremetalBaselineCapability = "None"
	defaultBaselineCapability          = "vCurrent"
)

var (
	defaultBaremetalAdditionalCapabilities = []configv1.ClusterVersionCapability{"baremetal", "Console", "Insights", "OperatorLifecycleManager", "Ingress"}
)

// ClusterDeploymentReconciler reconciles a ClusterDeployment object
type ClusterDeploymentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hivev1.ClusterDeployment{}).
		Watches(&controlplanev1alpha2.OpenshiftAssistedControlPlane{}, &handler.EnqueueRequestForObject{}).
		Watches(&clusterv1.MachineDeployment{}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}

func (r *ClusterDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	clusterDeployment := &hivev1.ClusterDeployment{}
	if err := r.Client.Get(ctx, req.NamespacedName, clusterDeployment); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	log.WithValues("cluster_deployment", clusterDeployment.Name, "cluster_deployment_namespace", clusterDeployment.Namespace)
	log.V(logutil.TraceLevel).Info("Reconciling ClusterDeployment")

	acp := controlplanev1alpha2.OpenshiftAssistedControlPlane{}
	if err := util.GetTypedOwner(ctx, r.Client, clusterDeployment, &acp); err != nil {
		log.V(logutil.TraceLevel).Info("Cluster deployment is not owned by OpenshiftAssistedControlPlane")
		return ctrl.Result{}, nil
	}
	log.WithValues("openshiftassisted_control_plane", acp.Name, "openshiftassisted_control_plane_namespace", acp.Namespace)

	arch, err := getArchitectureFromBootstrapConfigs(ctx, r.Client, &acp)
	if err != nil {
		return ctrl.Result{}, err
	}
	if err = ensureClusterImageSet(ctx, r.Client, clusterDeployment.Name, getReleaseImage(acp, arch)); err != nil {
		log.Error(err, "failed creating ClusterImageSet")
		return ctrl.Result{}, err
	}

	if acp.Spec.Config.ImageRegistryRef != nil {
		if err := r.createImageRegistry(ctx, acp.Spec.Config.ImageRegistryRef.Name, acp.Namespace); err != nil {
			log.Error(err, "failed to create image registry config manifest")
			return ctrl.Result{}, err
		}
	}

	if err := r.ensureAgentClusterInstall(ctx, clusterDeployment, acp); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, r.updateClusterDeploymentRef(ctx, clusterDeployment)
}

func (r *ClusterDeploymentReconciler) ensureAgentClusterInstall(
	ctx context.Context,
	clusterDeployment *hivev1.ClusterDeployment,
	oacp controlplanev1alpha2.OpenshiftAssistedControlPlane,
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
		aci.ObjectMeta.Labels = util.ControlPlaneMachineLabelsForCluster(
			&oacp,
			clusterDeployment.Labels[clusterv1.ClusterNameLabel],
		)
		aci.ObjectMeta.Labels[hiveext.ClusterConsumerLabel] = openshiftAssistedControlPlaneKind
		aci.ObjectMeta.OwnerReferences = []metav1.OwnerReference{
			*metav1.NewControllerRef(&oacp, controlplanev1alpha2.GroupVersion.WithKind(openshiftAssistedControlPlaneKind)),
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
		aci.Spec.Networking = hiveext.Networking{
			ClusterNetwork: clusterNetwork,
			ServiceNetwork: serviceNetwork,
		}
		aci.Spec.ManifestsConfigMapRefs = additionalManifests

		if len(oacp.Spec.Config.APIVIPs) > 0 && len(oacp.Spec.Config.IngressVIPs) > 0 {
			aci.Spec.APIVIPs = oacp.Spec.Config.APIVIPs
			aci.Spec.IngressVIPs = oacp.Spec.Config.IngressVIPs
			aci.Spec.PlatformType = hiveext.PlatformType(configv1.BareMetalPlatformType)
		}
		if err := setACICapabilities(&oacp, aci); err != nil {
			return err
		}
		return nil
	}

	if _, err = controllerutil.CreateOrUpdate(ctx, r.Client, aci, mutate); err != nil {
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
func getReleaseImage(oacp controlplanev1alpha2.OpenshiftAssistedControlPlane, architecture string) string {
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
	if err := r.Client.List(ctx, &mdList, client.MatchingLabels{clusterv1.ClusterNameLabel: cluster.Name}); err != nil {
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
	cd.Spec.ClusterInstallRef = &hivev1.ClusterInstallLocalReference{
		Group:   hiveext.Group,
		Version: hiveext.Version,
		Kind:    "AgentClusterInstall",
		Name:    cd.Name,
	}
	return r.Client.Update(ctx, cd)
}

func ensureClusterImageSet(ctx context.Context, c client.Client, imageSetName string, releaseImage string) error {
	imageSet := &hivev1.ClusterImageSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: imageSetName,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, c, imageSet, func() error {
		imageSet.Spec.ReleaseImage = releaseImage
		return nil
	})

	return err
}

func getClusterNetworks(cluster *clusterv1.Cluster) ([]hiveext.ClusterNetworkEntry, []string) {
	var clusterNetwork []hiveext.ClusterNetworkEntry

	if cluster.Spec.ClusterNetwork != nil && cluster.Spec.ClusterNetwork.Pods != nil {
		for _, cidrBlock := range cluster.Spec.ClusterNetwork.Pods.CIDRBlocks {
			clusterNetwork = append(clusterNetwork, hiveext.ClusterNetworkEntry{CIDR: cidrBlock, HostPrefix: 23})
		}
	}
	var serviceNetwork []string
	if cluster.Spec.ClusterNetwork != nil && cluster.Spec.ClusterNetwork.Services != nil {
		serviceNetwork = cluster.Spec.ClusterNetwork.Services.CIDRBlocks
	}

	return clusterNetwork, serviceNetwork
}

func getClusterAdditionalManifestRefs(acp controlplanev1alpha2.OpenshiftAssistedControlPlane) []hiveext.ManifestsConfigMapReference {
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
	if err := r.Client.Get(ctx, client.ObjectKey{Name: registryName, Namespace: registryNamespace}, registryConfigmap); err != nil {
		return err
	}

	spokeImageRegistryConfigmap, err := imageregistry.GenerateImageRegistryConfigmap(registryConfigmap, registryNamespace)
	if err != nil {
		return err
	}

	if err := util.CreateOrUpdate(ctx, r.Client, spokeImageRegistryConfigmap); err != nil {
		return err
	}
	return nil
}

type InstallConfigOverride struct {
	Capability configv1.ClusterVersionCapabilitiesSpec `json:"capabilities,omitempty"`
}

// setACICapabilities will set the install config override annotation in the AgentClusterInstall if there
// are additional enabled capabilities that need to be defined.
// For SNO (single node openshift) and non-baremetal platform MNO (multi-node openshift), this is set if
// the OpenshiftAssistedControlPlane has additional capabilities set.
// For MNO (multi-node openshift), this is set to the default list for baremetal platform,
// but the OpenshiftAssistedControlPlane can specify additional capabilities to be appended to this list.
func setACICapabilities(oacp *controlplanev1alpha2.OpenshiftAssistedControlPlane, aci *hiveext.AgentClusterInstall) error {
	isBaremetalPlatform := aci.Spec.PlatformType == hiveext.BareMetalPlatformType
	if isCapabilitiesEmpty(oacp.Spec.Config.Capabilities) && !isBaremetalPlatform {
		return nil
	}
	var installCfgOverride InstallConfigOverride
	baselineCapability, err := getBaselineCapability(oacp.Spec.Config.Capabilities.BaselineCapability, isBaremetalPlatform)
	if err != nil {
		return err
	}
	installCfgOverride.Capability.BaselineCapabilitySet = configv1.ClusterVersionCapabilitySet(baselineCapability)

	additionalEnabledCapabilities := getAdditionalCapabilities(oacp.Spec.Config.Capabilities.AdditionalEnabledCapabilities, isBaremetalPlatform)
	installCfgOverride.Capability.AdditionalEnabledCapabilities = additionalEnabledCapabilities

	installCfgOverrideStr, err := json.Marshal(installCfgOverride)
	if err != nil {
		return err
	}

	if aci.Annotations == nil {
		aci.Annotations = make(map[string]string)
	}

	aci.Annotations[InstallConfigOverrides] = string(installCfgOverrideStr)
	return nil
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

func isCapabilitiesEmpty(capabilities controlplanev1alpha2.Capabilities) bool {
	return equality.Semantic.DeepEqual(capabilities, controlplanev1alpha2.Capabilities{})
}
