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
	"github.com/openshift-assisted/cluster-api-agent/assistedinstaller"
	"github.com/openshift-assisted/cluster-api-agent/util"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/reference"
	"sigs.k8s.io/cluster-api/util/patch"
	"time"

	bootstrapv1alpha1 "github.com/openshift-assisted/cluster-api-agent/bootstrap/api/v1alpha1"
	logutil "github.com/openshift-assisted/cluster-api-agent/util/log"
	"sigs.k8s.io/cluster-api/controllers/external"
	"sigs.k8s.io/cluster-api/util/conditions"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/storage/names"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/annotations"

	"k8s.io/apimachinery/pkg/runtime"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/collections"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	controlplanev1alpha1 "github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1alpha1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
)

const (
	agentControlPlaneKind = "AgentControlPlane"
	acpFinalizer          = "agentcontrolplane." + controlplanev1alpha1.Group + "/deprovision"
)

// AgentControlPlaneReconciler reconciles a AgentControlPlane object
type AgentControlPlaneReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=agentbootstrapconfigs,verbs=get;list;watch;create;update;patch;delete
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
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=agentcontrolplanes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=agentcontrolplanes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=agentcontrolplanes/finalizers,verbs=update
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinepools,verbs=list
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the AgentControlPlane object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.2/pkg/reconcile
func (r *AgentControlPlaneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, rerr error) {
	log := ctrl.LoggerFrom(ctx)

	acp := &controlplanev1alpha1.AgentControlPlane{}
	if err := r.Client.Get(ctx, req.NamespacedName, acp); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	log.WithValues("AgentControlPlane Name", acp.Name, "AgentControlPlane Namespace", acp.Namespace)
	log.V(logutil.TraceLevel).Info("Started reconciling AgentControlPlane")

	// Initialize the patch helper.
	patchHelper, err := patch.NewHelper(acp, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Attempt to Patch the AgentControlPlane object and status after each reconciliation if no error occurs.
	defer func() {
		// Patch ObservedGeneration only if the reconciliation completed successfully
		patchOpts := []patch.Option{}
		if rerr == nil {
			patchOpts = append(patchOpts, patch.WithStatusObservedGeneration{})
		}
		if err := patchHelper.Patch(ctx, acp, patchOpts...); err != nil {
			rerr = kerrors.NewAggregate([]error{rerr, err})
		}

		log.V(logutil.TraceLevel).Info("Finished reconciling AgentControlPlane")
	}()

	if acp.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, acp)
	}

	if !controllerutil.ContainsFinalizer(acp, acpFinalizer) {
		controllerutil.AddFinalizer(acp, acpFinalizer)
	}

	cluster, err := capiutil.GetOwnerCluster(ctx, r.Client, acp.ObjectMeta)
	if err != nil {
		log.Error(err, "Failed to retrieve owner Cluster from the API Server")
		return ctrl.Result{}, err
	}
	if cluster == nil {
		log.Info("Cluster Controller has not yet set OwnerRef")
		return ctrl.Result{Requeue: true, RequeueAfter: 10 * time.Second}, nil
	}

	if annotations.IsPaused(cluster, acp) {
		log.Info("Reconciliation is paused for this object")
		return ctrl.Result{}, nil
	}

	if !cluster.Status.InfrastructureReady || !cluster.Spec.ControlPlaneEndpoint.IsValid() {
		log.Info("cluster is not ready, retrying...")
		return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 20}, nil
	}
	// TODO: handle changes in pull-secret (for example)
	// create clusterdeployment if not set
	if acp.Status.ClusterDeploymentRef == nil {
		log.Info("Creating clusterdeployment")
		clusterDeployment := assistedinstaller.GetClusterDeploymentFromConfig(acp, cluster.Name)
		if err := r.Create(ctx, clusterDeployment); err != nil && !apierrors.IsAlreadyExists(err) {
			log.Error(
				err,
				"couldn't create clusterDeployment",
				"name", clusterDeployment.Name,
				"namespace", clusterDeployment.Namespace,
			)
			return ctrl.Result{}, err
		}

		if clusterDeployment != nil {
			ref, err := reference.GetReference(r.Scheme, clusterDeployment)
			if err != nil {
				log.Error(err, "could not create reference to clusterDeployment")
				return
			}
			acp.Status.ClusterDeploymentRef = ref
			log.Info("Added clusterdeployment ref")
		}
	}
	// Retrieve clusterdeployment
	cd := &hivev1.ClusterDeployment{}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: acp.Status.ClusterDeploymentRef.Namespace, Name: acp.Status.ClusterDeploymentRef.Name}, cd); err != nil {
		if apierrors.IsNotFound(err) {
			// Cluster deployment no longer exists, unset reference and re-reconcile
			acp.Status.ClusterDeploymentRef = nil
			log.Info("Clusterdeployment doesn't exist, unset reference and reconcile again")
			return ctrl.Result{Requeue: true, RequeueAfter: 10 * time.Second}, nil
		}
	}

	// Retrieve actually labeled machines instead
	machines, err := collections.GetFilteredMachinesForCluster(ctx, r.Client, cluster, collections.OwnedMachines(acp))
	if err != nil {
		log.Error(err, "couldn't get machines for AgentControlPlane", "name", acp.Name, "namespace", acp.Namespace)
		return ctrl.Result{}, err
	}

	// If machines are the same number as desired replicas - do nothing.. or wait for bootstrap config,
	// Then set status when ready
	// Then do the workers (in the bootstrap)
	numMachines := int32(machines.Len()) //acp.Status.Replicas
	desiredReplicas := acp.Spec.Replicas
	machinesToCreate := desiredReplicas - numMachines

	readyMachines := machines.Filter(collections.IsReady())
	acp.Status.ReadyReplicas = int32(readyMachines.Len())

	acp.Status.Replicas = numMachines
	acp.Status.UpdatedReplicas = numMachines
	acp.Status.UnavailableReplicas = numMachines - acp.Status.ReadyReplicas

	log.Info("ACP", "all acp spec", acp.Spec)
	if machinesToCreate > 0 {
		log.Info("Creating Machines", "number of machines", machinesToCreate)
		for i := 0; i < int(machinesToCreate); i++ {
			log.Info("Scaling up control plane", "Desired", desiredReplicas, "Existing", numMachines)
			bootstrapSpec := acp.Spec.AgentBootstrapConfigSpec.DeepCopy()
			log.Info("agent bootstrap config", "spec", bootstrapSpec, "all acp spec", acp.Spec)
			bootstrapSpec.PullSecretRef = acp.Spec.AgentBootstrapConfigSpec.PullSecretRef.DeepCopy()
			if err := r.cloneConfigsAndGenerateMachine(ctx, cluster, acp, bootstrapSpec); err != nil {
				log.Info("Error cloning configs", "err", err)
			}
		}
	}
	return ctrl.Result{}, rerr
}

// Ensures dependencies are deleted before allowing the AgentControlPlane to be deleted
// Deletes the ClusterDeployment (which deletes the AgentClusterInstall)
// Machines, InfraMachines, and AgentBootstrapConfigs get auto-deleted when the ACP has a deletion timestamp - this deprovisions the BMH automatically
// TODO: should we handle watching until all machines & agentbootstrapconfigs are deleted too?
func (r *AgentControlPlaneReconciler) handleDeletion(ctx context.Context, acp *controlplanev1alpha1.AgentControlPlane) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	if !controllerutil.ContainsFinalizer(acp, acpFinalizer) {
		log.V(logutil.TraceLevel).Info("ACP doesn't contain finalizer, allow deletion")
		return ctrl.Result{}, nil
	}

	// Delete cluster deployment
	if err := r.deleteClusterDeployment(ctx, acp.Status.ClusterDeploymentRef); err != nil && !apierrors.IsNotFound(err) {
		log.Error(err, "failed deleting cluster deployment for ACP")
		return ctrl.Result{}, err
	}
	log.Info("ACP's clusterdeployment is deleted")
	acp.Status.ClusterDeploymentRef = nil

	// will be updated in the deferred function
	controllerutil.RemoveFinalizer(acp, acpFinalizer)
	return ctrl.Result{}, nil
}

func (r *AgentControlPlaneReconciler) deleteClusterDeployment(ctx context.Context, clusterDeployment *corev1.ObjectReference) error {
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

func (r *AgentControlPlaneReconciler) computeDesiredMachine(acp *controlplanev1alpha1.AgentControlPlane, cluster *clusterv1.Cluster) (*clusterv1.Machine, error) {
	var machineName string
	var machineUID types.UID
	var version *string
	annotations := map[string]string{
		"bmac.agent-install.openshift.io/role": "master",
		"foo":                                  "bar",
	}

	// Creating a new machine
	machineName = names.SimpleNameGenerator.GenerateName(acp.Name + "-")
	version = &acp.Spec.Version

	/*
		// Machine's bootstrap config may be missing ClusterConfiguration if it is not the first machine in the control plane.
		// We store ClusterConfiguration as annotation here to detect any changes in KCP ClusterConfiguration and rollout the machine if any.
		// Nb. This annotation is read when comparing the KubeadmConfig to check if a machine needs to be rolled out.
		clusterConfig, err := json.Marshal(acp.Spec.KubeadmConfigSpec.ClusterConfiguration)
		if err != nil {
			return nil, errors.Wrap(err, "failed to marshal cluster configuration")
		}
		annotations[controlplanev1alpha1.AgentClusterConfigurationAnnotation] = string(clusterConfig)
	*/
	// TODO: add label for role
	// Construct the basic Machine.
	desiredMachine := &clusterv1.Machine{
		TypeMeta: metav1.TypeMeta{
			APIVersion: clusterv1.GroupVersion.String(),
			Kind:       "Machine",
		},
		ObjectMeta: metav1.ObjectMeta{
			UID:       machineUID,
			Name:      machineName,
			Namespace: acp.Namespace,
			// Note: by setting the ownerRef on creation we signal to the Machine controller that this is not a stand-alone Machine.
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(acp, controlplanev1alpha1.GroupVersion.WithKind(agentControlPlaneKind)),
			},
			Labels:      map[string]string{},
			Annotations: map[string]string{},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     version,
		},
	}

	// Set the in-place mutable fields.
	// When we create a new Machine we will just create the Machine with those fields.
	// When we update an existing Machine will we update the fields on the existing Machine (in-place mutate).

	// Set labels
	desiredMachine.Labels = util.ControlPlaneMachineLabelsForCluster(acp, cluster.Name)

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

	return desiredMachine, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AgentControlPlaneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	//TODO: maybe enqueue for clusterdeployment owned by this ACP in case it gets deleted...?
	return ctrl.NewControllerManagedBy(mgr).
		For(&controlplanev1alpha1.AgentControlPlane{}).
		Complete(r)
}

func (r *AgentControlPlaneReconciler) cloneConfigsAndGenerateMachine(ctx context.Context, cluster *clusterv1.Cluster, acp *controlplanev1alpha1.AgentControlPlane, bootstrapSpec *bootstrapv1alpha1.AgentBootstrapConfigSpec) error {
	var errs []error

	log := ctrl.LoggerFrom(ctx)
	log.Info("Computing desired machines for cluster", "cluster", cluster.Name)
	log.Info("Bootstrap spec", "pull secret ref", bootstrapSpec.PullSecretRef)
	// Compute desired Machine
	machine, err := r.computeDesiredMachine(acp, cluster)
	if err != nil {
		return errors.Wrap(err, "failed to create Machine: failed to compute desired Machine")
	}

	// Since the cloned resource should eventually have a controller ref for the Machine, we create an
	// OwnerReference here without the Controller field set
	infraCloneOwner := &metav1.OwnerReference{
		APIVersion: controlplanev1alpha1.GroupVersion.String(),
		Kind:       agentControlPlaneKind,
		Name:       acp.Name,
		UID:        acp.UID,
	}

	// Clone the infrastructure template
	infraRef, err := external.CreateFromTemplate(ctx, &external.CreateFromTemplateInput{
		Client:      r.Client,
		TemplateRef: &acp.Spec.MachineTemplate.InfrastructureRef,
		Namespace:   acp.Namespace,
		Name:        machine.Name,
		OwnerRef:    infraCloneOwner,
		ClusterName: cluster.Name,
		Labels:      util.ControlPlaneMachineLabelsForCluster(acp, cluster.Name),
		Annotations: acp.Spec.MachineTemplate.ObjectMeta.Annotations,
	})
	if err != nil {
		// Safe to return early here since no resources have been created yet.
		conditions.MarkFalse(acp, controlplanev1alpha1.MachinesCreatedCondition, controlplanev1alpha1.InfrastructureTemplateCloningFailedReason,
			clusterv1.ConditionSeverityError, err.Error())
		return errors.Wrap(err, "failed to clone infrastructure template")
	}
	machine.Spec.InfrastructureRef = *infraRef
	log.Info("Generating control plane bootstrap config for cluster", "cluster", cluster.Name)
	// Clone the bootstrap configuration
	bootstrapRef, err := r.generateAgentBootstrapConfig(ctx, acp, cluster, bootstrapSpec, machine.Name)
	if err != nil {
		log.Info("Error generating control plane bootstrap config", "err", err)
		conditions.MarkFalse(acp, controlplanev1alpha1.MachinesCreatedCondition, controlplanev1alpha1.BootstrapTemplateCloningFailedReason,
			clusterv1.ConditionSeverityError, err.Error())
		errs = append(errs, errors.Wrap(err, "failed to generate bootstrap config"))
	}
	log.Info("Checking errors", "cluster", cluster.Name, "errors", len(errs))
	// Only proceed to generating the Machine if we haven't encountered an error
	if len(errs) == 0 {
		machine.Spec.Bootstrap.ConfigRef = bootstrapRef
		log.Info("Creating machine for cluster...", "cluster", cluster.Name)
		if err := r.createMachine(ctx, acp, machine); err != nil {
			log.Info("Error creating machine config", "err", err)
			conditions.MarkFalse(acp, controlplanev1alpha1.MachinesCreatedCondition, controlplanev1alpha1.MachineGenerationFailedReason,
				clusterv1.ConditionSeverityError, err.Error())
			errs = append(errs, errors.Wrap(err, "failed to create Machine"))
		}
	}
	/*
		// If we encountered any errors, attempt to clean up any dangling resources
		if len(errs) > 0 {
			if err := r.cleanupFromGeneration(ctx, infraRef, bootstrapRef); err != nil {
				errs = append(errs, errors.Wrap(err, "failed to cleanup generated resources"))
			}

			return kerrors.NewAggregate(errs)
		}
	*/
	return nil
}

func (r *AgentControlPlaneReconciler) createMachine(ctx context.Context, acp *controlplanev1alpha1.AgentControlPlane, machine *clusterv1.Machine) error {
	if err := r.Client.Create(ctx, machine); err != nil {
		return errors.Wrap(err, "failed to create Machine")
	}

	// Remove the annotation tracking that a remediation is in progress (the remediation completed when
	// the replacement machine has been created above).
	// delete(acp.Annotations, controlplanev1.RemediationInProgressAnnotation)
	return nil
}

func (r *AgentControlPlaneReconciler) generateAgentBootstrapConfig(ctx context.Context, acp *controlplanev1alpha1.AgentControlPlane, cluster *clusterv1.Cluster, spec *bootstrapv1alpha1.AgentBootstrapConfigSpec, name string) (*corev1.ObjectReference, error) {
	// Create an owner reference without a controller reference because the owning controller is the machine controller
	owner := metav1.OwnerReference{
		APIVersion: controlplanev1alpha1.GroupVersion.String(),
		Kind:       agentControlPlaneKind,
		Name:       acp.Name,
		UID:        acp.UID,
	}

	spec.PullSecretRef = acp.Spec.AgentBootstrapConfigSpec.PullSecretRef
	bootstrapConfig := &bootstrapv1alpha1.AgentBootstrapConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       acp.Namespace,
			Labels:          util.ControlPlaneMachineLabelsForCluster(acp, cluster.Name),
			Annotations:     acp.Spec.MachineTemplate.ObjectMeta.Annotations,
			OwnerReferences: []metav1.OwnerReference{owner},
		},
		Spec: *spec,
	}

	if err := r.Client.Create(ctx, bootstrapConfig); err != nil {
		return nil, errors.Wrap(err, "Failed to create bootstrap configuration")
	}

	return reference.GetReference(r.Scheme, bootstrapConfig)
}
