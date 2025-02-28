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
	"errors"
	"strings"
	"time"

	"github.com/openshift-assisted/cluster-api-agent/pkg/containers"

	"github.com/go-logr/logr"
	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/release"
	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/upgrade"

	semver "github.com/blang/semver/v4"
	"github.com/openshift-assisted/cluster-api-agent/assistedinstaller"
	bootstrapv1alpha1 "github.com/openshift-assisted/cluster-api-agent/bootstrap/api/v1alpha1"
	controlplanev1alpha2 "github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1alpha2"
	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/auth"
	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/version"
	"github.com/openshift-assisted/cluster-api-agent/util"
	logutil "github.com/openshift-assisted/cluster-api-agent/util/log"
	hivev1 "github.com/openshift/hive/apis/hive/v1"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apiserver/pkg/storage/names"
	"k8s.io/client-go/tools/reference"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/controllers/external"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

const (
	minOpenShiftVersion               = "4.14.0"
	openshiftAssistedControlPlaneKind = "OpenshiftAssistedControlPlane"
	acpFinalizer                      = "openshiftassistedcontrolplane." + controlplanev1alpha2.Group + "/deprovision"
	placeholderPullSecretName         = "placeholder-pull-secret"
)

// OpenshiftAssistedControlPlaneReconciler reconciles a OpenshiftAssistedControlPlane object
type OpenshiftAssistedControlPlaneReconciler struct {
	client.Client
	K8sVersionDetector version.KubernetesVersionDetector
	Scheme             *runtime.Scheme
	UpgradeFactory     upgrade.ClusterUpgradeFactory
}

var minVersion = semver.MustParse(minOpenShiftVersion)

// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=openshiftassistedconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=metal3machines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=metal3machinetemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinedeployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterimagesets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=agentclusterinstalls,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=agentclusterinstalls/status,verbs=get
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments/status,verbs=get
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=openshiftassistedcontrolplanes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=openshiftassistedcontrolplanes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=openshiftassistedcontrolplanes/finalizers,verbs=update
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinepools,verbs=list
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *OpenshiftAssistedControlPlaneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, rerr error) {
	log := ctrl.LoggerFrom(ctx)

	oacp := &controlplanev1alpha2.OpenshiftAssistedControlPlane{}
	if err := r.Client.Get(ctx, req.NamespacedName, oacp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.WithValues("openshift_assisted_control_plane", oacp.Name, "openshift_assisted_control_plane_namespace", oacp.Namespace)
	log.V(logutil.TraceLevel).Info("Started reconciling OpenshiftAssistedControlPlane")

	// Initialize the patch helper.
	patchHelper, err := patch.NewHelper(oacp, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Attempt to Patch the OpenshiftAssistedControlPlane object and status after each reconciliation if no error occurs.
	defer func() {
		conditions.SetSummary(oacp,
			conditions.WithConditions(
				clusterv1.MachinesReadyCondition,
				controlplanev1alpha2.KubeconfigAvailableCondition,
				controlplanev1alpha2.ControlPlaneReadyCondition,
				controlplanev1alpha2.MachinesCreatedCondition,
			),
		)

		// Patch ObservedGeneration only if the reconciliation completed successfully
		patchOpts := []patch.Option{}
		if rerr == nil {
			patchOpts = append(patchOpts, patch.WithStatusObservedGeneration{})
		}
		if err := patchHelper.Patch(ctx, oacp, patchOpts...); err != nil {
			rerr = kerrors.NewAggregate([]error{rerr, err})
		}

		log.V(logutil.TraceLevel).Info("Finished reconciling OpenshiftAssistedControlPlane")
	}()

	if oacp.DeletionTimestamp != nil {
		return ctrl.Result{}, r.handleDeletion(ctx, oacp)
	}

	if !controllerutil.ContainsFinalizer(oacp, acpFinalizer) {
		controllerutil.AddFinalizer(oacp, acpFinalizer)
	}

	acpVersion, err := semver.ParseTolerant(oacp.Spec.DistributionVersion)
	if err != nil {
		// we accept any format (i.e. latest)
		log.V(logutil.WarningLevel).Info("invalid OpenShift version", "version", oacp.Spec.DistributionVersion)
	}
	if err == nil && acpVersion.LT(minVersion) {
		conditions.MarkFalse(oacp, controlplanev1alpha2.MachinesCreatedCondition, controlplanev1alpha2.MachineGenerationFailedReason,
			clusterv1.ConditionSeverityError, "version %v is not supported, the minimum supported version is %s", oacp.Spec.DistributionVersion, minOpenShiftVersion)
		return ctrl.Result{}, nil
	}

	cluster, err := capiutil.GetOwnerCluster(ctx, r.Client, oacp.ObjectMeta)
	if err != nil {
		log.Error(err, "Failed to retrieve owner Cluster from the API Server")
		return ctrl.Result{}, err
	}
	if cluster == nil {
		log.V(logutil.TraceLevel).Info("Cluster Controller has not yet set OwnerRef")
		return ctrl.Result{Requeue: true, RequeueAfter: 10 * time.Second}, nil
	}

	if annotations.IsPaused(cluster, oacp) {
		log.V(logutil.TraceLevel).Info("Reconciliation is paused for this object")
		return ctrl.Result{}, nil
	}

	if !cluster.Status.InfrastructureReady || !cluster.Spec.ControlPlaneEndpoint.IsValid() {
		return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 20}, nil
	}

	if err := r.ensurePullSecret(ctx, oacp); err != nil {
		log.Error(err, "failed to ensure a pull secret exists")
		return ctrl.Result{}, err
	}

	if err := r.ensureClusterDeployment(ctx, oacp, cluster.Name); err != nil {
		log.Error(err, "failed to ensure a ClusterDeployment exists")
		return ctrl.Result{}, err
	}
	pullsecret, err := auth.GetPullSecret(r.Client, ctx, oacp)
	if err != nil {
		return ctrl.Result{}, err
	}
	releaseImage := getReleaseImage(*oacp)

	k8sVersion, err := r.K8sVersionDetector.GetKubernetesVersion(releaseImage, string(pullsecret))
	markKubernetesVersionCondition(oacp, err)
	// if image not found, mark upgrade unavailable condition
	if errors.Is(err, containers.ErrImageNotFound) {
		conditions.MarkFalse(
			oacp,
			controlplanev1alpha2.UpgradeAvailableCondition,
			controlplanev1alpha2.UpgradeImageUnavailableReason,
			clusterv1.ConditionSeverityError,
			"upgrade unavailable: %s", err.Error(),
		)
		return ctrl.Result{}, err
	}
	oacp.Status.Version = k8sVersion
	result := ctrl.Result{}
	if conditions.IsTrue(oacp, controlplanev1alpha2.KubeconfigAvailableCondition) {
		// in case upgrade is still in progress, we want to requeue, however we also want to reconcile replicas
		result, err = r.upgradeWorkloadCluster(ctx, cluster, oacp, log, pullsecret)
		if err != nil {
			return result, err
		}
	}
	return result, r.reconcileReplicas(ctx, oacp, cluster)
}

func (r *OpenshiftAssistedControlPlaneReconciler) upgradeWorkloadCluster(ctx context.Context, cluster *clusterv1.Cluster, oacp *controlplanev1alpha2.OpenshiftAssistedControlPlane, log logr.Logger, pullSecret []byte) (ctrl.Result, error) {
	// Retrieve KubeConfig
	kubeConfig, err := util.GetWorkloadKubeconfig(ctx, r.Client, cluster.Name, cluster.Namespace)
	if err != nil {
		return ctrl.Result{}, err
	}

	upgrader, err := r.UpgradeFactory.NewUpgrader(kubeConfig)
	if err != nil {
		return ctrl.Result{}, err
	}
	isUpdateInProgress, err := upgrader.IsUpgradeInProgress(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}
	if isUpdateInProgress {
		log.V(logutil.DebugLevel).Info("update is in progress...")
		conditions.MarkFalse(
			oacp,
			controlplanev1alpha2.UpgradeCompletedCondition,
			controlplanev1alpha2.UpgradeInProgressReason,
			clusterv1.ConditionSeverityInfo,
			"upgrade to version %s in progress",
			oacp.Spec.DistributionVersion,
		)
	}
	oacp.Status.DistributionVersion, err = upgrader.GetCurrentVersion(ctx)
	if err != nil {
		log.V(logutil.WarningLevel).Info("failed to get OpenShift version from ClusterVersion", "error", err.Error())
	}

	// TODO: check for upgrade errors, mark relevant conditions
	isDesiredVersionUpdated, err := upgrader.IsDesiredVersionUpdated(ctx, oacp.Spec.DistributionVersion)
	if err != nil {
		return ctrl.Result{}, err
	}
	if isDesiredVersionUpdated && isUpdateInProgress {
		log.V(logutil.WarningLevel).Info("desired version is updated, but did not complete upgrade yet. Re-reconciling")
		return ctrl.Result{
			Requeue:      true,
			RequeueAfter: 1 * time.Minute,
		}, nil
	}

	if isWorkloadClusterRunningDesiredVersion(oacp) && !isUpdateInProgress {
		log.V(logutil.WarningLevel).Info("Cluster is now running expected version, upgraded completed")

		conditions.MarkTrue(oacp, controlplanev1alpha2.UpgradeCompletedCondition)
		return ctrl.Result{}, nil
	}

	// once updating, requeue to check update status
	return ctrl.Result{
			Requeue:      true,
			RequeueAfter: 1 * time.Minute,
		},
		upgrader.UpdateClusterVersionDesiredUpdate(
			ctx,
			oacp.Spec.DistributionVersion,
			getUpgradeOptions(oacp, pullSecret)...,
		)
}

func getUpgradeOptions(oacp *controlplanev1alpha2.OpenshiftAssistedControlPlane, pullSecret []byte) []upgrade.ClusterUpgradeOption {
	upgradeOptions := []upgrade.ClusterUpgradeOption{
		{
			Name:  upgrade.ReleaseImagePullSecretOption,
			Value: string(pullSecret),
		},
	}
	if repo, ok := oacp.Annotations[release.ReleaseImageRepositoryOverrideAnnotation]; ok {
		upgradeOptions = append(upgradeOptions, upgrade.ClusterUpgradeOption{
			Name:  upgrade.ReleaseImageRepositoryOverrideOption,
			Value: repo,
		})
	}
	return upgradeOptions
}

func isWorkloadClusterRunningDesiredVersion(oacp *controlplanev1alpha2.OpenshiftAssistedControlPlane) bool {
	// TODO: Spec.DistributionVersion has architecture suffix, we need to remove it and compose it from arch field
	return strings.HasPrefix(oacp.Spec.DistributionVersion, oacp.Status.DistributionVersion)
}

func markKubernetesVersionCondition(oacp *controlplanev1alpha2.OpenshiftAssistedControlPlane, err error) {
	if err != nil {
		conditions.MarkFalse(
			oacp,
			controlplanev1alpha2.KubernetesVersionAvailableCondition,
			controlplanev1alpha2.KubernetesVersionUnavailableFailedReason,
			clusterv1.ConditionSeverityWarning,
			"failed to get k8s version from release image: %v",
			err,
		)
	} else {
		conditions.MarkTrue(oacp, controlplanev1alpha2.KubernetesVersionAvailableCondition)
	}
}

// Ensures dependencies are deleted before allowing the OpenshiftAssistedControlPlane to be deleted
// Deletes the ClusterDeployment (which deletes the AgentClusterInstall)
// Machines, InfraMachines, and OpenshiftAssistedConfigs get auto-deleted when the ACP has a deletion timestamp - this deprovisions the BMH automatically
// TODO: should we handle watching until all machines & openshiftassistedconfigs are deleted too?
func (r *OpenshiftAssistedControlPlaneReconciler) handleDeletion(ctx context.Context, acp *controlplanev1alpha2.OpenshiftAssistedControlPlane) error {
	log := ctrl.LoggerFrom(ctx)

	if !controllerutil.ContainsFinalizer(acp, acpFinalizer) {
		log.V(logutil.TraceLevel).Info("ACP doesn't contain finalizer, allow deletion")
		return nil
	}

	// Delete cluster deployment
	if err := r.deleteClusterDeployment(ctx, acp.Status.ClusterDeploymentRef); err != nil &&
		!apierrors.IsNotFound(err) {
		log.Error(err, "failed deleting cluster deployment for ACP")
		return err
	}
	acp.Status.ClusterDeploymentRef = nil

	// will be updated in the deferred function
	controllerutil.RemoveFinalizer(acp, acpFinalizer)
	return nil
}

func (r *OpenshiftAssistedControlPlaneReconciler) deleteClusterDeployment(
	ctx context.Context,
	clusterDeployment *corev1.ObjectReference,
) error {
	if clusterDeployment == nil {
		return nil
	}
	cd := &hivev1.ClusterDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterDeployment.Name,
			Namespace: clusterDeployment.Namespace,
		},
	}
	return r.Client.Delete(ctx, cd)
}

func (r *OpenshiftAssistedControlPlaneReconciler) computeDesiredMachine(acp *controlplanev1alpha2.OpenshiftAssistedControlPlane, name, clusterName string) *clusterv1.Machine {
	var machineUID types.UID
	annotations := map[string]string{
		"bmac.agent-install.openshift.io/role": "master",
	}

	// Creating a new machine

	desiredMachine := &clusterv1.Machine{
		TypeMeta: metav1.TypeMeta{
			APIVersion: clusterv1.GroupVersion.String(),
			Kind:       "Machine",
		},
		ObjectMeta: metav1.ObjectMeta{
			UID:         machineUID,
			Name:        name,
			Namespace:   acp.Namespace,
			Labels:      map[string]string{},
			Annotations: map[string]string{},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: clusterName,
			// TODO: add distribution version
		},
	}

	// Note: by setting the ownerRef on creation we signal to the Machine controller that this is not a stand-alone Machine.
	_ = controllerutil.SetOwnerReference(acp, desiredMachine, r.Scheme)

	// Set the in-place mutable fields.
	// When we create a new Machine we will just create the Machine with those fields.
	// When we update an existing Machine will we update the fields on the existing Machine (in-place mutate).

	// Set labels
	desiredMachine.Labels = util.ControlPlaneMachineLabelsForCluster(acp, clusterName)

	// Set annotations
	// Add the annotations from the MachineTemplate.
	// Note: we intentionally don't use the map directly to ensure we don't modify the map in KCP.
	for k, v := range acp.Spec.MachineTemplate.ObjectMeta.Annotations {
		desiredMachine.Annotations[k] = v
	}
	for k, v := range annotations {
		desiredMachine.Annotations[k] = v
	}

	// Set other in-place mutable fields
	desiredMachine.Spec.NodeDrainTimeout = acp.Spec.MachineTemplate.NodeDrainTimeout
	desiredMachine.Spec.NodeDeletionTimeout = acp.Spec.MachineTemplate.NodeDeletionTimeout
	desiredMachine.Spec.NodeVolumeDetachTimeout = acp.Spec.MachineTemplate.NodeVolumeDetachTimeout

	return desiredMachine
}

// SetupWithManager sets up the controller with the Manager.
func (r *OpenshiftAssistedControlPlaneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	//TODO: maybe enqueue for clusterdeployment owned by this ACP in case it gets deleted...?
	return ctrl.NewControllerManagedBy(mgr).
		For(&controlplanev1alpha2.OpenshiftAssistedControlPlane{}).
		Watches(
			&clusterv1.Machine{},
			handler.EnqueueRequestForOwner(r.Scheme, mgr.GetRESTMapper(), &controlplanev1alpha2.OpenshiftAssistedControlPlane{}),
		).
		Complete(r)
}

func (r *OpenshiftAssistedControlPlaneReconciler) ensureClusterDeployment(
	ctx context.Context,
	acp *controlplanev1alpha2.OpenshiftAssistedControlPlane,
	clusterName string,
) error {
	if acp.Status.ClusterDeploymentRef == nil {
		clusterDeployment := assistedinstaller.GetClusterDeploymentFromConfig(acp, clusterName)
		_ = controllerutil.SetOwnerReference(acp, clusterDeployment, r.Scheme)
		if _, err := ctrl.CreateOrUpdate(ctx, r.Client, clusterDeployment, func() error { return nil }); err != nil {
			return err
		}
		ref, err := reference.GetReference(r.Scheme, clusterDeployment)
		if err != nil {
			return err
		}
		acp.Status.ClusterDeploymentRef = ref
		return nil
	}

	// Retrieve clusterdeployment
	cd := &hivev1.ClusterDeployment{}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: acp.Status.ClusterDeploymentRef.Namespace, Name: acp.Status.ClusterDeploymentRef.Name}, cd); err != nil {
		if apierrors.IsNotFound(err) {
			// Cluster deployment no longer exists, unset reference and re-reconcile
			acp.Status.ClusterDeploymentRef = nil
			return nil
		}
		return err
	}
	return nil
}

func (r *OpenshiftAssistedControlPlaneReconciler) reconcileReplicas(ctx context.Context, acp *controlplanev1alpha2.OpenshiftAssistedControlPlane, cluster *clusterv1.Cluster) error {
	machines, err := collections.GetFilteredMachinesForCluster(ctx, r.Client, cluster, collections.OwnedMachines(acp))
	if err != nil {
		return err
	}

	numMachines := machines.Len()
	desiredReplicas := int(acp.Spec.Replicas)
	machinesToCreate := desiredReplicas - numMachines
	created := 0
	var errs []error
	if machinesToCreate > 0 {
		for i := 0; i < machinesToCreate; i++ {
			if err := r.scaleUpControlPlane(ctx, acp, cluster.Name); err != nil {
				errs = append(errs, err)
				continue
			}
			created++
		}
	}
	updateReplicaStatus(acp, machines, created)
	return kerrors.NewAggregate(errs)
}

func (r *OpenshiftAssistedControlPlaneReconciler) scaleUpControlPlane(ctx context.Context, acp *controlplanev1alpha2.OpenshiftAssistedControlPlane, clusterName string) error {
	name := names.SimpleNameGenerator.GenerateName(acp.Name + "-")
	machine, err := r.generateMachine(ctx, acp, name, clusterName)
	if err != nil {
		return err
	}
	bootstrapConfig := r.generateOpenshiftAssistedConfig(acp, clusterName, name)
	_ = controllerutil.SetOwnerReference(acp, bootstrapConfig, r.Scheme)
	if err := r.Client.Create(ctx, bootstrapConfig); err != nil {
		conditions.MarkFalse(acp, controlplanev1alpha2.MachinesCreatedCondition, controlplanev1alpha2.BootstrapTemplateCloningFailedReason,
			clusterv1.ConditionSeverityError, "error creating bootstrap config: %v", err)
		return err
	}
	bootstrapRef, err := reference.GetReference(r.Scheme, bootstrapConfig)
	if err != nil {
		return err
	}
	machine.Spec.Bootstrap.ConfigRef = bootstrapRef
	if err := r.Client.Create(ctx, machine); err != nil {
		conditions.MarkFalse(acp, controlplanev1alpha2.MachinesCreatedCondition,
			controlplanev1alpha2.MachineGenerationFailedReason,
			clusterv1.ConditionSeverityError, "error creating machine %v", err)
		if deleteErr := r.Client.Delete(ctx, bootstrapConfig); deleteErr != nil {
			err = errors.Join(err, deleteErr)
		}
		if deleteErr := external.Delete(ctx, r.Client, &machine.Spec.InfrastructureRef); deleteErr != nil {
			err = errors.Join(err, deleteErr)
		}
		return err
	}
	return nil
}

func updateReplicaStatus(acp *controlplanev1alpha2.OpenshiftAssistedControlPlane, machines collections.Machines, updatedMachines int) {
	desiredReplicas := acp.Spec.Replicas
	readyMachines := machines.Filter(collections.IsReady()).Len()

	acp.Status.Replicas = int32(machines.Len())
	acp.Status.UpdatedReplicas = int32(updatedMachines)
	acp.Status.UnavailableReplicas = desiredReplicas - int32(readyMachines)
	acp.Status.ReadyReplicas = int32(readyMachines)
	if acp.Status.ReadyReplicas == desiredReplicas {
		conditions.MarkTrue(acp, controlplanev1alpha2.MachinesCreatedCondition)
	}

	// Aggregate the operational state of all the machines; while aggregating we are adding the
	// source ref (reason@machine/name) so the problem can be easily tracked down to its source machine.
	conditions.SetAggregate(acp,
		clusterv1.MachinesReadyCondition,
		machines.ConditionGetters(),
		conditions.AddSourceRef(),
		conditions.WithStepCounterIf(false))
}

func (r *OpenshiftAssistedControlPlaneReconciler) generateMachine(ctx context.Context, acp *controlplanev1alpha2.OpenshiftAssistedControlPlane, name, clusterName string) (*clusterv1.Machine, error) {
	// Compute desired Machine
	machine := r.computeDesiredMachine(acp, name, clusterName)
	infraRef, err := r.computeInfraRef(ctx, acp, machine.Name, clusterName)
	if err != nil {
		return nil, err
	}
	machine.Spec.InfrastructureRef = *infraRef
	return machine, nil
}

func (r *OpenshiftAssistedControlPlaneReconciler) computeInfraRef(ctx context.Context, acp *controlplanev1alpha2.OpenshiftAssistedControlPlane, machineName, clusterName string) (*corev1.ObjectReference, error) {
	// Since the cloned resource should eventually have a controller ref for the Machine, we create an
	// OwnerReference here without the Controller field set
	infraCloneOwner := &metav1.OwnerReference{
		APIVersion: controlplanev1alpha2.GroupVersion.String(),
		Kind:       openshiftAssistedControlPlaneKind,
		Name:       acp.Name,
		UID:        acp.UID,
	}

	// Clone the infrastructure template
	infraRef, err := external.CreateFromTemplate(ctx, &external.CreateFromTemplateInput{
		Client:      r.Client,
		TemplateRef: &acp.Spec.MachineTemplate.InfrastructureRef,
		Namespace:   acp.Namespace,
		Name:        machineName,
		OwnerRef:    infraCloneOwner,
		ClusterName: clusterName,
		Labels:      util.ControlPlaneMachineLabelsForCluster(acp, clusterName),
		Annotations: acp.Spec.MachineTemplate.ObjectMeta.Annotations,
	})
	if err != nil {
		// Safe to return early here since no resources have been created yet.
		conditions.MarkFalse(acp, controlplanev1alpha2.MachinesCreatedCondition, controlplanev1alpha2.InfrastructureTemplateCloningFailedReason,
			clusterv1.ConditionSeverityError, "error creating infraenv: %v", err)
		return nil, err
	}
	return infraRef, nil
}

func (r *OpenshiftAssistedControlPlaneReconciler) generateOpenshiftAssistedConfig(acp *controlplanev1alpha2.OpenshiftAssistedControlPlane, clusterName string, name string) *bootstrapv1alpha1.OpenshiftAssistedConfig {
	bootstrapConfig := &bootstrapv1alpha1.OpenshiftAssistedConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   acp.Namespace,
			Labels:      util.ControlPlaneMachineLabelsForCluster(acp, clusterName),
			Annotations: acp.Spec.MachineTemplate.ObjectMeta.Annotations,
		},
		Spec: *acp.Spec.OpenshiftAssistedConfigSpec.DeepCopy(),
	}

	_ = controllerutil.SetOwnerReference(acp, bootstrapConfig, r.Scheme)
	return bootstrapConfig
}

func (r *OpenshiftAssistedControlPlaneReconciler) ensurePullSecret(
	ctx context.Context,
	acp *controlplanev1alpha2.OpenshiftAssistedControlPlane,
) error {
	if acp.Spec.Config.PullSecretRef != nil {
		return nil
	}

	secret := auth.GenerateFakePullSecret(placeholderPullSecretName, acp.Namespace)
	if err := controllerutil.SetOwnerReference(acp, secret, r.Scheme); err != nil {
		return err
	}

	if err := r.Client.Create(ctx, secret); err != nil {
		return err
	}
	acp.Spec.Config.PullSecretRef = &corev1.LocalObjectReference{Name: secret.Name}
	return nil
}
