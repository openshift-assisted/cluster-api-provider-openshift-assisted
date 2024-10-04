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

	controlplanev1alpha1 "github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1alpha1"
	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/imageregistry"
	"github.com/openshift-assisted/cluster-api-agent/util"
	logutil "github.com/openshift-assisted/cluster-api-agent/util/log"
	configv1 "github.com/openshift/api/config/v1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capiutil "sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

const (
	InstallConfigOverrides = aiv1beta1.Group + "/install-config-overrides"
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
		Watches(&controlplanev1alpha1.OpenshiftAssistedControlPlane{}, &handler.EnqueueRequestForObject{}).
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

	acp := controlplanev1alpha1.OpenshiftAssistedControlPlane{}
	if err := util.GetTypedOwner(ctx, r.Client, clusterDeployment, &acp); err != nil {
		log.V(logutil.TraceLevel).Info("Cluster deployment is not owned by OpenshiftAssistedControlPlane")
		return ctrl.Result{}, nil
	}
	log.WithValues("openshiftassisted_control_plane", acp.Name, "openshiftassisted_control_plane_namespace", acp.Namespace)

	if clusterDeployment.Spec.ClusterInstallRef != nil && r.agentClusterInstallExists(ctx, clusterDeployment.Spec.ClusterInstallRef.Name, clusterDeployment.Namespace) {
		log.V(logutil.TraceLevel).Info(
			"skipping reconciliation: cluster deployment already has a referenced agent cluster install",
			"agent_cluster_install", clusterDeployment.Spec.ClusterInstallRef.Name,
		)
		return ctrl.Result{}, nil
	}

	return r.ensureAgentClusterInstall(ctx, clusterDeployment, acp)
}

func (r *ClusterDeploymentReconciler) agentClusterInstallExists(ctx context.Context, agentClusterInstallName, namespace string) bool {
	agentclusterinstall := &hiveext.AgentClusterInstall{}
	err := r.Client.Get(ctx, client.ObjectKey{Name: agentClusterInstallName, Namespace: namespace}, agentclusterinstall)
	return err != nil
}

func (r *ClusterDeploymentReconciler) ensureAgentClusterInstall(
	ctx context.Context,
	clusterDeployment *hivev1.ClusterDeployment,
	acp controlplanev1alpha1.OpenshiftAssistedControlPlane,
) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	cluster, err := capiutil.GetOwnerCluster(ctx, r.Client, acp.ObjectMeta)
	if err != nil {
		log.Error(err, "failed to retrieve owner Cluster from the API Server")
		return ctrl.Result{}, err
	}

	imageSet, err := r.createOrUpdateClusterImageSet(ctx, clusterDeployment.Name, acp.Spec.Config.ReleaseImage)
	if err != nil {
		log.Error(err, "failed creating ClusterImageSet")
		return ctrl.Result{}, err
	}

	workerNodes := r.getWorkerNodesCount(ctx, cluster)
	aci, err := r.computeAgentClusterInstall(ctx, clusterDeployment, acp, imageSet, cluster, workerNodes)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.createOrUpdateAgentClusterInstall(ctx, aci); err != nil {
		log.Error(err, "failed creating AgentClusterInstall")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, r.updateClusterDeploymentRef(ctx, clusterDeployment, aci)
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
	aci *hiveext.AgentClusterInstall,
) error {
	cd.Spec.ClusterInstallRef = &hivev1.ClusterInstallLocalReference{
		Group:   hiveext.Group,
		Version: hiveext.Version,
		Kind:    "AgentClusterInstall",
		Name:    aci.Name,
	}
	return r.Client.Update(ctx, cd)
}

func (r *ClusterDeploymentReconciler) createOrUpdateClusterImageSet(
	ctx context.Context,
	imageSetName, releaseImage string,
) (*hivev1.ClusterImageSet, error) {
	imageSet := computeClusterImageSet(imageSetName, releaseImage)
	_, err := ctrl.CreateOrUpdate(ctx, r.Client, imageSet, func() error { return nil })
	return imageSet, client.IgnoreAlreadyExists(err)
}

func computeClusterImageSet(imageSetName string, releaseImage string) *hivev1.ClusterImageSet {
	return &hivev1.ClusterImageSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: imageSetName,
		},
		Spec: hivev1.ClusterImageSetSpec{
			ReleaseImage: releaseImage,
		},
	}
}

func (r *ClusterDeploymentReconciler) createOrUpdateAgentClusterInstall(
	ctx context.Context,
	aci *hiveext.AgentClusterInstall,
) error {
	_, err := ctrl.CreateOrUpdate(ctx, r.Client, aci, func() error { return nil })
	return client.IgnoreAlreadyExists(err)
}

func (r *ClusterDeploymentReconciler) computeAgentClusterInstall(
	ctx context.Context,
	clusterDeployment *hivev1.ClusterDeployment,
	acp controlplanev1alpha1.OpenshiftAssistedControlPlane,
	imageSet *hivev1.ClusterImageSet,
	cluster *clusterv1.Cluster,
	workerReplicas int,
) (*hiveext.AgentClusterInstall, error) {
	log := ctrl.LoggerFrom(ctx)
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
	var additionalManifests []hiveext.ManifestsConfigMapReference
	if len(acp.Spec.Config.ManifestsConfigMapRefs) > 0 {
		additionalManifests = append(additionalManifests, acp.Spec.Config.ManifestsConfigMapRefs...)
	}

	if acp.Spec.Config.ImageRegistryRef != nil {
		if err := r.createImageRegistry(ctx, acp.Spec.Config.ImageRegistryRef.Name, acp.Namespace); err != nil {
			log.Error(err, "failed to create image registry config manifest")
			return nil, err
		}
		additionalManifests = append(additionalManifests, hiveext.ManifestsConfigMapReference{Name: imageregistry.ImageConfigMapName})
	}

	aci := &hiveext.AgentClusterInstall{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterDeployment.Name,
			Namespace: clusterDeployment.Namespace,
			Labels: util.ControlPlaneMachineLabelsForCluster(
				&acp,
				clusterDeployment.Labels[clusterv1.ClusterNameLabel],
			),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(&acp, controlplanev1alpha1.GroupVersion.WithKind(openshiftAssistedControlPlaneKind)),
			},
		},
		Spec: hiveext.AgentClusterInstallSpec{
			ClusterDeploymentRef: corev1.LocalObjectReference{Name: clusterDeployment.Name},
			PlatformType:         hiveext.PlatformType(configv1.NonePlatformType),
			ProvisionRequirements: hiveext.ProvisionRequirements{
				ControlPlaneAgents: int(acp.Spec.Replicas),
				WorkerAgents:       workerReplicas,
			},
			DiskEncryption:     acp.Spec.Config.DiskEncryption,
			MastersSchedulable: acp.Spec.Config.MastersSchedulable,
			Proxy:              acp.Spec.Config.Proxy,
			SSHPublicKey:       acp.Spec.Config.SSHAuthorizedKey,
			ImageSetRef:        &hivev1.ClusterImageSetReference{Name: imageSet.Name},
			Networking: hiveext.Networking{
				ClusterNetwork: clusterNetwork,
				ServiceNetwork: serviceNetwork,
			},
			ManifestsConfigMapRefs: additionalManifests,
		},
	}

	if len(acp.Spec.Config.APIVIPs) > 0 && len(acp.Spec.Config.IngressVIPs) > 0 {
		aci.Spec.APIVIPs = acp.Spec.Config.APIVIPs
		aci.Spec.IngressVIPs = acp.Spec.Config.IngressVIPs
		aci.Spec.PlatformType = hiveext.PlatformType(configv1.BareMetalPlatformType)
		aci.Annotations = map[string]string{
			InstallConfigOverrides: `{"capabilities": {"baselineCapabilitySet": "None", "additionalEnabledCapabilities": ["baremetal","Console","Insights","OperatorLifecycleManager","Ingress"]}}"`,
		}
	}
	return aci, nil
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

	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, spokeImageRegistryConfigmap, func() error { return nil }); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}
