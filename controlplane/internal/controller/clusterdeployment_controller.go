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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	"github.com/go-logr/logr"
	"github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1beta1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
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
		Watches(&v1beta1.AgentControlPlane{}, &handler.EnqueueRequestForObject{}).
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
	// reconcile only in case the clusterdeployment is owned by AgentControlPlane

	// Check if the resource has owner references
	agentControlPlaneKindOwnerRef := false
	if len(clusterDeployment.OwnerReferences) > 0 {
		for _, ownerRef := range clusterDeployment.OwnerReferences {
			if ownerRef.Kind == agentControlPlaneKind {
				log.Info("clusterDeployment has an owner reference with agentControlPlaneKind, reconciling\n",
					"namespace:", clusterDeployment.Namespace, "name:", clusterDeployment.Name, "ownerRef:", ownerRef.Name)
				agentControlPlaneKindOwnerRef = true
				break
			}
		}
	}
	if !agentControlPlaneKindOwnerRef {
		log.Info("clusterDeployment %s %sisn't owned by AgentControlPlane", "namespace:", clusterDeployment.Namespace, "name:", clusterDeployment.Name)
		return ctrl.Result{}, nil
	}

	acp := &v1beta1.AgentControlPlane{}
	// Note we assume the same name and namespace for the ACP and clsuterDeployment
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: clusterDeployment.Namespace, Name: clusterDeployment.Name}, acp); err != nil {
		return ctrl.Result{}, err
	}

	imageSetName, err := r.ensureClusterImageSet(ctx, log, clusterDeployment, acp)
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, r.ensureAgentClusterInstall(ctx, log, clusterDeployment, acp, imageSetName)
}

func (r *ClusterDeploymentReconciler) ensureAgentClusterInstall(ctx context.Context, log logr.Logger, clusterDeployment *hivev1.ClusterDeployment, acp *v1beta1.AgentControlPlane, imageSetName string) error {
	log.Info("Setting AgentClusterInstall")
	agentClusterInstall := &hiveext.AgentClusterInstall{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: clusterDeployment.Namespace, Name: clusterDeployment.Name}, agentClusterInstall); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "failed to get AgentClusterInstall")
			return err
		}
		err = r.createAgentClusterInstall(ctx, log, clusterDeployment, acp, imageSetName)
		if err != nil {
			log.Error(err, "failed to create AgentClusterInstall")
			return err
		}
	}
	// TODO: consider removing this as it should already be set on the CD upon creation
	clusterDeployment.Spec.ClusterInstallRef = &hivev1.ClusterInstallLocalReference{
		Group:   hiveext.Group,
		Version: hiveext.Version,
		Kind:    "AgentClusterInstall",
		Name:    agentClusterInstall.Name,
	}
	return r.Client.Update(ctx, clusterDeployment)
}

func (r *ClusterDeploymentReconciler) ensureClusterImageSet(ctx context.Context, log logr.Logger, clusterDeployment *hivev1.ClusterDeployment, acp *v1beta1.AgentControlPlane) (string, error) {
	imageSet := &hivev1.ClusterImageSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterDeployment.Name,
			Namespace: clusterDeployment.Namespace,
		},
		Spec: hivev1.ClusterImageSetSpec{
			ReleaseImage: acp.Spec.AgentConfigSpec.ReleaseImage,
		},
	}
	if err := r.Client.Create(ctx, imageSet); err != nil {
		if !apierrors.IsAlreadyExists(err) { // ignore already exists error
			log.Error(err, "Error creating clusterImageSet ", "name", imageSet.Name, "namespace", imageSet.Namespace)
			return "", err
		}
	}
	return clusterDeployment.Name, nil
}

func (r *ClusterDeploymentReconciler) createAgentClusterInstall(ctx context.Context, log logr.Logger, clusterDeployment *hivev1.ClusterDeployment, acp *v1beta1.AgentControlPlane, imageSetName string) error {

	log.Info("Creating agent cluster install for ClusterDeployment", "name", clusterDeployment.Name, "namespace", clusterDeployment.Namespace)
	agentClusterInstall := &hiveext.AgentClusterInstall{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterDeployment.Name,
			Namespace: clusterDeployment.Namespace,
		},
		Spec: hiveext.AgentClusterInstallSpec{
			ClusterDeploymentRef: corev1.LocalObjectReference{Name: clusterDeployment.Name},
			ProvisionRequirements: hiveext.ProvisionRequirements{
				ControlPlaneAgents: int(acp.Spec.Replicas),
			},
			SSHPublicKey: acp.Spec.AgentConfigSpec.SSHAuthorizedKey,
			ImageSetRef:  &hivev1.ClusterImageSetReference{Name: imageSetName},
		},
	}
	// TODO: convert this to create or update
	err := r.Client.Create(ctx, agentClusterInstall)
	if !apierrors.IsAlreadyExists(err) { // ignore already exists error
		log.Error(err, "Error creating AgentClusterInstall ", "name", agentClusterInstall.Name, "namespace", agentClusterInstall.Namespace)
		return err
	}
	return nil
}
