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
	"time"

	"github.com/openshift-assisted/cluster-api-agent/assistedinstaller"
	"k8s.io/client-go/tools/reference"

	controlplanev1alpha1 "github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1alpha1"
	"github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aimodels "github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"

	util "github.com/openshift-assisted/cluster-api-agent/util"
	logutil "github.com/openshift-assisted/cluster-api-agent/util/log"

	hivev1 "github.com/openshift/hive/apis/hive/v1"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metal3 "github.com/metal3-io/cluster-api-provider-metal3/api/v1beta1"
	bootstrapv1alpha1 "github.com/openshift-assisted/cluster-api-agent/bootstrap/api/v1alpha1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	bsutil "sigs.k8s.io/cluster-api/bootstrap/util"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

var liveIsoFormat string = "live-iso"

const (
	openshiftAssistedConfigFinalizer = "openshiftassistedconfig." + bootstrapv1alpha1.Group + "/deprovision"
)

// OpenshiftAssistedConfigReconciler reconciles a OpenshiftAssistedConfig object
type OpenshiftAssistedConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=metal3machinetemplates,verbs=list;patch;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=metal3machines,verbs=list;patch;watch
// +kubebuilder:rbac:groups=metal3.io,resources=baremetalhosts,verbs=list;watch
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=openshiftassistedconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=openshiftassistedconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=openshiftassistedconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;watch;
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=infraenvs,verbs=delete;list;watch;get;update;create
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;create;list;watch
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents,verbs=delete;list;watch;get;update
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=metal3machines;metal3machinetemplates,verbs=get;update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;create
// +kubebuilder:rbac:groups="",resources=services,verbs=list;get;watch
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments,verbs=list;watch
// +kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=agentclusterinstalls;agentclusterinstalls/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinesets;machinesets/status,verbs=get;list;watch;
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=openshiftassistedcontrolplanes,verbs=get;list;watch

// Reconciles OpenshiftAssistedConfig
func (r *OpenshiftAssistedConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, rerr error) {
	log := ctrl.LoggerFrom(ctx)

	log.V(logutil.TraceLevel).Info("Reconciling OpenshiftAssistedConfig")

	config := &bootstrapv1alpha1.OpenshiftAssistedConfig{}
	if err := r.Client.Get(ctx, req.NamespacedName, config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	log.WithValues("agent_bootstrap_config", config.Name, "agent_bootstrap_config_namespace", config.Namespace)

	// Initialize the patch helper.
	patchHelper, err := patch.NewHelper(config, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Attempt to Patch the OpenshiftAssistedConfig object and status after each reconciliation if no error occurs.
	defer func() {
		// always update the readyCondition; the summary is represented using the "1 of x completed" notation.
		conditions.SetSummary(config,
			conditions.WithConditions(
				bootstrapv1alpha1.DataSecretAvailableCondition,
			),
		)

		// Patch ObservedGeneration only if the reconciliation completed successfully
		patchOpts := []patch.Option{}
		if rerr == nil {
			patchOpts = append(patchOpts, patch.WithStatusObservedGeneration{})
		}
		if err := patchHelper.Patch(ctx, config, patchOpts...); err != nil {
			rerr = kerrors.NewAggregate([]error{rerr, err})
		}
		log.V(logutil.TraceLevel).Info("Finished reconciling OpenshiftAssistedConfig")
	}()

	// Look up the owner of this openshiftassistedconfig if there is one
	configOwner, err := bsutil.GetTypedConfigOwner(ctx, r.Client, config)
	if apierrors.IsNotFound(err) {
		// Could not find the owner yet, this is not an error and will re-reconcile when the owner gets set.
		log.V(logutil.InfoLevel).Info("config owner not found", "name", configOwner.GetName())
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "failed to get owner")
	}
	if configOwner == nil {
		return ctrl.Result{}, nil
	}

	log.V(logutil.TraceLevel).Info("config owner found", "name", configOwner.GetName())

	if !config.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.handleDeletion(ctx, config, configOwner)
	}

	if !controllerutil.ContainsFinalizer(config, openshiftAssistedConfigFinalizer) {
		controllerutil.AddFinalizer(config, openshiftAssistedConfigFinalizer)
	}

	cluster, err := capiutil.GetClusterByName(ctx, r.Client, configOwner.GetNamespace(), configOwner.ClusterName())
	if err != nil {
		if errors.Cause(err) == capiutil.ErrNoCluster {
			log.V(logutil.TraceLevel).
				Info(fmt.Sprintf("%s does not belong to a cluster yet, waiting until it's part of a cluster", configOwner.GetKind()))
			return ctrl.Result{}, nil
		}

		if apierrors.IsNotFound(err) {
			log.V(logutil.TraceLevel).Info("Cluster does not exist yet, waiting until it is created")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Could not get cluster with metadata")
		return ctrl.Result{}, err
	}

	if !cluster.Status.InfrastructureReady {
		log.V(logutil.TraceLevel).Info("Cluster infrastructure is not read, waiting")
		conditions.MarkFalse(
			config,
			bootstrapv1alpha1.DataSecretAvailableCondition,
			bootstrapv1alpha1.WaitingForClusterInfrastructureReason,
			clusterv1.ConditionSeverityInfo,
			"",
		)

		return ctrl.Result{Requeue: true, RequeueAfter: retryAfter}, nil
	}
	// Get the Machine that owns this openshiftassistedconfig
	machine, err := capiutil.GetOwnerMachine(ctx, r.Client, config.ObjectMeta)
	if err != nil {
		log.Error(err, "couldn't get machine associated with openshiftassistedconfig", "name", config.Name)
		return ctrl.Result{}, err
	}

	clusterDeployment, err := r.getClusterDeployment(ctx, cluster.GetName())
	if err != nil {
		log.V(logutil.InfoLevel).Info("could not retrieve ClusterDeployment... requeuing", "cluster", cluster.GetName())
		conditions.MarkFalse(
			config,
			bootstrapv1alpha1.DataSecretAvailableCondition,
			bootstrapv1alpha1.WaitingForAssistedInstallerReason,
			clusterv1.ConditionSeverityInfo,
			"",
		)
		return ctrl.Result{Requeue: true}, nil
	}

	aci, err := r.getAgentClusterInstall(ctx, clusterDeployment)
	if err != nil {
		log.V(logutil.InfoLevel).Info("could not retrieve AgentClusterInstall... requeuing")
		conditions.MarkFalse(
			config,
			bootstrapv1alpha1.DataSecretAvailableCondition,
			bootstrapv1alpha1.WaitingForAssistedInstallerReason,
			clusterv1.ConditionSeverityInfo,
			"",
		)
		return ctrl.Result{Requeue: true}, nil
	}

	// if added worker after start install, will be treated as day2
	if !capiutil.IsControlPlaneMachine(machine) &&
		!(aci.Status.DebugInfo.State == aimodels.ClusterStatusAddingHosts || aci.Status.DebugInfo.State == aimodels.ClusterStatusPendingForInput || aci.Status.DebugInfo.State == aimodels.ClusterStatusInsufficient || aci.Status.DebugInfo.State == "") {
		log.V(logutil.DebugLevel).Info("not controlplane machine and installation already started, requeuing")
		conditions.MarkFalse(
			config,
			bootstrapv1alpha1.DataSecretAvailableCondition,
			bootstrapv1alpha1.WaitingForInstallCompleteReason,
			clusterv1.ConditionSeverityInfo,
			"",
		)
		return ctrl.Result{Requeue: true, RequeueAfter: 60 * time.Second}, nil
	}

	if err := r.ensureInfraEnv(ctx, config, clusterDeployment); err != nil {
		conditions.MarkFalse(
			config,
			bootstrapv1alpha1.DataSecretAvailableCondition,
			bootstrapv1alpha1.InfraEnvFailedReason,
			clusterv1.ConditionSeverityWarning,
			"",
		)
		return ctrl.Result{}, err
	}

	if config.Status.ISODownloadURL == "" {
		conditions.MarkFalse(
			config,
			bootstrapv1alpha1.DataSecretAvailableCondition,
			bootstrapv1alpha1.WaitingForLiveISOURLReason,
			clusterv1.ConditionSeverityInfo,
			"",
		)
		return ctrl.Result{}, nil
	}

	if err := r.setMetal3MachineTemplateImage(ctx, config, machine); err != nil {
		conditions.MarkFalse(
			config,
			bootstrapv1alpha1.DataSecretAvailableCondition,
			bootstrapv1alpha1.PropagatingLiveISOURLFailedReason,
			clusterv1.ConditionSeverityWarning,
			"",
		)
		return ctrl.Result{}, err
	}

	// if a metal3 machine booted before the template was updated, then we need to update it
	if err := r.setMetal3MachineImage(ctx, config, machine); err != nil {
		conditions.MarkFalse(
			config,
			bootstrapv1alpha1.DataSecretAvailableCondition,
			bootstrapv1alpha1.PropagatingLiveISOURLFailedReason,
			clusterv1.ConditionSeverityWarning,
			"",
		)
		return ctrl.Result{}, err
	}

	secret, err := r.createUserDataSecret(ctx, config)
	if err != nil {
		log.Error(err, "couldn't create user data secret", "name", config.Name)
		conditions.MarkFalse(
			config,
			bootstrapv1alpha1.DataSecretAvailableCondition,
			bootstrapv1alpha1.CreatingSecretFailedReason,
			clusterv1.ConditionSeverityWarning,
			"",
		)
		return ctrl.Result{}, err
	}

	config.Status.Ready = true
	config.Status.DataSecretName = &secret.Name
	conditions.MarkTrue(config, bootstrapv1alpha1.DataSecretAvailableCondition)
	return ctrl.Result{}, rerr
}

// Ensures InfraEnv exists
func (r *OpenshiftAssistedConfigReconciler) ensureInfraEnv(
	ctx context.Context,
	config *bootstrapv1alpha1.OpenshiftAssistedConfig,
	clusterDeployment *hivev1.ClusterDeployment,
) error {
	log := ctrl.LoggerFrom(ctx)
	infraEnvName, err := getInfraEnvName(config)
	log.WithValues(
		"OpenshiftAssistedConfig Name",
		config.Name,
		"OpenshiftAssistedConfig Namespace",
		config.Namespace,
		"InfraEnv Name",
		infraEnvName,
	)

	if err != nil {
		log.Error(err, "couldn't get infraenv name for openshiftassistedconfig")
		return err
	}
	log.V(logutil.DebugLevel).Info("computed infraEnvName", "name", infraEnvName)

	if infraEnvName == "" {
		log.V(logutil.DebugLevel).Info("no infraenv name for openshiftassistedconfig")
		return fmt.Errorf("no infraenv name for openshiftassistedconfig")
	}

	infraEnv := assistedinstaller.GetInfraEnvFromConfig(infraEnvName, config, clusterDeployment)
	_ = controllerutil.SetOwnerReference(config, infraEnv, r.Scheme)
	err = r.Create(ctx, infraEnv)
	if err == nil {
		log.V(logutil.DebugLevel).Info("Created infra env", "name", infraEnv.Name, "namespace", infraEnv.Namespace)
	}
	if err != nil && !apierrors.IsAlreadyExists(err) {
		log.V(logutil.DebugLevel).Error(err, "infra env error", "name", infraEnv.Name, "namespace", infraEnv.Namespace)
		// something went wrong, let's not exist because we might be able to read it and reference it in the status
	}

	// Set infraEnv if not already set
	if config.Status.InfraEnvRef == nil {
		ref, err := reference.GetReference(r.Scheme, infraEnv)
		if err != nil {
			return err
		}
		config.Status.InfraEnvRef = ref
	}
	return nil
}

// Retrieve AgentClusterInstall by ClusterDeployment.Spec.ClusterInstallRef
func (r *OpenshiftAssistedConfigReconciler) getAgentClusterInstall(
	ctx context.Context,
	clusterDeployment *hivev1.ClusterDeployment,
) (*v1beta1.AgentClusterInstall, error) {
	if clusterDeployment.Spec.ClusterInstallRef == nil {
		return nil, fmt.Errorf("cluster deployment does not reference ACI")
	}
	objKey := types.NamespacedName{
		Namespace: clusterDeployment.Namespace,
		Name:      clusterDeployment.Spec.ClusterInstallRef.Name,
	}
	aci := v1beta1.AgentClusterInstall{}
	if err := r.Client.Get(ctx, objKey, &aci); err != nil {
		return nil, err
	}
	return &aci, nil
}

// Retrieve ClusterDeployment by cluster name label
func (r *OpenshiftAssistedConfigReconciler) getClusterDeployment(
	ctx context.Context,
	clusterName string,
) (*hivev1.ClusterDeployment, error) {
	clusterDeployments := hivev1.ClusterDeploymentList{}
	if err := r.Client.List(ctx, &clusterDeployments, client.MatchingLabels{clusterv1.ClusterNameLabel: clusterName}); err != nil {
		return nil, err
	}
	if len(clusterDeployments.Items) != 1 {
		return nil, fmt.Errorf("found more or less than 1 cluster deployments. exactly one is needed")
	}

	clusterDeployment := clusterDeployments.Items[0]
	return &clusterDeployment, nil
}

// Creates UserData secret
func (r *OpenshiftAssistedConfigReconciler) createUserDataSecret(
	ctx context.Context,
	config *bootstrapv1alpha1.OpenshiftAssistedConfig,
) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: config.Namespace, Name: config.Name}, secret); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, err
		}
		secret.Name = config.Name
		secret.Namespace = config.Namespace
		if err := controllerutil.SetOwnerReference(config, secret, r.Scheme); err != nil {
			return nil, err
		}

		if err := r.Client.Create(ctx, secret); err != nil {
			return nil, err
		}
	}
	return secret, nil
}

// Overrides image reference by setting the LiveISO present on the OpenshiftAssistedConfig.Status.ISODownloadURL
func (r *OpenshiftAssistedConfigReconciler) setMetal3MachineImage(
	ctx context.Context,
	config *bootstrapv1alpha1.OpenshiftAssistedConfig,
	machine *clusterv1.Machine,
) error {
	log := ctrl.LoggerFrom(ctx)
	m3MachineKey := types.NamespacedName{
		Name:      machine.Spec.InfrastructureRef.Name,
		Namespace: machine.Spec.InfrastructureRef.Namespace,
	}
	log = log.WithValues(
		"OpenshiftAssistedConfig Name",
		config.Name,
		"OpenshiftAssistedConfig Namespace",
		config.Namespace,
		"Metal3Machine Name",
		m3MachineKey.Name,
		"Metal3Machine Namespace",
		m3MachineKey.Namespace,
	)

	metal3Machine := &metal3.Metal3Machine{}
	if err := r.Client.Get(ctx, m3MachineKey, metal3Machine); err != nil {
		log.Error(err, "couldn't get metal3machine associated with machine and openshiftassistedconfig")
		// no machine, no need to inject live iso
		if !apierrors.IsNotFound(err) {
			return err
		}
		return nil
	}

	log.V(logutil.TraceLevel).Info("Found metal3 machine owned by machine")
	if metal3Machine.Spec.Image.URL == "" || metal3Machine.Spec.Image.URL != config.Status.ISODownloadURL {
		log.WithValues("ISO", config.Status.ISODownloadURL)
		log.V(logutil.DebugLevel).Info("adding ISO URL to metal3 machine")

		patchHelper, err := patch.NewHelper(metal3Machine, r.Client)
		if err != nil {
			log.Error(err, "couldn't create patch helper for metal3machine")
			return err
		}

		metal3Machine.Spec.Image.URL = config.Status.ISODownloadURL
		metal3Machine.Spec.Image.DiskFormat = &liveIsoFormat

		if err := patchHelper.Patch(ctx, metal3Machine); err != nil {
			log.Error(err, "couldn't update metal3 machine")
			return err
		}
		log.V(logutil.TraceLevel).
			Info("Added ISO URLs to metal3 machines", "machine", metal3Machine.Name, "namespace", metal3Machine.Namespace)
	}
	return nil
}

// Retrieves InfrastructureRefKey
func (r *OpenshiftAssistedConfigReconciler) getInfrastructureRefKey(
	ctx context.Context,
	machine *clusterv1.Machine,
) (types.NamespacedName, error) {
	acp := controlplanev1alpha1.OpenshiftAssistedControlPlane{}
	namespace := machine.Namespace
	err := util.GetTypedOwner(ctx, r.Client, machine, &acp)
	if err != nil {
		// Machine is not owned by ACP, check for MS
		ms := clusterv1.MachineSet{}
		if err := util.GetTypedOwner(ctx, r.Client, machine, &ms); err != nil {
			return types.NamespacedName{}, fmt.Errorf("machine has neither acp nor md owner")
		}
		if ms.Spec.Template.Spec.InfrastructureRef.Namespace != "" {
			namespace = ms.Spec.Template.Spec.InfrastructureRef.Namespace
		}
		return types.NamespacedName{
			Namespace: namespace,
			Name:      ms.Spec.Template.Spec.InfrastructureRef.Name,
		}, nil
	}
	if acp.Spec.MachineTemplate.InfrastructureRef.Namespace != "" {
		namespace = acp.Spec.MachineTemplate.InfrastructureRef.Namespace
	}
	return types.NamespacedName{
		Namespace: namespace,
		Name:      acp.Spec.MachineTemplate.InfrastructureRef.Name,
	}, nil
}

// Overrides image reference by setting the LiveISO present on the OpenshiftAssistedConfig.Status.ISODownloadURL
func (r *OpenshiftAssistedConfigReconciler) setMetal3MachineTemplateImage(
	ctx context.Context,
	config *bootstrapv1alpha1.OpenshiftAssistedConfig,
	machine *clusterv1.Machine,
) error {
	log := ctrl.LoggerFrom(ctx)
	tplKey, err := r.getInfrastructureRefKey(ctx, machine)
	log = log.WithValues("Metal3MachineTemplate Name", tplKey.Name, "Metal3MachineTemplate Namespace", tplKey.Namespace)

	if err != nil {
		return err
	}
	machineTpl := &metal3.Metal3MachineTemplate{}

	if err := r.Client.Get(ctx, tplKey, machineTpl); err != nil {
		log.Error(err, "couldn't find machine template")
		return err
	}

	if machineTpl.Spec.Template.Spec.Image.URL != config.Status.ISODownloadURL ||
		machineTpl.Spec.Template.Spec.Image.DiskFormat != &liveIsoFormat {
		patchHelper, err := patch.NewHelper(machineTpl, r.Client)
		if err != nil {
			log.Error(err, "couldn't create patch helper for machineTpl")
			return err
		}
		machineTpl.Spec.Template.Spec.Image.URL = config.Status.ISODownloadURL
		machineTpl.Spec.Template.Spec.Image.DiskFormat = &liveIsoFormat

		if err := patchHelper.Patch(ctx, machineTpl); err != nil {
			log.Error(err, "couldn't update machine template")
			return err
		}
	}
	return nil
}

// Deletes child resources (Agent) and removes finalizer
func (r *OpenshiftAssistedConfigReconciler) handleDeletion(ctx context.Context, config *bootstrapv1alpha1.OpenshiftAssistedConfig, owner *bsutil.ConfigOwner) error {
	log := ctrl.LoggerFrom(ctx)
	log.WithValues("name", config.Name, "namespace", config.Namespace)
	if controllerutil.ContainsFinalizer(config, openshiftAssistedConfigFinalizer) {
		// Check if it's a control plane node and if that cluster is being deleted
		if _, isControlPlane := config.Labels[clusterv1.MachineControlPlaneLabel]; isControlPlane &&
			owner.GetDeletionTimestamp().IsZero() {
			// Don't remove finalizer if the controlplane is not being deleted
			err := fmt.Errorf("agent bootstrap config belongs to control plane that's not being deleted")
			log.Error(err, "unable to delete agent bootstrap config")
			return err
		}

		// Delete associated agent
		if config.Status.AgentRef != nil {
			if err := r.Client.Delete(ctx, &aiv1beta1.Agent{ObjectMeta: metav1.ObjectMeta{Name: config.Status.AgentRef.Name, Namespace: config.Namespace}}); err != nil &&
				!apierrors.IsNotFound(err) {
				log.Error(err, "failed to delete agent associated with this agent bootstrap config")
				return err
			}
			config.Status.AgentRef = nil
		}
		controllerutil.RemoveFinalizer(config, openshiftAssistedConfigFinalizer)
	}
	return nil
}

// Generate InfraEnvName
func getInfraEnvName(config *bootstrapv1alpha1.OpenshiftAssistedConfig) (string, error) {
	// this should be based on Infra template instead
	nameFormat := "%s-%s"

	clusterName, ok := config.Labels[clusterv1.ClusterNameLabel]
	if !ok {
		return "", fmt.Errorf("cluster name label does not exist on agent bootstrap config %s", config.Name)
	}

	if _, isControlPlane := config.Labels[clusterv1.MachineControlPlaneLabel]; isControlPlane {
		return fmt.Sprintf(nameFormat, clusterName, "control-plane"), nil
	}

	machineDeploymentName, ok := config.Labels[clusterv1.MachineDeploymentNameLabel]
	if !ok {
		return "", fmt.Errorf("machine deployment name label does not exist on agent bootstrap config %s", config.Name)

	}
	return fmt.Sprintf(nameFormat, clusterName, machineDeploymentName), nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *OpenshiftAssistedConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bootstrapv1alpha1.OpenshiftAssistedConfig{}).
		Watches(
			&clusterv1.Machine{},
			handler.EnqueueRequestsFromMapFunc(r.FilterMachine),
		).
		Watches(
			&hivev1.ClusterDeployment{},
			&handler.EnqueueRequestForObject{},
		).
		Complete(r)
}

// Filter machine owned by this openshiftassistedconfig
func (r *OpenshiftAssistedConfigReconciler) FilterMachine(_ context.Context, o client.Object) []ctrl.Request {
	result := []ctrl.Request{}
	m, ok := o.(*clusterv1.Machine)
	if !ok {
		panic(fmt.Sprintf("Expected a Machine but got a %T", o))
	}
	// m.Spec.ClusterName

	if m.Spec.Bootstrap.ConfigRef != nil &&
		m.Spec.Bootstrap.ConfigRef.GroupVersionKind() == bootstrapv1alpha1.GroupVersion.WithKind(
			"OpenshiftAssistedConfig",
		) {
		name := client.ObjectKey{Namespace: m.Namespace, Name: m.Spec.Bootstrap.ConfigRef.Name}
		result = append(result, ctrl.Request{NamespacedName: name})
	}
	return result
}
