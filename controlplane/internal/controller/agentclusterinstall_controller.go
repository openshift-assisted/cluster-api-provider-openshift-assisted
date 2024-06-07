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
	"fmt"

	"github.com/pkg/errors"

	controlplanev1alpha1 "github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1alpha1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aimodels "github.com/openshift/assisted-service/models"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	clusterDeployementRefNameKey     = "spec.agentConfigSpec.clusterDeploymentRef.name"
	clusterDeploymentRefNamespaceKey = "spec.agentConfigSpec.clusterDeploymentRef.namespace"
)

// AgentClusterInstallReconciler reconciles a AgentClusterInstall object

type AgentClusterInstallReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *AgentClusterInstallReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hiveext.AgentClusterInstall{}).
		Complete(r)
}

func (r *AgentClusterInstallReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	defer func() {
		log.Info("Agent Cluster Install Reconcile ended")
	}()

	log.Info("Agent Cluster Install Reconcile started")
	aci := &hiveext.AgentClusterInstall{}
	if err := r.Client.Get(ctx, req.NamespacedName, aci); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	log.Info("Reconciling AgentClusterInstall", "name", aci.Name, "namespace", aci.Namespace)

	cd, err := r.getClusterDeployment(ctx, aci)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	acpList, err := r.getAgentControlPlaneList(ctx, cd, aci)
	if err != nil {
		return ctrl.Result{}, err
	}
	if acpList == nil || len(acpList.Items) == 0 {
		log.Info("agentcontrolplane not found", "num", len(acpList.Items))
		return ctrl.Result{}, nil
	}
	log.Info("found agentcontrolplane", "num", len(acpList.Items))

	for _, acp := range acpList.Items {
		// Check if AgentClusterInstall has moved to day 2 aka control plane is installed
		if isInstalled(aci) && hasKubeconfigRef(aci) {
			acp.Status.Initialized = true

			log.Info("Agent cluster install for control plane nodes has finished. Attaching kubeconfig secret to agent control plane",
				"agent cluster install name", aci.Name, "agent cluster install namespace", aci.Namespace, "agent control plane name", acp.Name)

			kubeconfigSecret, err := r.getACIKubeconfig(ctx, aci, acp)
			if err != nil {
				log.Error(err, "failed getting kubeconfig secret for agentclusterinstall", "agent cluster install name", aci.Name, "agent cluster install namespace", aci.Namespace, "secret name", kubeconfigSecret.Name)
				return ctrl.Result{}, err
			}
			log.Info("found kubeconfig secret", "agent cluster install name", aci.Name, "agent cluster install namespace", aci.Namespace, "secret name", kubeconfigSecret.Name)

			clusterName := acp.Labels[clusterv1.ClusterNameLabel]
			labels := map[string]string{
				clusterv1.ClusterNameLabel: clusterName,
			}

			if err := r.updateLabels(ctx, kubeconfigSecret, labels); err != nil {
				log.Error(err, "failed updating kubeconfig secret for agentclusterinstall", "agent cluster install name", aci.Name, "agent cluster install namespace", aci.Namespace, "secret name", kubeconfigSecret.Name)
				return ctrl.Result{}, err
			}

			if !r.ClusterKubeconfigSecretExists(ctx, clusterName, acp.Namespace) {
				log.Info("Cluster kubeconfig doesn't exist, creating it")
				if err := r.createKubeconfig(ctx, kubeconfigSecret, clusterName, acp); err != nil {
					log.Error(err, "failed to create secret for agent control plane", "agent cluster install name", aci.Name, "agent cluster install namespace", aci.Namespace, "agent control plane name", acp.Name)
					return ctrl.Result{}, err
				}
			}
			// Update AgentControlPlane status
			acp.Status.Ready = true
			if err := r.Client.Status().Update(ctx, &acp); err != nil {
				log.Error(err, "failed updating agent control plane to ready for agentclusterinstall", "agent cluster install name", aci.Name, "agent cluster install namespace", aci.Namespace, "agent control plane name", acp.Name)
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *AgentClusterInstallReconciler) createKubeconfig(ctx context.Context, kubeconfigSecret *corev1.Secret, clusterName string, acp controlplanev1alpha1.AgentControlPlane) error {
	kubeconfig, ok := kubeconfigSecret.Data["kubeconfig"]
	if !ok {
		return errors.New("kubeconfig not found in secret")
	}
	// Create secret <cluster-name>-kubeconfig from original kubeconfig secret - this is what the CAPI Cluster looks for to set the control plane as initialized
	clusterNameKubeconfigSecret := GenerateSecretWithOwner(
		client.ObjectKey{Name: clusterName, Namespace: acp.Namespace},
		kubeconfig,
		*metav1.NewControllerRef(&acp, controlplanev1alpha1.GroupVersion.WithKind(agentControlPlaneKind)),
	)
	if err := r.Client.Create(ctx, clusterNameKubeconfigSecret); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
		if err := r.Client.Update(ctx, clusterNameKubeconfigSecret); err != nil {
			return err
		}
	}
	return nil
}

func (r *AgentClusterInstallReconciler) updateLabels(ctx context.Context, obj client.Object, labels map[string]string) error {

	objLabels := obj.GetLabels()
	for k, v := range labels {
		objLabels[k] = v
	}
	obj.SetLabels(objLabels)
	if err := r.Client.Update(ctx, obj); err != nil {
		return err
	}
	return nil
}

func (r *AgentClusterInstallReconciler) getACIKubeconfig(ctx context.Context, aci *hiveext.AgentClusterInstall, agentCP controlplanev1alpha1.AgentControlPlane) (*corev1.Secret, error) {
	secretName := aci.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name

	// Get the kubeconfig secret and label with capi key pair cluster.x-k8s.io/cluster-name=<cluster name>
	kubeconfigSecret := &corev1.Secret{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: secretName, Namespace: agentCP.Namespace}, kubeconfigSecret); err != nil {
		return nil, err
	}
	return kubeconfigSecret, nil
}

func hasKubeconfigRef(aci *hiveext.AgentClusterInstall) bool {
	return aci.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name != ""
}

func isInstalled(aci *hiveext.AgentClusterInstall) bool {
	return aci.Status.DebugInfo.State == aimodels.ClusterStatusAddingHosts
}

func (r *AgentClusterInstallReconciler) getAgentControlPlaneList(ctx context.Context, cd *hivev1.ClusterDeployment, aci *hiveext.AgentClusterInstall) (*controlplanev1alpha1.AgentControlPlaneList, error) {
	acpList := &controlplanev1alpha1.AgentControlPlaneList{}
	if err := r.Client.List(ctx, acpList, client.InNamespace(aci.Namespace)); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	filteredACPList := &controlplanev1alpha1.AgentControlPlaneList{}
	for _, acp := range acpList.Items {
		if referencesClusterDeployment(acp, cd) {
			filteredACPList.Items = append(filteredACPList.Items, acp)
		}
	}
	return filteredACPList, nil
}

func getMatchClusterDeploymentOpt(cd *hivev1.ClusterDeployment) client.MatchingFields {
	return client.MatchingFields{
		clusterDeployementRefNameKey:     cd.Name,
		clusterDeploymentRefNamespaceKey: cd.Namespace,
	}
}

func (r *AgentClusterInstallReconciler) ClusterKubeconfigSecretExists(ctx context.Context, clusterName, namespace string) bool {
	secretName := fmt.Sprintf("%s-kubeconfig", clusterName)
	kubeconfigSecret := &corev1.Secret{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: secretName, Namespace: namespace}, kubeconfigSecret); err != nil {
		return !apierrors.IsNotFound(err)
	}
	return true
}

func (r *AgentClusterInstallReconciler) getClusterDeployment(ctx context.Context, aci *hiveext.AgentClusterInstall) (*hivev1.ClusterDeployment, error) {
	// Get cluster deployment associated with this agent cluster install
	cdName := aci.Spec.ClusterDeploymentRef.Name
	cd := hivev1.ClusterDeployment{}

	// Ensure cluster deployment exists
	if err := r.Client.Get(ctx, client.ObjectKey{Name: cdName, Namespace: aci.Namespace}, &cd); err != nil {
		return &cd, err
	}
	return &cd, nil
}

// GenerateSecretWithOwner returns a Kubernetes secret for the given Cluster name, namespace, kubeconfig data, and ownerReference.
func GenerateSecretWithOwner(clusterName client.ObjectKey, data []byte, owner metav1.OwnerReference) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-kubeconfig", clusterName.Name),
			Namespace: clusterName.Namespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: clusterName.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				owner,
			},
		},
		Data: map[string][]byte{
			"value": data,
		},
		Type: clusterv1.ClusterSecretType,
	}
}

func referencesClusterDeployment(acp controlplanev1alpha1.AgentControlPlane, cd *hivev1.ClusterDeployment) bool {
	return acp.Status.ClusterDeploymentRef != nil && acp.Status.ClusterDeploymentRef.Name == cd.Name &&
		acp.Status.ClusterDeploymentRef.Namespace == cd.Namespace
}
