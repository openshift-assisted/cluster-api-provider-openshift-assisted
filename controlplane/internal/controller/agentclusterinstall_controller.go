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

	"sigs.k8s.io/cluster-api/util/conditions"

	controlplanev1alpha2 "github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1alpha2"
	"github.com/openshift-assisted/cluster-api-agent/util"
	logutil "github.com/openshift-assisted/cluster-api-agent/util/log"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aimodels "github.com/openshift/assisted-service/models"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const kubeconfigSecretKey = "kubeconfig"

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
		log.V(logutil.TraceLevel).Info("Agent Cluster Install Reconcile ended")
	}()

	log.V(logutil.TraceLevel).Info("Agent Cluster Install Reconcile started")
	aci := &hiveext.AgentClusterInstall{}
	if err := r.Client.Get(ctx, req.NamespacedName, aci); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	log.WithValues("agent_cluster_install", aci.Name, "agent_cluster_install_namespace", aci.Namespace)

	acp := controlplanev1alpha2.OpenshiftAssistedControlPlane{}
	if err := util.GetTypedOwner(ctx, r.Client, aci, &acp); err != nil {
		return ctrl.Result{}, err
	}
	log.WithValues("openshiftassisted_control_plane", acp.Name, "openshiftassisted_control_plane_namespace", acp.Namespace)

	if err := r.reconcile(ctx, aci, &acp); err != nil {
		return ctrl.Result{}, err
	}

	// Check if AgentClusterInstall has moved to day 2 aka control plane is installed
	if isInstalled(aci) {
		acp.Status.Ready = true
		conditions.MarkTrue(&acp, controlplanev1alpha2.ControlPlaneReadyCondition)
		return ctrl.Result{}, r.updateControlplaneStatus(ctx, &acp)
	}
	conditions.MarkFalse(
		&acp,
		controlplanev1alpha2.ControlPlaneReadyCondition,
		controlplanev1alpha2.ControlPlaneInstallingReason,
		clusterv1.ConditionSeverityInfo,
		"Controlplane installing, status: %s",
		aci.Status.DebugInfo.State,
	)
	return ctrl.Result{}, r.updateControlplaneStatus(ctx, &acp)
}

func (r *AgentClusterInstallReconciler) reconcile(
	ctx context.Context,
	aci *hiveext.AgentClusterInstall,
	acp *controlplanev1alpha2.OpenshiftAssistedControlPlane,
) error {
	if !hasKubeconfigRef(aci) {
		conditions.MarkFalse(
			acp,
			controlplanev1alpha2.KubeconfigAvailableCondition,
			controlplanev1alpha2.KubeconfigUnavailableFailedReason,
			clusterv1.ConditionSeverityInfo,
			"Kubeconfig not available",
		)
		return nil
	}

	kubeconfigSecret, err := r.getACIKubeconfig(ctx, aci, *acp)
	if err != nil {
		conditions.MarkFalse(
			acp,
			controlplanev1alpha2.KubeconfigAvailableCondition,
			controlplanev1alpha2.KubeconfigUnavailableFailedReason,
			clusterv1.ConditionSeverityInfo,
			"error retrieving Kubeconfig %v", err,
		)
		return err
	}

	clusterName := acp.Labels[clusterv1.ClusterNameLabel]
	labels := map[string]string{
		clusterv1.ClusterNameLabel: clusterName,
	}

	if err := r.updateLabels(ctx, kubeconfigSecret, labels); err != nil {
		conditions.MarkFalse(
			acp,
			controlplanev1alpha2.KubeconfigAvailableCondition,
			controlplanev1alpha2.KubeconfigUnavailableFailedReason,
			clusterv1.ConditionSeverityInfo,
			"error updating Kubeconfig secret labels %v", err,
		)
		return err
	}

	if !r.ClusterKubeconfigSecretExists(ctx, clusterName, acp.Namespace) {
		if err := r.createKubeconfig(ctx, kubeconfigSecret, clusterName, *acp); err != nil {
			conditions.MarkFalse(
				acp,
				controlplanev1alpha2.KubeconfigAvailableCondition,
				controlplanev1alpha2.KubeconfigUnavailableFailedReason,
				clusterv1.ConditionSeverityInfo,
				"error creating Kubeconfig secret: %v", err,
			)
			return err
		}
	}
	conditions.MarkTrue(acp, controlplanev1alpha2.KubeconfigAvailableCondition)

	acp.Status.Initialized = true
	if err := r.Client.Status().Update(ctx, acp); err != nil {
		return err
	}
	return nil
}

func (r *AgentClusterInstallReconciler) createKubeconfig(
	ctx context.Context,
	kubeconfigSecret *corev1.Secret,
	clusterName string,
	acp controlplanev1alpha2.OpenshiftAssistedControlPlane,
) error {
	kubeconfig, ok := kubeconfigSecret.Data[kubeconfigSecretKey]
	if !ok {
		return fmt.Errorf("kubeconfig with key `%s` not found in secret %s", kubeconfigSecretKey, kubeconfigSecret.Name)
	}
	// Create secret <cluster-name>-kubeconfig from original kubeconfig secret - this is what the CAPI Cluster looks for to set the control plane as initialized
	clusterNameKubeconfigSecret := GenerateSecretWithOwner(
		client.ObjectKey{Name: clusterName, Namespace: acp.Namespace},
		kubeconfig,
		*metav1.NewControllerRef(&acp, controlplanev1alpha2.GroupVersion.WithKind(openshiftAssistedControlPlaneKind)),
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

func (r *AgentClusterInstallReconciler) updateLabels(
	ctx context.Context,
	obj client.Object,
	labels map[string]string,
) error {
	objLabels := obj.GetLabels()
	if len(objLabels) < 1 {
		objLabels = make(map[string]string)
	}

	for k, v := range labels {
		objLabels[k] = v
	}
	obj.SetLabels(objLabels)
	if err := r.Client.Update(ctx, obj); err != nil {
		return err
	}
	return nil
}

func (r *AgentClusterInstallReconciler) getACIKubeconfig(
	ctx context.Context,
	aci *hiveext.AgentClusterInstall,
	openshiftAssistedCP controlplanev1alpha2.OpenshiftAssistedControlPlane,
) (*corev1.Secret, error) {
	secretName := aci.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name

	// Get the kubeconfig secret and label with capi key pair cluster.x-k8s.io/cluster-name=<cluster name>
	kubeconfigSecret := &corev1.Secret{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: secretName, Namespace: openshiftAssistedCP.Namespace}, kubeconfigSecret); err != nil {
		return nil, err
	}
	return kubeconfigSecret, nil
}

func hasKubeconfigRef(aci *hiveext.AgentClusterInstall) bool {
	return aci.Spec.ClusterMetadata != nil && aci.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name != ""
}

func isInstalled(aci *hiveext.AgentClusterInstall) bool {
	return aci.Status.DebugInfo.State == aimodels.ClusterStatusAddingHosts
}

func (r *AgentClusterInstallReconciler) ClusterKubeconfigSecretExists(
	ctx context.Context,
	clusterName, namespace string,
) bool {
	secretName := fmt.Sprintf("%s-kubeconfig", clusterName)
	kubeconfigSecret := &corev1.Secret{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: secretName, Namespace: namespace}, kubeconfigSecret); err != nil {
		return !apierrors.IsNotFound(err)
	}
	return true
}

func (r *AgentClusterInstallReconciler) updateControlplaneStatus(ctx context.Context, oacp *controlplanev1alpha2.OpenshiftAssistedControlPlane) error {
	if err := r.Client.Status().Update(ctx, oacp); err != nil {
		return err
	}
	return nil
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
