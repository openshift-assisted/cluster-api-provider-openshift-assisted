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
	"github.com/openshift-assisted/cluster-api-agent/util"
	"time"

	"github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1alpha1"
	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/imageregistry"
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
		Watches(&v1alpha1.AgentControlPlane{}, &handler.EnqueueRequestForObject{}).
		Watches(&clusterv1.MachineDeployment{}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}
func (r *ClusterDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	clusterDeployment := &hivev1.ClusterDeployment{}
	if err := r.Client.Get(ctx, req.NamespacedName, clusterDeployment); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	log.Info("Reconciling ClusterDeployment", "name", clusterDeployment.Name, "namespace", clusterDeployment.Namespace)

	agentCPList := &v1alpha1.AgentControlPlaneList{}
	if err := r.Client.List(ctx, agentCPList, client.InNamespace(clusterDeployment.Namespace)); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	log.Info("found agentcontrolplane", "num", len(agentCPList.Items))

	if len(agentCPList.Items) == 0 {
		return ctrl.Result{}, nil
	}

	for _, acp := range agentCPList.Items {
		if IsAgentControlPlaneReferencingClusterDeployment(acp, clusterDeployment) {
			log.Info("ClusterDeployment is referenced by AgentControlPlane")
			return r.ensureAgentClusterInstall(ctx, clusterDeployment, acp)
		}
	}

	return ctrl.Result{}, nil
}

func IsAgentControlPlaneReferencingClusterDeployment(agentCP v1alpha1.AgentControlPlane, clusterDeployment *hivev1.ClusterDeployment) bool {
	return agentCP.Status.ClusterDeploymentRef != nil &&
		agentCP.Status.ClusterDeploymentRef.GroupVersionKind().String() == hivev1.SchemeGroupVersion.WithKind("ClusterDeployment").String() &&
		agentCP.Status.ClusterDeploymentRef.Namespace == clusterDeployment.Namespace &&
		agentCP.Status.ClusterDeploymentRef.Name == clusterDeployment.Name
}

func (r *ClusterDeploymentReconciler) ensureAgentClusterInstall(ctx context.Context, clusterDeployment *hivev1.ClusterDeployment, acp v1alpha1.AgentControlPlane) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("No agentcontrolplane is referenced by ClusterDeployment", "name", clusterDeployment.Name, "namespace", clusterDeployment.Namespace)

	cluster, err := capiutil.GetOwnerCluster(ctx, r.Client, acp.ObjectMeta)
	if err != nil {
		log.Error(err, "failed to retrieve owner Cluster from the API Server", "agentcontrolplane name", acp.Name, "agentcontrolplane namespace", acp.Namespace)
		return ctrl.Result{}, err
	}
	if cluster == nil {
		log.Info("Cluster Controller has not yet set OwnerRef")
		return ctrl.Result{Requeue: true, RequeueAfter: 10 * time.Second}, nil
	}

	imageSet, err := r.createOrUpdateClusterImageSet(ctx, clusterDeployment.Name, acp.Spec.AgentConfigSpec.ReleaseImage)
	if err != nil {
		return ctrl.Result{}, err
	}
	if imageSet == nil {
		return ctrl.Result{Requeue: true, RequeueAfter: 10 * time.Second}, nil
	}
	workerNodes := r.getWorkerNodesCount(ctx, cluster)
	aci, err := r.createOrUpdateAgentClusterInstall(ctx, clusterDeployment, acp, cluster, imageSet, workerNodes)
	if err != nil {
		return ctrl.Result{}, err
	}
	if aci == nil {
		return ctrl.Result{Requeue: true, RequeueAfter: 10 * time.Second}, nil
	}
	if clusterDeployment.Spec.ClusterInstallRef != nil {
		log.Info(
			"skipping reconciliation: cluster deployment already has a referenced agent cluster install",
			"cluster_deployment_name", clusterDeployment.Name,
			"cluster_deployment_namespace", clusterDeployment.Namespace,
			"agent_cluster_install", clusterDeployment.Spec.ClusterInstallRef.Name,
		)
		return ctrl.Result{}, nil
	}
	err = r.updateClusterDeploymentRef(ctx, clusterDeployment, aci)

	return ctrl.Result{}, err
}

func (r *ClusterDeploymentReconciler) getWorkerNodesCount(ctx context.Context, cluster *clusterv1.Cluster) int {
	log := ctrl.LoggerFrom(ctx)

	mdList := clusterv1.MachineDeploymentList{}
	if err := r.Client.List(ctx, &mdList, client.MatchingLabels{clusterv1.ClusterNameLabel: cluster.Name}); err != nil {
		log.Error(err, "failed to list MachineDeployments", "cluster", cluster.Name)
		return 0
	}
	count := 0
	for _, md := range mdList.Items {
		count += int(*md.Spec.Replicas)
	}
	return count
}

func (r *ClusterDeploymentReconciler) updateClusterDeploymentRef(ctx context.Context, cd *hivev1.ClusterDeployment, aci *hiveext.AgentClusterInstall) error {
	cd.Spec.ClusterInstallRef = &hivev1.ClusterInstallLocalReference{
		Group:   hiveext.Group,
		Version: hiveext.Version,
		Kind:    "AgentClusterInstall",
		Name:    aci.Name,
	}
	return r.Client.Update(ctx, cd)

}

func (r *ClusterDeploymentReconciler) createOrUpdateClusterImageSet(ctx context.Context, imageSetName, releaseImage string) (*hivev1.ClusterImageSet, error) {
	log := ctrl.LoggerFrom(ctx)

	imageSet := computeClusterImageSet(imageSetName, releaseImage)
	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, imageSet, func() error { return nil }); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			log.Info("failed to retrieve ImageSet", "name", imageSetName)
			return nil, err
		}
	}
	return imageSet, nil
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

func (r *ClusterDeploymentReconciler) createOrUpdateAgentClusterInstall(ctx context.Context, clusterDeployment *hivev1.ClusterDeployment, acp v1alpha1.AgentControlPlane, cluster *clusterv1.Cluster, imageSet *hivev1.ClusterImageSet, workerNodes int) (*hiveext.AgentClusterInstall, error) {
	log := ctrl.LoggerFrom(ctx)
	aci := r.computeAgentClusterInstall(ctx, clusterDeployment, acp, imageSet, cluster, workerNodes)
	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, aci, func() error { return nil }); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			log.Info("failed to create agent cluster install for ClusterDeployment", "name", clusterDeployment.Name, "namespace", clusterDeployment.Namespace)
			return nil, err
		}
	}
	return aci, nil
}

func (r *ClusterDeploymentReconciler) computeAgentClusterInstall(ctx context.Context, clusterDeployment *hivev1.ClusterDeployment, acp v1alpha1.AgentControlPlane, imageSet *hivev1.ClusterImageSet, cluster *clusterv1.Cluster, workerReplicas int) *hiveext.AgentClusterInstall {
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
	if len(acp.Spec.AgentConfigSpec.ManifestsConfigMapRefs) > 0 {
		additionalManifests = append(additionalManifests, acp.Spec.AgentConfigSpec.ManifestsConfigMapRefs...)
	}

	if acp.Spec.AgentConfigSpec.ImageRegistryRef != nil {
		imageRegistryManifest, err := imageregistry.CreateConfig(ctx, r.Client, acp.Spec.AgentConfigSpec.ImageRegistryRef, clusterDeployment.Namespace)
		if err != nil {
			log.Error(err, "failed to create image registry config manifest")
		} else {
			additionalManifests = append(additionalManifests, hiveext.ManifestsConfigMapReference{Name: imageRegistryManifest})
		}
	}

	aci := &hiveext.AgentClusterInstall{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterDeployment.Name,
			Namespace: clusterDeployment.Namespace,
			Labels:    util.ControlPlaneMachineLabelsForCluster(&acp, clusterDeployment.Labels[clusterv1.ClusterNameLabel]),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(&acp, v1alpha1.GroupVersion.WithKind(agentControlPlaneKind)),
			},
		},
		Spec: hiveext.AgentClusterInstallSpec{
			ClusterDeploymentRef: corev1.LocalObjectReference{Name: clusterDeployment.Name},
			PlatformType:         hiveext.PlatformType(configv1.NonePlatformType),
			ProvisionRequirements: hiveext.ProvisionRequirements{
				ControlPlaneAgents: int(acp.Spec.Replicas),
				WorkerAgents:       workerReplicas,
			},
			DiskEncryption:     acp.Spec.AgentConfigSpec.DiskEncryption,
			MastersSchedulable: acp.Spec.AgentConfigSpec.MastersSchedulable,
			Proxy:              acp.Spec.AgentConfigSpec.Proxy,
			SSHPublicKey:       acp.Spec.AgentConfigSpec.SSHAuthorizedKey,
			ImageSetRef:        &hivev1.ClusterImageSetReference{Name: imageSet.Name},
			Networking: hiveext.Networking{
				ClusterNetwork: clusterNetwork,
				ServiceNetwork: serviceNetwork,
			},
			ManifestsConfigMapRefs: additionalManifests,
		},
	}
	return aci
}
