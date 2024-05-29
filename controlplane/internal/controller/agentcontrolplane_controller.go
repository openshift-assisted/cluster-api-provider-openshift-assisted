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
	"time"

	"sigs.k8s.io/cluster-api/controllers/external"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/labels/format"

	bootstrapv1beta1 "github.com/openshift-assisted/cluster-api-agent/bootstrap/api/v1beta1"
	"github.com/openshift/hive/apis/hive/v1/agent"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/storage/names"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/annotations"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/collections"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	controlplanev1beta1 "github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
)

const (
	agentControlPlaneKind = "AgentControlPlane"
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

	// handle deletion
	acp := &controlplanev1beta1.AgentControlPlane{}
	if err := r.Client.Get(ctx, req.NamespacedName, acp); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	defer func() {
		if rerr = r.Client.Status().Update(ctx, acp); rerr != nil {
			log.Error(rerr, "couldn't update AgentControlPlane Status", "name", acp.Name, "namespace", acp.Namespace)
		}
	}()

	cluster, err := util.GetOwnerCluster(ctx, r.Client, acp.ObjectMeta)
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
		err, clusterDeployment := r.createClusterDeployment(ctx, acp, cluster.Name)
		if err != nil {
			log.Error(
				err,
				"couldn't create clusterDeployment",
				"name", clusterDeployment.Name,
				"namespace", clusterDeployment.Namespace,
			)
			return ctrl.Result{}, err
		}

		if clusterDeployment != nil {
			acp.Status.ClusterDeploymentRef = &corev1.ObjectReference{
				Name:       clusterDeployment.Name,
				Namespace:  clusterDeployment.Namespace,
				Kind:       "ClusterDeployment",
				APIVersion: hivev1.SchemeGroupVersion.String(),
			}
			if err = r.Client.Status().Update(ctx, acp); err != nil {
				log.Error(rerr, "couldn't update AgentControlPlane", "name", acp.Name, "namespace", acp.Namespace)
				return ctrl.Result{}, err
			}
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
			if err := r.Client.Update(ctx, acp); err != nil {
				log.Error(err, "failed to updated agentcontrolplane to unreference clusterdeployment")
				return ctrl.Result{}, err
			}
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

func (r *AgentControlPlaneReconciler) computeDesiredMachine(acp *controlplanev1beta1.AgentControlPlane, cluster *clusterv1.Cluster) (*clusterv1.Machine, error) {
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
		annotations[controlplanev1beta1.AgentClusterConfigurationAnnotation] = string(clusterConfig)
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
				*metav1.NewControllerRef(acp, controlplanev1beta1.GroupVersion.WithKind(agentControlPlaneKind)),
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
	desiredMachine.Labels = controlPlaneMachineLabelsForCluster(acp, cluster.Name)

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

func (r *AgentControlPlaneReconciler) createClusterDeployment(ctx context.Context, acp *controlplanev1beta1.AgentControlPlane, clusterName string) (error, *hivev1.ClusterDeployment) {
	var pullSecret *corev1.LocalObjectReference
	if acp.Spec.AgentConfigSpec.PullSecretRef != nil {
		pullSecret = acp.Spec.AgentConfigSpec.PullSecretRef
	}
	//TODO: create logic for placeholder pull secret

	// Get cluster clusterName instead of reference to ACP clusterName
	clusterDeployment := &hivev1.ClusterDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      acp.Name,
			Namespace: acp.Namespace,
			Labels:    controlPlaneMachineLabelsForCluster(acp, clusterName),
		},
		Spec: hivev1.ClusterDeploymentSpec{
			ClusterName: clusterName,
			BaseDomain:  acp.Spec.AgentConfigSpec.BaseDomain,
			Platform: hivev1.Platform{
				AgentBareMetal: &agent.BareMetalPlatform{
					//AgentSelector: metav1.LabelSelector{}, // TODO: What label should we select?
				},
			},
			//PreserveOnDelete: True, // TODO take from CR
			//ControlPlaneConfig: hivev1.ControlPlaneConfigSpec{}, //
			//Ingress: ( []hivev1.ClusterIngress)
			//CertificateBundles: ([]hivev1.CertificateBundleSpec)
			// ManageDNS: bool,
			//ClusterMetadata: *hivev1.ClusterMetadata.
			Installed: false,
			// Provisioning: *hivev1.Provisioning
			//ClusterInstallRef: // reference to AgentClusterInstall  *ClusterInstallLocalReference
			// ClusterPoolRef *ClusterPoolReference `json:"clusterPoolRef,omitempty"`,
			// ClusterPoolRef *ClusterPoolReference `json:"clusterPoolRef,omitempty"`
			PullSecretRef: pullSecret,
		},
	}
	return r.Create(ctx, clusterDeployment), clusterDeployment
}

// SetupWithManager sets up the controller with the Manager.
func (r *AgentControlPlaneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	//TODO: maybe enqueue for clusterdeployment owned by this ACP?
	return ctrl.NewControllerManagedBy(mgr).
		For(&controlplanev1beta1.AgentControlPlane{}).
		Complete(r)
}

// ControlPlaneMachineLabelsForCluster returns a set of labels to add to a control plane machine for this specific cluster.
func controlPlaneMachineLabelsForCluster(acp *controlplanev1beta1.AgentControlPlane, clusterName string) map[string]string {
	labels := map[string]string{}

	// Add the labels from the MachineTemplate.
	// Note: we intentionally don't use the map directly to ensure we don't modify the map in KCP.
	for k, v := range acp.Spec.MachineTemplate.ObjectMeta.Labels {
		labels[k] = v
	}

	// Always force these labels over the ones coming from the spec.
	labels[clusterv1.ClusterNameLabel] = clusterName
	labels[clusterv1.MachineControlPlaneLabel] = ""
	// Note: MustFormatValue is used here as the label value can be a hash if the control plane name is longer than 63 characters.
	labels[clusterv1.MachineControlPlaneNameLabel] = format.MustFormatValue(acp.Name)
	return labels
}

func (r *AgentControlPlaneReconciler) cloneConfigsAndGenerateMachine(ctx context.Context, cluster *clusterv1.Cluster, acp *controlplanev1beta1.AgentControlPlane, bootstrapSpec *bootstrapv1beta1.AgentBootstrapConfigSpec) error {
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
		APIVersion: controlplanev1beta1.GroupVersion.String(),
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
		Labels:      controlPlaneMachineLabelsForCluster(acp, cluster.Name),
		Annotations: acp.Spec.MachineTemplate.ObjectMeta.Annotations,
	})
	if err != nil {
		// Safe to return early here since no resources have been created yet.
		conditions.MarkFalse(acp, controlplanev1beta1.MachinesCreatedCondition, controlplanev1beta1.InfrastructureTemplateCloningFailedReason,
			clusterv1.ConditionSeverityError, err.Error())
		return errors.Wrap(err, "failed to clone infrastructure template")
	}
	machine.Spec.InfrastructureRef = *infraRef
	log.Info("Generating control plane bootstrap config for cluster", "cluster", cluster.Name)
	// Clone the bootstrap configuration
	bootstrapRef, err := r.generateAgentBootstrapConfig(ctx, acp, cluster, bootstrapSpec, machine.Name)
	if err != nil {
		log.Info("Error generating control plane bootstrap config", "err", err)
		conditions.MarkFalse(acp, controlplanev1beta1.MachinesCreatedCondition, controlplanev1beta1.BootstrapTemplateCloningFailedReason,
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
			conditions.MarkFalse(acp, controlplanev1beta1.MachinesCreatedCondition, controlplanev1beta1.MachineGenerationFailedReason,
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

func (r *AgentControlPlaneReconciler) createMachine(ctx context.Context, acp *controlplanev1beta1.AgentControlPlane, machine *clusterv1.Machine) error {
	// TODO : add cache everywhere
	if err := r.Client.Create(ctx, machine); err != nil {
		return errors.Wrap(err, "failed to create Machine")
	}

	// Remove the annotation tracking that a remediation is in progress (the remediation completed when
	// the replacement machine has been created above).
	// delete(acp.Annotations, controlplanev1.RemediationInProgressAnnotation)
	return nil
}

func (r *AgentControlPlaneReconciler) generateAgentBootstrapConfig(ctx context.Context, acp *controlplanev1beta1.AgentControlPlane, cluster *clusterv1.Cluster, spec *bootstrapv1beta1.AgentBootstrapConfigSpec, name string) (*corev1.ObjectReference, error) {
	// Create an owner reference without a controller reference because the owning controller is the machine controller
	owner := metav1.OwnerReference{
		APIVersion: controlplanev1beta1.GroupVersion.String(),
		Kind:       agentControlPlaneKind,
		Name:       acp.Name,
		UID:        acp.UID,
	}

	spec.PullSecretRef = acp.Spec.AgentBootstrapConfigSpec.PullSecretRef
	bootstrapConfig := &bootstrapv1beta1.AgentBootstrapConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       acp.Namespace,
			Labels:          controlPlaneMachineLabelsForCluster(acp, cluster.Name),
			Annotations:     acp.Spec.MachineTemplate.ObjectMeta.Annotations,
			OwnerReferences: []metav1.OwnerReference{owner},
		},
		Spec: *spec,
	}
	//bootstrapConfig.Spec.PullSecretRef = acp.Spec.AgentBootstrapConfigSpec.PullSecretRef

	if err := r.Client.Create(ctx, bootstrapConfig); err != nil {
		return nil, errors.Wrap(err, "Failed to create bootstrap configuration")
	}

	bootstrapRef := &corev1.ObjectReference{
		APIVersion: bootstrapv1beta1.GroupVersion.String(),
		Kind:       "AgentBootstrapConfig",
		Name:       bootstrapConfig.GetName(),
		Namespace:  bootstrapConfig.GetNamespace(),
		UID:        bootstrapConfig.GetUID(),
	}

	return bootstrapRef, nil
}
