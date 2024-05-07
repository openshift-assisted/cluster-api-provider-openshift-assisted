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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1beta1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aimodels "github.com/openshift/assisted-service/models"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
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

	// Check if cluster has finished installing
	if agentClusterInstall.Status.DebugInfo.State != aimodels.ClusterStatusAddingHosts ||
		agentClusterInstall.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name == "" {
		log.Info("Agent cluster install not finished installing", "agent cluster install name", agentClusterInstall.Name, "agent cluster install namespace", agentClusterInstall.Namespace)
		return ctrl.Result{}, nil
	}

	// Ensure kubeconfig secret exists
	secretName := agentClusterInstall.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name
	kubeconfigSecret := &corev1.Secret{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: secretName, Namespace: agentClusterInstall.Namespace}, kubeconfigSecret); err != nil {
		log.Error(err, "failed getting kubeconfig secret for agentclusterinstall", "agent cluster install name", agentClusterInstall.Name, "agent cluster install namespace", agentClusterInstall.Namespace, "secret name", secretName)
		return ctrl.Result{}, err
	}
	log.Info("found kubeconfig secret", "agent cluster install name", agentClusterInstall.Name, "agent cluster install namespace", agentClusterInstall.Namespace, "secret name", secretName)

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

			log.Info("Attaching kubeconfig secret to agent control plane",
				"agent cluster install name", agentClusterInstall.Name,
				"agent cluster install namespace", agentClusterInstall.Namespace, "agent control plane name", agentCP.Name)

			// Label original kubeconfig secret with clusternamelabel
			clusterName := agentCP.Labels[clusterv1.ClusterNameLabel]
			EnsureClusterLabelOnSecret(kubeconfigSecret, clusterName)
			if err := r.Client.Update(ctx, kubeconfigSecret); err != nil {
				log.Error(err, "failed updating kubeconfig secret for agentclusterinstall", "agent cluster install name", agentClusterInstall.Name, "agent cluster install namespace", agentClusterInstall.Namespace, "secret name", secretName)
				return ctrl.Result{}, err
			}

			if !r.ClusterKubeconfigSecretExists(ctx, clusterName, agentCP.Namespace) {
				// Create secret <cluster-name>-kubeconfig from original kubeconfig secret - this is what the CAPI Cluster looks for to set the control plane as initialized
				clusterNameKubeconfigSecret := GenerateSecretWithOwner(client.ObjectKey{Name: clusterName, Namespace: agentCP.Namespace}, kubeconfigSecret.Data["kubeconfig"], *metav1.NewControllerRef(&agentCP, v1beta1.GroupVersion.WithKind(agentControlPlaneKind)))
				log.Info("Cluster kubeconfig doesn't exist, creating it", "secret name", clusterNameKubeconfigSecret.Name)
				if err := r.Client.Create(ctx, clusterNameKubeconfigSecret); err != nil {
					log.Error(err, "failed creating secret for agent control plane", "agent cluster install name", agentClusterInstall.Name, "agent cluster install namespace", agentClusterInstall.Namespace, "agent control plane name", agentCP.Name)
					return ctrl.Result{}, err
				}
			}

			// Update AgentControlPlane status
			if err := isControlPlaneReady(ctx, kubeconfigSecret.Data["kubeconfig"], &agentCP); err != nil {
				log.Error(err, "couldn't determine if control plane is not ready")
			}

			log.Info("Setting control plane status")
			agentCP.Status.Initialized = true
			if err := r.Client.Status().Update(ctx, &agentCP); err != nil {
				log.Error(err, "failed updating agent control plane to ready for agentclusterinstall", "agent cluster install name", agentClusterInstall.Name, "agent cluster install namespace", agentClusterInstall.Namespace, "agent control plane name", agentCP.Name)
				return ctrl.Result{}, err
			}
			log.Info("Finished updating agentcontrolplane for agentclusterinstall")
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

func IsAgentControlPlaneReferencingClusterDeployment(agentCP v1beta1.AgentControlPlane, clusterDeployment *hivev1.ClusterDeployment) bool {
	return agentCP.Spec.AgentConfigSpec.ClusterDeploymentRef != nil &&
		agentCP.Spec.AgentConfigSpec.ClusterDeploymentRef.GroupVersionKind().String() == hivev1.SchemeGroupVersion.WithKind("ClusterDeployment").String() &&
		agentCP.Spec.AgentConfigSpec.ClusterDeploymentRef.Namespace == clusterDeployment.Namespace &&
		agentCP.Spec.AgentConfigSpec.ClusterDeploymentRef.Name == clusterDeployment.Name
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

func EnsureClusterLabelOnSecret(secret *corev1.Secret, clusterName string) {
	if secret.Labels == nil {
		secret.Labels = map[string]string{}
	}
	secret.Labels[clusterv1.ClusterNameLabel] = clusterName
}

// Checks if the spoke cluster's nodes are ready and set the agent control plane status based on the result
func isControlPlaneReady(ctx context.Context, kubeconfig []byte, agentCP *v1beta1.AgentControlPlane) error {
	log := ctrl.LoggerFrom(ctx)
	agentCP.Status.Ready = false

	client, err := getSpokeClient(kubeconfig)
	if err != nil {
		log.Error(err, "Couldn't get spoke client from kubeconfig")
		return err
	}

	nodeList := &corev1.NodeList{}
	if err := client.List(ctx, nodeList); err != nil {
		log.Error(err, "Couldn't list nodes from spoke cluster")
		return err
	}

	log.Info("Listing nodes in spoke cluster to verify the control plane is reachable and nodes are ready", "node list", nodeList.Size())
	if nodeList.Size() < int(agentCP.Spec.Replicas) {
		log.Info("Node list is less than expected replicas, control plane not ready",
			"node list", nodeList.Size(), "expected nodes", agentCP.Spec.Replicas)
		return nil
	}

	// Count how many nodes in the spoke cluster are actually ready
	readyNodes := 0
	for _, node := range nodeList.Items {
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
				readyNodes++
				break
			}
		}
	}

	// Set agent control plane replica count and ready status
	agentCP.Status.ReadyReplicas = int32(readyNodes)
	agentCP.Status.Replicas = int32(readyNodes)
	if readyNodes == int(agentCP.Spec.Replicas) {
		agentCP.Status.Ready = true
	}

	log.Info("looped through all nodes and determined", "control plane ready", agentCP.Status.Ready, "ready replicas", readyNodes)
	return nil
}

func getSpokeClient(kubeconfig []byte) (client.Client, error) {
	clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfig)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get clientconfig from kubeconfig data")
	}

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get restconfig for kube client")
	}

	schemes := getKubeClientSchemes()
	targetClient, err := client.New(restConfig, client.Options{Scheme: schemes})
	if err != nil {
		return nil, err
	}
	return targetClient, nil
}

func getKubeClientSchemes() *runtime.Scheme {
	var schemes = runtime.NewScheme()
	utilruntime.Must(scheme.AddToScheme(schemes))
	utilruntime.Must(corev1.AddToScheme(schemes))
	return schemes
}
