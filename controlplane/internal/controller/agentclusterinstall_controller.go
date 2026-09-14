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
	"strings"
	"time"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util"
	logutil "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util/log"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aimodels "github.com/openshift/assisted-service/models"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	capiutil "sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	kubeconfigSecretKey = "kubeconfig"
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

// +kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=agentclusterinstalls,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=agentclusterinstalls/status,verbs=get;patch
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=openshiftassistedcontrolplanes,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=openshiftassistedcontrolplanes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=infraenvs,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch

func (r *AgentClusterInstallReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, rerr error) {
	log := ctrl.LoggerFrom(ctx)

	defer func() {
		log.V(logutil.DebugLevel).Info("agent cluster install reconcile ended")
	}()

	log.V(logutil.DebugLevel).Info("agent cluster install reconcile started")
	aci := &hiveext.AgentClusterInstall{}
	if err := r.Get(ctx, req.NamespacedName, aci); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	log.WithValues("agent_cluster_install", aci.Name, "agent_cluster_install_namespace", aci.Namespace)

	oacp := controlplanev1alpha3.OpenshiftAssistedControlPlane{}
	if err := util.GetTypedOwner(ctx, r.Client, aci, &oacp); err != nil {
		return ctrl.Result{}, err
	}
	log.WithValues("openshiftassisted_control_plane", oacp.Name, "openshiftassisted_control_plane_namespace", oacp.Namespace)

	// Capture the patch base before any in-memory mutations so that
	// the deferred status patch sends only the fields we actually changed.
	statusPatchBase := client.MergeFrom(oacp.DeepCopy())
	defer func() {
		if patchErr := r.Client.Status().Patch(ctx, &oacp, statusPatchBase); patchErr != nil {
			log.Error(patchErr, "failed to patch OpenshiftAssistedControlPlane status")
			if rerr == nil {
				rerr = patchErr
			}
		}
	}()

	cluster, err := capiutil.GetOwnerCluster(ctx, r.Client, oacp.ObjectMeta)
	if err != nil {
		log.Error(err, "failed to retrieve owner Cluster")
		return ctrl.Result{}, err
	}
	if cluster == nil {
		log.V(logutil.DebugLevel).Info("owner Cluster not set yet, requeuing")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	if err := r.reconcile(ctx, aci, &oacp, cluster); err != nil {
		return ctrl.Result{}, err
	}

	// Check if AgentClusterInstall has reached finalizing or day 2 (adding-hosts) state
	if isAvailable(aci) {
		oacp.Status.Initialization.ControlPlaneInitialized = ptr.To(true)
		setConditionTrue(&oacp, controlplanev1alpha3.ControlPlaneAvailableCondition)
		return ctrl.Result{}, nil
	}
	setConditionFalse(&oacp, controlplanev1alpha3.ControlPlaneAvailableCondition,
		controlplanev1alpha3.ControlPlaneInstallingReason,
		"Controlplane installing, status: %s", aci.Status.DebugInfo.State)
	return ctrl.Result{}, nil
}

func (r *AgentClusterInstallReconciler) reconcile(
	ctx context.Context,
	aci *hiveext.AgentClusterInstall,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	cluster *clusterv1.Cluster,
) error {
	if !hasKubeconfigRef(aci) {
		setConditionFalse(oacp, controlplanev1alpha3.KubeconfigAvailableCondition,
			controlplanev1alpha3.KubeconfigUnavailableFailedReason, "Kubeconfig not available")
		return nil
	}

	kubeconfigSecret, err := r.getACIKubeconfig(ctx, aci, *oacp)
	if err != nil {
		setConditionFalse(oacp, controlplanev1alpha3.KubeconfigAvailableCondition,
			controlplanev1alpha3.KubeconfigUnavailableFailedReason,
			"error retrieving Kubeconfig %v", err)
		return err
	}

	clusterName := oacp.Labels[clusterv1.ClusterNameLabel]
	labels := map[string]string{
		clusterv1.ClusterNameLabel: clusterName,
	}

	if err := r.updateLabels(ctx, kubeconfigSecret, labels); err != nil {
		setConditionFalse(oacp, controlplanev1alpha3.KubeconfigAvailableCondition,
			controlplanev1alpha3.KubeconfigUnavailableFailedReason,
			"error updating Kubeconfig secret labels %v", err)
		return err
	}

	if !r.ClusterKubeconfigSecretExists(ctx, clusterName, oacp.Namespace) {
		if err := r.createKubeconfig(ctx, kubeconfigSecret, clusterName, *oacp, cluster); err != nil {
			setConditionFalse(oacp, controlplanev1alpha3.KubeconfigAvailableCondition,
				controlplanev1alpha3.KubeconfigUnavailableFailedReason,
				"error creating Kubeconfig secret: %v", err)
			return err
		}
	}
	setConditionTrue(oacp, controlplanev1alpha3.KubeconfigAvailableCondition)

	oacp.Status.Initialization.ControlPlaneInitialized = ptr.To(true)
	return nil
}

func (r *AgentClusterInstallReconciler) createKubeconfig(
	ctx context.Context,
	kubeconfigSecret *corev1.Secret,
	clusterName string,
	acp controlplanev1alpha3.OpenshiftAssistedControlPlane,
	cluster *clusterv1.Cluster,
) error {
	kubeconfig, ok := kubeconfigSecret.Data[kubeconfigSecretKey]
	if !ok {
		return fmt.Errorf("kubeconfig with key `%s` not found in secret %s", kubeconfigSecretKey, kubeconfigSecret.Name)
	}

	// When using Route-based access (KubeVirt), rewrite the kubeconfig server URL
	// from port 6443 to port 443 so it goes through the infra router's passthrough Route.
	// The hostname stays the same (api.<cluster>.<baseDomain>), preserving TLS validity.
	if isKubeVirtInfra(cluster) {
		kubeconfig = rewriteKubeconfigPort(kubeconfig)
	}

	// Create secret <cluster-name>-kubeconfig from original kubeconfig secret - this is what the CAPI Cluster looks for to set the control plane as initialized
	clusterNameKubeconfigSecret := GenerateSecretWithOwner(
		client.ObjectKey{Name: clusterName, Namespace: acp.Namespace},
		kubeconfig,
		*metav1.NewControllerRef(&acp, controlplanev1alpha3.GroupVersion.WithKind(openshiftAssistedControlPlaneKind)),
	)
	if err := r.Create(ctx, clusterNameKubeconfigSecret); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
		if err := r.Update(ctx, clusterNameKubeconfigSecret); err != nil {
			return err
		}
	}
	return nil
}

// rewriteKubeconfigPort replaces :6443 with :443 in the kubeconfig server URL.
// This allows the kubeconfig to work through the infra cluster's OpenShift Router
// (passthrough Route on port 443) while preserving the hostname for TLS validation.
func rewriteKubeconfigPort(kubeconfig []byte) []byte {
	config, err := clientcmd.Load(kubeconfig)
	if err != nil {
		return kubeconfig
	}
	modified := false
	for _, cluster := range config.Clusters {
		if strings.HasSuffix(cluster.Server, ":6443") {
			cluster.Server = strings.TrimSuffix(cluster.Server, ":6443") + ":443"
			modified = true
		} else if strings.Contains(cluster.Server, ":6443/") {
			cluster.Server = strings.Replace(cluster.Server, ":6443/", ":443/", 1)
			modified = true
		}
	}
	if !modified {
		return kubeconfig
	}
	out, err := clientcmd.Write(*config)
	if err != nil {
		return kubeconfig
	}
	return out
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
	if err := r.Update(ctx, obj); err != nil {
		return err
	}
	return nil
}

func (r *AgentClusterInstallReconciler) getACIKubeconfig(
	ctx context.Context,
	aci *hiveext.AgentClusterInstall,
	openshiftAssistedCP controlplanev1alpha3.OpenshiftAssistedControlPlane,
) (*corev1.Secret, error) {
	secretName := aci.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name

	// Get the kubeconfig secret and label with capi key pair cluster.x-k8s.io/cluster-name=<cluster name>
	kubeconfigSecret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Name: secretName, Namespace: openshiftAssistedCP.Namespace}, kubeconfigSecret); err != nil {
		return nil, err
	}
	return kubeconfigSecret, nil
}

func hasKubeconfigRef(aci *hiveext.AgentClusterInstall) bool {
	return aci.Spec.ClusterMetadata != nil && aci.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name != ""
}

func isAvailable(aci *hiveext.AgentClusterInstall) bool {
	state := aci.Status.DebugInfo.State
	return state == aimodels.ClusterStatusFinalizing || state == aimodels.ClusterStatusAddingHosts
}

func (r *AgentClusterInstallReconciler) ClusterKubeconfigSecretExists(
	ctx context.Context,
	clusterName, namespace string,
) bool {
	secretName := fmt.Sprintf("%s-kubeconfig", clusterName)
	kubeconfigSecret := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Name: secretName, Namespace: namespace}, kubeconfigSecret)
	return err == nil
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
