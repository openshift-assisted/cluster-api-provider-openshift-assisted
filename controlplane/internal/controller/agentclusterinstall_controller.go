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

	"github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1beta1"
	controlplanev1beta1 "github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1beta1"
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
	agentClusterInstall := &hiveext.AgentClusterInstall{}
	if err := r.Client.Get(ctx, req.NamespacedName, agentClusterInstall); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	log.Info("Reconciling AgentClusterInstall", "name", agentClusterInstall.Name, "namespace", agentClusterInstall.Namespace)

	// Get cluster deployment associated with this agent cluster install
	clusterDeploymentName := agentClusterInstall.Spec.ClusterDeploymentRef.Name
	clusterDeployment := &hivev1.ClusterDeployment{}

	// Ensure cluster deployment exists
	if err := r.Client.Get(ctx, client.ObjectKey{Name: clusterDeploymentName, Namespace: agentClusterInstall.Namespace}, clusterDeployment); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Get agent control plane based on clusterDeployment
	agentCPList := &v1beta1.AgentControlPlaneList{}
	if err := r.Client.List(ctx, agentCPList, client.InNamespace(agentClusterInstall.Namespace)); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	log.Info("found agentcontrolplane", "num", len(agentCPList.Items))

	if len(agentCPList.Items) == 0 {
		return ctrl.Result{}, nil
	}
	for _, agentCP := range agentCPList.Items {
		if IsAgentControlPlaneReferencingClusterDeployment(agentCP, clusterDeployment) {
			log.Info("Found AgentControlPlane associated with agent cluster install")

			// Check if AgentClusterInstall has moved to day 2 aka control plane is installed
			if agentClusterInstall.Status.DebugInfo.State == aimodels.ClusterStatusAddingHosts &&
				agentClusterInstall.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name != "" {
				secretName := agentClusterInstall.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name
				log.Info("Agent cluster install for control plane nodes has finished. Attaching kubeconfig secret to agent control plane",
					"agent cluster install name", agentClusterInstall.Name, "agent cluster install namespace", agentClusterInstall.Namespace, "agent control plane name", agentCP.Name)

				// Get the kubeconfig secret and label with capi key pair cluster.x-k8s.io/cluster-name=<cluster name>
				kubeconfigSecret := &corev1.Secret{}
				if err := r.Client.Get(ctx, client.ObjectKey{Name: secretName, Namespace: agentCP.Namespace}, kubeconfigSecret); err != nil {
					log.Error(err, "failed getting kubeconfig secret for agentclusterinstall", "agent cluster install name", agentClusterInstall.Name, "agent cluster install namespace", agentClusterInstall.Namespace, "secret name", secretName)
					return ctrl.Result{}, err
				}
				log.Info("found kubeconfig secret", "agent cluster install name", agentClusterInstall.Name, "agent cluster install namespace", agentClusterInstall.Namespace, "secret name", secretName)

				clusterName := agentCP.Labels[clusterv1.ClusterNameLabel]
				if kubeconfigSecret.Labels == nil {
					kubeconfigSecret.Labels = map[string]string{}
				}

				kubeconfigSecret.Labels[clusterv1.ClusterNameLabel] = clusterName
				if err := r.Client.Update(ctx, kubeconfigSecret); err != nil {
					log.Error(err, "failed updating kubeconfig secret for agentclusterinstall", "agent cluster install name", agentClusterInstall.Name, "agent cluster install namespace", agentClusterInstall.Namespace, "secret name", secretName)
					return ctrl.Result{}, err
				}

				if !r.ClusterKubeconfigSecretExists(ctx, clusterName, agentCP.Namespace) {
					kubeconfig, ok := kubeconfigSecret.Data["kubeconfig"]
					if !ok {
						return ctrl.Result{}, errors.New("kubeconfig not found in secret")
					}
					// Create secret <cluster-name>-kubeconfig from original kubeconfig secret - this is what the CAPI Cluster looks for to set the control plane as initialized
					clusterNameKubeconfigSecret := GenerateSecretWithOwner(client.ObjectKey{Name: clusterName, Namespace: agentCP.Namespace}, kubeconfig, *metav1.NewControllerRef(&agentCP, controlplanev1beta1.GroupVersion.WithKind(agentControlPlaneKind)))
					log.Info("Cluster kubeconfig doesn't exist, creating it", "secret name", clusterNameKubeconfigSecret.Name)
					if err := r.Client.Create(ctx, clusterNameKubeconfigSecret); err != nil {
						if !apierrors.IsAlreadyExists(err) {
							log.Error(err, "failed creating secret for agent control plane", "agent cluster install name", agentClusterInstall.Name, "agent cluster install namespace", agentClusterInstall.Namespace, "agent control plane name", agentCP.Name)
							return ctrl.Result{}, err
						}
						if err := r.Client.Update(ctx, clusterNameKubeconfigSecret); err != nil {
							log.Error(err, "failed to update secret for agent control plane", "agent cluster install name", agentClusterInstall.Name, "agent cluster install namespace", agentClusterInstall.Namespace, "agent control plane name", agentCP.Name)
							return ctrl.Result{}, err
						}
					}
					// Update AgentControlPlane status
					agentCP.Status.Ready = true
					agentCP.Status.Initialized = true
					if err := r.Client.Status().Update(ctx, &agentCP); err != nil {
						log.Error(err, "failed updating agent control plane to ready for agentclusterinstall", "agent cluster install name", agentClusterInstall.Name, "agent cluster install namespace", agentClusterInstall.Namespace, "agent control plane name", agentCP.Name)
						return ctrl.Result{}, err
					}
				}
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *AgentClusterInstallReconciler) ClusterKubeconfigSecretExists(ctx context.Context, clusterName, namespace string) bool {
	secretName := fmt.Sprintf("%s-kubeconfig", clusterName)
	kubeconfigSecret := &corev1.Secret{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: secretName, Namespace: namespace}, kubeconfigSecret); err != nil {
		return !apierrors.IsNotFound(err)
	}
	return true
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
