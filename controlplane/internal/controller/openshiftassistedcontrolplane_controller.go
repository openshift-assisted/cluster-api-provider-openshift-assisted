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
	"fmt"
	"time"

	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/assistedinstaller"

	semver "github.com/blang/semver/v4"

	bootstrapv1alpha2 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/bootstrap/api/v1alpha2"
	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/auth"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/release"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/upgrade"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/version"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/pkg/containers"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util/failuredomains"
	logutil "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util/log"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/openshift/hive/apis/hive/v1/agent"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apiserver/pkg/storage/names"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
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
	oacpFinalizer                     = "openshiftassistedcontrolplane." + controlplanev1alpha3.Group + "/deprovision"
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
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=*,verbs=get;list;watch;create;update;patch;delete
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
// +kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclustersets/join,verbs=create
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *OpenshiftAssistedControlPlaneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, rerr error) {
	log := ctrl.LoggerFrom(ctx)

	oacp := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
	if err := r.Get(ctx, req.NamespacedName, oacp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.WithValues("openshift_assisted_control_plane", oacp.Name, "openshift_assisted_control_plane_namespace", oacp.Namespace)
	log.V(logutil.DebugLevel).Info("started reconciling OpenshiftAssistedControlPlane")

	// Initialize the patch helper.
	patchHelper, err := patch.NewHelper(oacp, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Attempt to Patch the OpenshiftAssistedControlPlane object and status after each reconciliation if no error occurs.
	defer func() {
		// Set Ready condition as summary of key conditions
		_ = conditions.SetSummaryCondition(oacp, oacp, string(clusterv1.ReadyCondition),
			conditions.ForConditionTypes{
				string(clusterv1.MachinesReadyCondition),
				string(controlplanev1alpha3.KubeconfigAvailableCondition),
				string(controlplanev1alpha3.ControlPlaneReadyCondition),
				string(controlplanev1alpha3.MachinesCreatedCondition),
			},
		)

		// Patch ObservedGeneration only if the reconciliation completed successfully
		patchOpts := []patch.Option{}
		if rerr == nil {
			patchOpts = append(patchOpts, patch.WithStatusObservedGeneration{})
		}
		if err := patchHelper.Patch(ctx, oacp, patchOpts...); err != nil {
			rerr = kerrors.NewAggregate([]error{rerr, err})
		}

		log.V(logutil.DebugLevel).Info("finished reconciling OpenshiftAssistedControlPlane")
	}()

	if oacp.DeletionTimestamp != nil {
		return ctrl.Result{}, r.handleDeletion(ctx, oacp)
	}

	if !controllerutil.ContainsFinalizer(oacp, oacpFinalizer) {
		controllerutil.AddFinalizer(oacp, oacpFinalizer)
	}

	oacpVersion, err := semver.ParseTolerant(oacp.Spec.DistributionVersion)
	if err != nil {
		// we accept any format (i.e. latest)
		log.V(logutil.DebugLevel).Info("invalid OpenShift version", "version", oacp.Spec.DistributionVersion)
	}
	if err == nil && oacpVersion.LT(minVersion) {
		setConditionFalse(oacp, controlplanev1alpha3.MachinesCreatedCondition, controlplanev1alpha3.MachineGenerationFailedReason,
			"version %v is not supported, the minimum supported version is %s", oacp.Spec.DistributionVersion, minOpenShiftVersion)
		return ctrl.Result{}, nil
	}

	cluster, err := capiutil.GetOwnerCluster(ctx, r.Client, oacp.ObjectMeta)
	if err != nil {
		log.Error(err, "failed to retrieve owner Cluster from the API Server")
		return ctrl.Result{}, err
	}
	if cluster == nil {
		log.V(logutil.DebugLevel).Info("cluster Controller has not yet set OwnerRef")
		return ctrl.Result{Requeue: true, RequeueAfter: 10 * time.Second}, nil
	}

	if annotations.IsPaused(cluster, oacp) {
		log.V(logutil.DebugLevel).Info("reconciliation is paused for this object")
		return ctrl.Result{}, nil
	}

	log.V(logutil.TraceLevel).Info("validation passed")

	if !isInfrastructureProvisioned(cluster) || !cluster.Spec.ControlPlaneEndpoint.IsValid() {
		return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 20}, nil
	}
	log.V(logutil.TraceLevel).Info("infra provisioned")

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
	architecture, err := getArchitectureFromBootstrapConfigs(ctx, r.Client, oacp)
	if err != nil {
		return ctrl.Result{}, err
	}
	releaseImage := getReleaseImage(*oacp, architecture)

	k8sVersion, err := r.K8sVersionDetector.GetKubernetesVersion(releaseImage, string(pullsecret))
	markKubernetesVersionCondition(oacp, err)
	// if image not found, mark upgrade unavailable condition
	if errors.Is(err, containers.ErrImageNotFound) {
		setConditionFalse(oacp, controlplanev1alpha3.UpgradeAvailableCondition, controlplanev1alpha3.UpgradeImageUnavailableReason,
			"upgrade unavailable: %s", err.Error())
		return ctrl.Result{}, err
	}
	oacp.Status.Version = *k8sVersion
	result := ctrl.Result{}
	if conditions.IsTrue(oacp, string(controlplanev1alpha3.KubeconfigAvailableCondition)) {
		// in case upgrade is still in progress, we want to requeue, however we also want to reconcile replicas
		result, err = r.upgradeWorkloadCluster(ctx, cluster, oacp, architecture, pullsecret)
		if err != nil {
			return result, err
		}
	}
	return result, r.reconcileReplicas(ctx, oacp, cluster)
}

func isInfrastructureProvisioned(cluster *clusterv1.Cluster) bool {
	return cluster.Status.Initialization.InfrastructureProvisioned != nil && *(cluster.Status.Initialization.InfrastructureProvisioned)
}

func getArchitectureFromBootstrapConfigs(ctx context.Context, k8sClient client.Client, oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane) (string, error) {
	defaultArch := "multi"

	// if oacp is nil, return default arch
	if oacp == nil {
		return defaultArch, nil
	}

	// if no clusterName label available, return default arch
	clusterName, ok := oacp.Labels[clusterv1.ClusterNameLabel]
	if !ok {
		return defaultArch, nil
	}
	labelSelector := map[string]string{
		clusterv1.ClusterNameLabel: clusterName,
	}
	listOptions := []client.ListOption{
		client.InNamespace(oacp.Namespace),
		client.MatchingLabels(labelSelector),
	}
	var configList bootstrapv1alpha2.OpenshiftAssistedConfigList
	if err := k8sClient.List(ctx, &configList, listOptions...); err != nil {
		return "", err
	}

	architectures := make([]string, 0)
	for _, config := range configList.Items {
		architectures = append(architectures, config.Spec.CpuArchitecture)
	}
	return getArchitecture(architectures, defaultArch), nil
}

func getArchitecture(architectures []string, defaultArchitecture string) string {
	// by default, return multi arch
	if len(architectures) < 1 {
		return defaultArchitecture
	}
	// if there is only one architecture, return it
	if len(architectures) == 1 {
		return architectures[0]
	}
	firstArch := architectures[0]
	for _, arch := range architectures {
		if arch != firstArch {
			return defaultArchitecture
		}
	}
	// if all architectures are the same, check for empty
	if firstArch == "" {
		return defaultArchitecture
	}
	return firstArch
}

func (r *OpenshiftAssistedControlPlaneReconciler) upgradeWorkloadCluster(ctx context.Context, cluster *clusterv1.Cluster, oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane, architecture string, pullSecret []byte) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	var isUpdateInProgress bool
	var upgradeConditionMessage string
	defer func() {
		if isUpdateInProgress || !isWorkloadClusterRunningDesiredVersion(oacp) {
			// Either upgrade is in progress or it failed
			setUpgradeStatus(oacp, isUpdateInProgress, upgradeConditionMessage)
			return
		}
		if conditions.IsFalse(oacp, string(controlplanev1alpha3.UpgradeCompletedCondition)) {
			setConditionTrue(oacp, controlplanev1alpha3.UpgradeCompletedCondition)
		}
	}()

	kubeConfig, err := util.GetWorkloadKubeconfig(ctx, r.Client, cluster.Name, cluster.Namespace)
	if err != nil {
		return ctrl.Result{}, err
	}

	upgrader, err := r.UpgradeFactory.NewUpgrader(kubeConfig)
	if err != nil {
		return ctrl.Result{}, err
	}
	isUpdateInProgress, err = upgrader.IsUpgradeInProgress(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}
	upgradeConditionMessage, err = upgrader.GetUpgradeStatus(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	oacp.Status.DistributionVersion, err = upgrader.GetCurrentVersion(ctx)
	if err != nil {
		log.V(logutil.DebugLevel).Info("failed to get OpenShift version from ClusterVersion", "error", err.Error())
	}

	// TODO: check for upgrade errors, mark relevant conditions
	isDesiredVersionUpdated, err := upgrader.IsDesiredVersionUpdated(ctx, oacp.Spec.DistributionVersion)
	if err != nil {
		return ctrl.Result{}, err
	}
	if isDesiredVersionUpdated && isUpdateInProgress {
		log.V(logutil.DebugLevel).Info("desired version is updated, but did not complete upgrade yet, re-reconciling")
		return ctrl.Result{
			Requeue:      true,
			RequeueAfter: 1 * time.Minute,
		}, nil
	}

	if isWorkloadClusterRunningDesiredVersion(oacp) && !isUpdateInProgress {
		log.V(logutil.DebugLevel).Info("cluster is now running expected version, upgrade completed")

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
			architecture,
			getUpgradeOptions(oacp, pullSecret)...,
		)
}

func setUpgradeStatus(oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane, upgradeInProgress bool, conditionMessage string) {
	reason := controlplanev1alpha3.UpgradeInProgressReason
	msg := "upgrade to version %s in progress\n%s"
	if !upgradeInProgress {
		reason = controlplanev1alpha3.UpgradeFailedReason
		msg = "upgrade to version %s has failed\n%s"
	}
	setConditionFalse(oacp, controlplanev1alpha3.UpgradeCompletedCondition, reason, msg, oacp.Spec.DistributionVersion, conditionMessage)
}

func getUpgradeOptions(oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane, pullSecret []byte) []upgrade.ClusterUpgradeOption {
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

func isWorkloadClusterRunningDesiredVersion(oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane) bool {
	return oacp.Spec.DistributionVersion == oacp.Status.DistributionVersion
}

func markKubernetesVersionCondition(oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane, err error) {
	if err != nil {
		setConditionFalse(oacp, controlplanev1alpha3.KubernetesVersionAvailableCondition,
			controlplanev1alpha3.KubernetesVersionUnavailableFailedReason,
			"failed to get k8s version from release image: %v", err)
		return
	}
	setConditionTrue(oacp, controlplanev1alpha3.KubernetesVersionAvailableCondition)
}

// Ensures dependencies are deleted before allowing the OpenshiftAssistedControlPlane to be deleted
// Deletes the ClusterDeployment (which deletes the AgentClusterInstall)
// Machines, InfraMachines, and OpenshiftAssistedConfigs get auto-deleted when the oacp has a deletion timestamp - this deprovisions the BMH automatically
// TODO: should we handle watching until all machines & openshiftassistedconfigs are deleted too?
func (r *OpenshiftAssistedControlPlaneReconciler) handleDeletion(ctx context.Context, oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane) error {
	log := ctrl.LoggerFrom(ctx)

	if !controllerutil.ContainsFinalizer(oacp, oacpFinalizer) {
		log.V(logutil.DebugLevel).Info("OACP doesn't contain finalizer, allow deletion")
		return nil
	}

	if err := r.Delete(ctx, &hivev1.ClusterDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      oacp.Name,
			Namespace: oacp.Namespace,
		},
	}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	// will be updated in the deferred function
	controllerutil.RemoveFinalizer(oacp, oacpFinalizer)
	return nil
}

func (r *OpenshiftAssistedControlPlaneReconciler) computeDesiredMachine(oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane, name string, cluster *clusterv1.Cluster, failureDomain string) *clusterv1.Machine {
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
			Namespace:   oacp.Namespace,
			Labels:      map[string]string{},
			Annotations: map[string]string{},
		},
	}

	desiredMachine.Spec.ClusterName = cluster.Name
	desiredMachine.Spec.Deletion = oacp.Spec.MachineTemplate.Deletion
	desiredMachine.Spec.FailureDomain = failureDomain

	// Note: by setting the ownerRef on creation we signal to the Machine controller that this is not a stand-alone Machine.
	_ = controllerutil.SetOwnerReference(oacp, desiredMachine, r.Scheme)

	// Set the in-place mutable fields.
	// When we create a new Machine we will just create the Machine with those fields.
	// When we update an existing Machine will we update the fields on the existing Machine (in-place mutate).

	desiredMachine.Labels = util.ControlPlaneMachineLabelsForCluster(oacp, cluster.Name)

	// We intentionally don't use the map directly to ensure we don't modify the map in OACP.
	for k, v := range oacp.Spec.MachineTemplate.ObjectMeta.Annotations {
		desiredMachine.Annotations[k] = v
	}
	for k, v := range annotations {
		desiredMachine.Annotations[k] = v
	}

	return desiredMachine
}

// SetupWithManager sets up the controller with the Manager.
func (r *OpenshiftAssistedControlPlaneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// No longer need field indexer since we're using labels
	return ctrl.NewControllerManagedBy(mgr).
		For(&controlplanev1alpha3.OpenshiftAssistedControlPlane{}).
		Watches(
			&clusterv1.Machine{},
			handler.EnqueueRequestForOwner(r.Scheme, mgr.GetRESTMapper(), &controlplanev1alpha3.OpenshiftAssistedControlPlane{}),
		).
		Complete(r)
}

func (r *OpenshiftAssistedControlPlaneReconciler) ensureClusterDeployment(
	ctx context.Context,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	clusterName string,
) error {
	cd := &hivev1.ClusterDeployment{}
	err := r.Get(ctx, client.ObjectKey{Namespace: oacp.Namespace, Name: oacp.Name}, cd)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	if err == nil {
		return nil
	}

	if oacp.Spec.Config.ClusterName != "" {
		clusterName = oacp.Spec.Config.ClusterName
	}

	cd.Name = oacp.Name
	cd.Namespace = oacp.Namespace
	_, err = controllerutil.CreateOrPatch(ctx, r.Client, cd, func() error {
		_ = controllerutil.SetOwnerReference(oacp, cd, r.Scheme)
		cd.Labels = util.ControlPlaneMachineLabelsForCluster(oacp, clusterName)

		cd.Spec.ClusterName = clusterName
		cd.Spec.ClusterInstallRef = &hivev1.ClusterInstallLocalReference{
			Group:   hiveext.Group,
			Version: hiveext.Version,
			Kind:    "AgentClusterInstall",
			Name:    oacp.Name,
		}
		cd.Spec.BaseDomain = oacp.Spec.Config.BaseDomain
		cd.Spec.Platform = hivev1.Platform{
			AgentBareMetal: &agent.BareMetalPlatform{},
		}
		cd.Spec.PullSecretRef = oacp.Spec.Config.PullSecretRef

		return nil
	})

	return err
}

func (r *OpenshiftAssistedControlPlaneReconciler) reconcileReplicas(ctx context.Context, oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane, cluster *clusterv1.Cluster) error {
	log := ctrl.LoggerFrom(ctx)
	machines, err := collections.GetFilteredMachinesForCluster(ctx, r.Client, cluster, collections.OwnedMachines(oacp))
	if err != nil {
		return err
	}

	numMachines := machines.Len()
	desiredReplicas := int(oacp.Spec.Replicas)
	machinesToCreate := desiredReplicas - numMachines
	var errs []error
	if machinesToCreate > 0 {
		fd, err := failuredomains.NextFailureDomainForScaleUp(ctx, cluster, machines)
		if err != nil {
			return fmt.Errorf("failed to find failure domain for scale up: %v", err)
		}
		machine, err := r.scaleUpControlPlane(ctx, oacp, cluster, fd)
		if err != nil {
			return fmt.Errorf("failed to scale up control plane: %v", err)
		}
		log.V(logutil.InfoLevel).Info("creating controlplane machine", "machine name", machine.Name)
	}
	if machinesToCreate < 0 {
		fd, err := failuredomains.NextFailureDomainForScaleDown(ctx, cluster, machines)
		if err != nil {
			return fmt.Errorf("failed to find failure domain for scale down: %v", err)
		}
		machine, err := r.scaleDownControlPlane(ctx, machines, fd)
		if err != nil {
			return fmt.Errorf("failed to scale down control plane: %v", err)
		}
		log.V(logutil.InfoLevel).Info("creating controlplane machine", "machine name", machine.Name)
	}

	log.V(logutil.DebugLevel).Info("updating replica status", "oacp", oacp, "machines", machines)

	r.updateReplicaStatus(ctx, oacp, machines)
	return kerrors.NewAggregate(errs)
}

func (r *OpenshiftAssistedControlPlaneReconciler) scaleUpControlPlane(ctx context.Context, oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane, cluster *clusterv1.Cluster, failureDomain string) (*clusterv1.Machine, error) {
	name := names.SimpleNameGenerator.GenerateName(oacp.Name + "-")
	machine, err := r.generateMachine(ctx, oacp, name, cluster, failureDomain)
	if err != nil {
		return nil, err
	}
	bootstrapConfig := r.generateOpenshiftAssistedConfig(oacp, cluster.Name, name)
	_ = controllerutil.SetOwnerReference(oacp, bootstrapConfig, r.Scheme)
	if err := r.Create(ctx, bootstrapConfig); err != nil {
		setConditionFalse(oacp, controlplanev1alpha3.MachinesCreatedCondition, controlplanev1alpha3.BootstrapTemplateCloningFailedReason,
			"error creating bootstrap config: %v", err)
		return nil, err
	}
	machine.Spec.Bootstrap.ConfigRef = clusterv1.ContractVersionedObjectReference{
		Kind:     "OpenshiftAssistedConfig",
		Name:     bootstrapConfig.Name,
		APIGroup: bootstrapv1alpha2.GroupVersion.Group,
	}
	if err := r.Create(ctx, machine); err != nil {
		setConditionFalse(oacp, controlplanev1alpha3.MachinesCreatedCondition,
			controlplanev1alpha3.MachineGenerationFailedReason, "error creating machine %v", err)
		if deleteBootstrapErr := r.Delete(ctx, bootstrapConfig); deleteBootstrapErr != nil {
			err = errors.Join(err, deleteBootstrapErr)
		}
		infraRefKey := &corev1.ObjectReference{Name: machine.Spec.InfrastructureRef.Name, Namespace: machine.Namespace}
		if deleteInfraRefErr := external.Delete(ctx, r.Client, infraRefKey); deleteInfraRefErr != nil {
			err = errors.Join(err, deleteInfraRefErr)
		}
		return nil, err
	}
	return machine, nil
}

func (r *OpenshiftAssistedControlPlaneReconciler) isMachineUpToDate(ctx context.Context, machine *clusterv1.Machine, oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane) bool {
	log := ctrl.LoggerFrom(ctx)

	if !equality.Semantic.DeepEqual(machine.Spec.Deletion, oacp.Spec.MachineTemplate.Deletion) {
		log.V(logutil.DebugLevel).Info("Machine not up-to-date: Deletion spec mismatch",
			"machine", machine.Name,
			"machineDeletion", machine.Spec.Deletion,
			"oacpDeletion", oacp.Spec.MachineTemplate.Deletion)
		return false
	}

	if machine.Spec.Bootstrap.ConfigRef.Name == "" {
		log.V(logutil.DebugLevel).Info("Machine not up-to-date: Bootstrap ConfigRef is empty", "machine", machine.Name)
		return false
	}

	expectedBootstrapConfigSpec := oacp.Spec.OpenshiftAssistedConfigSpec
	bootstrapConfig := &bootstrapv1alpha2.OpenshiftAssistedConfig{}
	if err := r.Get(ctx, types.NamespacedName{Name: machine.Spec.Bootstrap.ConfigRef.Name, Namespace: machine.Namespace}, bootstrapConfig); err != nil {
		log.V(logutil.DebugLevel).Info("Machine not up-to-date: Failed to get bootstrap config",
			"machine", machine.Name,
			"configRef", machine.Spec.Bootstrap.ConfigRef.Name,
			"error", err)
		return false
	}

	if !equality.Semantic.DeepDerivative(expectedBootstrapConfigSpec, bootstrapConfig.Spec) {
		log.V(logutil.DebugLevel).Info("Machine not up-to-date: Bootstrap config spec mismatch",
			"machine", machine.Name,
			"expectedCpuArch", expectedBootstrapConfigSpec.CpuArchitecture,
			"actualCpuArch", bootstrapConfig.Spec.CpuArchitecture,
			"expectedProxy", expectedBootstrapConfigSpec.Proxy,
			"actualProxy", bootstrapConfig.Spec.Proxy,
			"expectedSSHKey", expectedBootstrapConfigSpec.SSHAuthorizedKey,
			"actualSSHKey", bootstrapConfig.Spec.SSHAuthorizedKey,
			"expectedPullSecretRef", expectedBootstrapConfigSpec.PullSecretRef,
			"actualPullSecretRef", bootstrapConfig.Spec.PullSecretRef,
			"expectedNMStateSelector", expectedBootstrapConfigSpec.NMStateConfigLabelSelector,
			"actualNMStateSelector", bootstrapConfig.Spec.NMStateConfigLabelSelector)
		return false
	}

	log.V(logutil.DebugLevel).Info("Machine is up-to-date", "machine", machine.Name)
	return true
}

func (r *OpenshiftAssistedControlPlaneReconciler) updateReplicaStatus(ctx context.Context, oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane, machines collections.Machines) {
	log := ctrl.LoggerFrom(ctx)

	desiredReplicas := oacp.Spec.Replicas
	var readyReplicas, availableReplicas, upToDateReplicas int32
	for _, machine := range machines {
		if conditions.IsTrue(machine, clusterv1.MachineReadyCondition) {
			readyReplicas++
		}
		if conditions.IsTrue(machine, clusterv1.MachineAvailableCondition) {
			availableReplicas++
		}

		// Check if machine is up-to-date and set the condition on the machine
		isUpToDate := r.isMachineUpToDate(ctx, machine, oacp)
		if isUpToDate {
			upToDateReplicas++
		}

		// Set the UpToDate condition on the machine (as the owner, we're responsible for this)
		if err := r.setMachineUpToDateCondition(ctx, machine, isUpToDate); err != nil {
			log.Error(err, "failed to set UpToDate condition on machine", "machine", machine.Name)
		}
	}
	replicas := int32(machines.Len())

	// Set new status fields (conversions handle mapping to v1alpha2)
	oacp.Status.UpToDateReplicas = &upToDateReplicas
	oacp.Status.Replicas = &replicas
	oacp.Status.AvailableReplicas = &availableReplicas
	oacp.Status.ReadyReplicas = &readyReplicas

	if *(oacp.Status.ReadyReplicas) == desiredReplicas {
		setConditionTrue(oacp, controlplanev1alpha3.MachinesCreatedCondition)
	}

	// Set MachinesReady condition based on machine readiness
	if readyReplicas == desiredReplicas && desiredReplicas > 0 {
		conditions.Set(oacp, metav1.Condition{
			Type:   clusterv1.MachinesReadyCondition,
			Status: metav1.ConditionTrue,
			Reason: "MachinesReady",
		})
		return
	}
	if desiredReplicas > 0 {
		conditions.Set(oacp, metav1.Condition{
			Type:    clusterv1.MachinesReadyCondition,
			Status:  metav1.ConditionFalse,
			Reason:  "MachinesNotReady",
			Message: fmt.Sprintf("%d of %d machines are ready", readyReplicas, desiredReplicas),
		})
	}
}

func (r *OpenshiftAssistedControlPlaneReconciler) generateMachine(ctx context.Context, oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane, name string, cluster *clusterv1.Cluster, failureDomain string) (*clusterv1.Machine, error) {
	// Compute desired Machine
	machine := r.computeDesiredMachine(oacp, name, cluster, failureDomain)
	infraRef, err := r.computeInfraRef(ctx, oacp, machine.Name, cluster.Name)
	if err != nil {
		return nil, err
	}
	machine.Spec.InfrastructureRef = infraRef
	return machine, nil
}

func (r *OpenshiftAssistedControlPlaneReconciler) computeInfraRef(ctx context.Context, oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane, machineName, clusterName string) (clusterv1.ContractVersionedObjectReference, error) {
	// Since the cloned resource should eventually have a controller ref for the Machine, we create an
	// OwnerReference here without the Controller field set
	infraCloneOwner := &metav1.OwnerReference{
		APIVersion: controlplanev1alpha3.GroupVersion.String(),
		Kind:       openshiftAssistedControlPlaneKind,
		Name:       oacp.Name,
		UID:        oacp.UID,
	}

	// Convert ContractVersionedObjectReference to corev1.ObjectReference for external.CreateFromTemplate
	// The external package expects corev1.ObjectReference with APIVersion
	templateRef := contractVersionedRefToObjectRef(&oacp.Spec.MachineTemplate.InfrastructureRef, oacp.Namespace)

	// Clone the infrastructure template
	_, infraRef, err := external.CreateFromTemplate(ctx, &external.CreateFromTemplateInput{
		Client:      r.Client,
		TemplateRef: templateRef,
		Namespace:   oacp.Namespace,
		Name:        machineName,
		OwnerRef:    infraCloneOwner,
		ClusterName: clusterName,
		Labels:      util.ControlPlaneMachineLabelsForCluster(oacp, clusterName),
		Annotations: oacp.Spec.MachineTemplate.ObjectMeta.Annotations,
	})
	if err != nil {
		// Safe to return early here since no resources have been created yet.
		setConditionFalse(oacp, controlplanev1alpha3.MachinesCreatedCondition, controlplanev1alpha3.InfrastructureTemplateCloningFailedReason,
			"error creating infraenv: %v", err)
		return clusterv1.ContractVersionedObjectReference{}, err
	}
	return infraRef, nil
}

func (r *OpenshiftAssistedControlPlaneReconciler) generateOpenshiftAssistedConfig(oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane, clusterName string, name string) *bootstrapv1alpha2.OpenshiftAssistedConfig {
	labels := util.ControlPlaneMachineLabelsForCluster(oacp, clusterName)

	// Merge in labels from the OpenshiftAssistedControlPlane itself
	// This allows users to set labels on the control plane that will be propagated to the configs
	for k, v := range oacp.Labels {
		if _, exists := labels[k]; !exists {
			labels[k] = v
		}
	}

	annotations := make(map[string]string)
	for k, v := range oacp.Spec.MachineTemplate.ObjectMeta.Annotations {
		annotations[k] = v
	}

	// Merge in annotations from the OpenshiftAssistedControlPlane itself
	// This allows propagation of discovery-ignition-override and other annotations
	for k, v := range oacp.Annotations {
		if _, exists := annotations[k]; !exists {
			annotations[k] = v
		}
	}

	bootstrapConfig := &bootstrapv1alpha2.OpenshiftAssistedConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   oacp.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: *oacp.Spec.OpenshiftAssistedConfigSpec.DeepCopy(),
	}

	_ = controllerutil.SetOwnerReference(oacp, bootstrapConfig, r.Scheme)
	return bootstrapConfig
}

func (r *OpenshiftAssistedControlPlaneReconciler) ensurePullSecret(
	ctx context.Context,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
) error {
	if oacp.Spec.Config.PullSecretRef != nil {
		return nil
	}

	secret := assistedinstaller.GenerateFakePullSecret("", oacp.Namespace)
	if err := controllerutil.SetOwnerReference(oacp, secret, r.Scheme); err != nil {
		return err
	}

	if err := r.Create(ctx, secret); err != nil {
		return err
	}
	oacp.Spec.Config.PullSecretRef = &corev1.LocalObjectReference{Name: secret.Name}
	return nil
}

func (r *OpenshiftAssistedControlPlaneReconciler) scaleDownControlPlane(ctx context.Context, eligibleMachines collections.Machines, failureDomain string) (*clusterv1.Machine, error) {
	machineToDelete, err := selectMachineForScaleDown(eligibleMachines, failureDomain)
	if err != nil {
		return nil, fmt.Errorf("failed to select machine for scale down: %v", err)
	}
	if machineToDelete == nil {
		return nil, errors.New("failed to select machine for scale down: no machine found")
	}
	if err := r.Delete(ctx, machineToDelete); err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}
	return machineToDelete, nil
}

// Selects machines for scale down. Give priority to machines with the delete annotation.
func selectMachineForScaleDown(eligibleMachines collections.Machines, failureDomain string) (*clusterv1.Machine, error) {
	machinesInFailureDomain := eligibleMachines.Filter(collections.InFailureDomains(failureDomain))
	machineToScaleDown := machinesInFailureDomain.Oldest()
	if machineToScaleDown == nil {
		return nil, errors.New("failed to pick control plane Machine to scale down")
	}
	return machineToScaleDown, nil
}

// contractVersionedRefToObjectRef converts ContractVersionedObjectReference (with APIGroup)
// to corev1.ObjectReference (with APIVersion) for use with external.CreateFromTemplate.
// Since ContractVersionedObjectReference only has APIGroup, we use the contract version convention
// and default to v1beta1 for infrastructure providers.
func contractVersionedRefToObjectRef(in *clusterv1.ContractVersionedObjectReference, namespace string) *corev1.ObjectReference {
	apiVersion := ""
	if in.APIGroup != "" {
		// Default to v1beta1 for infrastructure group, as that's the common convention
		apiVersion = in.APIGroup + "/v1beta1"
	}
	return &corev1.ObjectReference{
		Kind:       in.Kind,
		Name:       in.Name,
		Namespace:  namespace,
		APIVersion: apiVersion,
	}
}

// Condition helper functions for setting metav1.Condition (new format).

// setConditionTrue sets a condition to True.
func setConditionTrue(oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane, conditionType clusterv1.ConditionType) {
	conditions.Set(oacp, metav1.Condition{
		Type:   string(conditionType),
		Status: metav1.ConditionTrue,
		Reason: string(conditionType),
	})
}

// setConditionFalse sets a condition to False with reason and message.
func setConditionFalse(oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane, conditionType clusterv1.ConditionType, reason, messageFormat string, messageArgs ...interface{}) {
	conditions.Set(oacp, metav1.Condition{
		Type:    string(conditionType),
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: fmt.Sprintf(messageFormat, messageArgs...),
	})
}

// setMachineUpToDateCondition sets the UpToDate condition on a Machine.
// As the owner of the machine, the control plane provider is responsible for setting this condition.
func (r *OpenshiftAssistedControlPlaneReconciler) setMachineUpToDateCondition(ctx context.Context, machine *clusterv1.Machine, isUpToDate bool) error {
	// Check if condition already has the correct value to avoid unnecessary patches
	currentCondition := conditions.Get(machine, clusterv1.MachineUpToDateCondition)
	expectedStatus := metav1.ConditionTrue
	expectedReason := clusterv1.MachineUpToDateReason
	if !isUpToDate {
		expectedStatus = metav1.ConditionFalse
		expectedReason = clusterv1.MachineNotUpToDateReason
	}

	// Skip if condition already has the expected value
	if currentCondition != nil &&
		currentCondition.Status == expectedStatus &&
		currentCondition.Reason == expectedReason {
		return nil
	}

	patchHelper, err := patch.NewHelper(machine, r.Client)
	if err != nil {
		return err
	}

	conditions.Set(machine, metav1.Condition{
		Type:   clusterv1.MachineUpToDateCondition,
		Status: expectedStatus,
		Reason: expectedReason,
	})

	return patchHelper.Patch(ctx, machine)
}
