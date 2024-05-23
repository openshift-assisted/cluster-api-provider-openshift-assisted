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

	controlplanev1alpha1 "github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1alpha1"
	"github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aimodels "github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"

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
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

var liveIsoFormat string = "live-iso"

const (
	agentBootstrapConfigFinalizer = "agentbootstrapconfig." + bootstrapv1beta1.Group + "/deprovision"
)

// AgentBootstrapConfigReconciler reconciles a AgentBootstrapConfigSpec object
type AgentBootstrapConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=agentclusterinstalls,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=agentclusterinstalls/status,verbs=get
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=metal3machines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=metal3machines;metal3machines/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=metal3machinetemplates;metal3machinetemplates/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=agentbootstrapconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=agentbootstrapconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=agentbootstrapconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=infraenvs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=infraenvs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=metal3.io,resources=baremetalhosts,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=agentcontrolplanes,verbs=get;list;watch;
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinedeployments;machinedeployments/status,verbs=get;list;watch;

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the AgentBootstrapConfigSpec object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.2/pkg/reconcile
func (r *AgentBootstrapConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, rerr error) {
	log := ctrl.LoggerFrom(ctx)

	log.V(logutil.TraceLevel).Info("Reconciling AgentBootstrapConfig")
	defer (func() {
		log.V(logutil.TraceLevel).Info("Finished reconciling AgentBootstrapConfig")
	})()

	config := &bootstrapv1alpha1.AgentBootstrapConfig{}
	if err := r.Client.Get(ctx, req.NamespacedName, config); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(logutil.TraceLevel).Info("agentBootstrapConfig not found", "namespacedname", req.NamespacedName)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	log.WithValues("agentBootstrapConfig Name", config.Name, "agentBootstrapConfig Namespace", config.Namespace)

	// Initialize the patch helper.
	patchHelper, err := patch.NewHelper(config, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Attempt to Patch the KubeadmConfig object and status after each reconciliation if no error occurs.
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
		log.V(logutil.TraceLevel).Info("Finished reconciling AgentBootstrapConfig")
	}()

	// Look up the owner of this agentbootstrapconfig if there is one
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
		return r.handleDeletion(ctx, config, configOwner)
	}

	if !controllerutil.ContainsFinalizer(config, agentBootstrapConfigFinalizer) {
		controllerutil.AddFinalizer(config, agentBootstrapConfigFinalizer)
		if err := r.Client.Update(ctx, config); err != nil {
			log.Error(rerr, "couldn't update Agent Bootstrap Config")
			return ctrl.Result{}, err
		}
	}
	clusterName, ok := config.Labels[clusterv1.ClusterNameLabel]
	if !ok {
		return ctrl.Result{}, fmt.Errorf("cluster name label not found in config")
	}

	// Get the Machine associated with this agentbootstrapconfig

	// TODO: change the way we get this, for now it has the same name but that may not be the case - we should change to fetch by spec's reference to this agentbootstrapconfig
	machine, err := util.GetMachineByName(ctx, r.Client, config.Namespace, config.Name)
	if err != nil {
		log.Error(err, "couldn't get machine associated with agentbootstrapconfig", "name", config.Name)
		return ctrl.Result{}, err
	}

	clusterDeployment, err := r.getClusterDeployment(ctx, clusterName)
	if err != nil {
		log.V(logutil.InfoLevel).Info("could not retrieve ClusterDeployment... requeuing")
		return ctrl.Result{Requeue: true}, nil
	}

	aci, err := r.getAgentClusterInstall(ctx, clusterDeployment)
	if err != nil {
		log.V(logutil.InfoLevel).Info("could not retrieve AgentClusterInstall... requeuing")
		return ctrl.Result{Requeue: true}, nil
	}

	// if added worker after start install, will be treated as day2
	if !util.IsControlPlaneMachine(machine) && !(aci.Status.DebugInfo.State == aimodels.ClusterStatusAddingHosts || aci.Status.DebugInfo.State == aimodels.ClusterStatusPendingForInput || aci.Status.DebugInfo.State == aimodels.ClusterStatusInsufficient || aci.Status.DebugInfo.State == "") {
		log.V(logutil.DebugLevel).Info("not controlplane machine and installation already started, requeuing")

		return ctrl.Result{Requeue: true, RequeueAfter: 60 * time.Second}, nil
	}

	if err := r.ensureInfraEnv(ctx, config, clusterDeployment); err != nil {
		return ctrl.Result{}, err
	}

	if config.Status.ISODownloadURL == "" {
		return ctrl.Result{}, nil
	}

	if err := r.setMetal3MachineTemplateImage(ctx, config, machine); err != nil {
		return ctrl.Result{}, err
	}

	// if a metal3 machine booted before the template was updated, then we need to update it
	if err := r.setMetal3MachineImage(ctx, config, machine); err != nil {
		return ctrl.Result{}, err
	}

	secret, err := r.createUserDataSecret(ctx, config)
	if err != nil {
		log.Error(err, "couldn't create user data secret", "name", config.Name)
		return ctrl.Result{}, err
	}

	config.Status.Ready = true
	config.Status.DataSecretName = &secret.Name
	conditions.MarkTrue(config, bootstrapv1alpha1.DataSecretAvailableCondition)
	return ctrl.Result{}, rerr
}

func (r *AgentBootstrapConfigReconciler) ensureInfraEnv(ctx context.Context, config *bootstrapv1alpha1.AgentBootstrapConfig, clusterDeployment *hivev1.ClusterDeployment) error {
	log := ctrl.LoggerFrom(ctx)
	infraEnvName, err := getInfraEnvName(config)
	log.WithValues("AgentBootstrapConfig Name", config.Name, "AgentBootstrapConfig Namespace", config.Namespace, "InfraEnv Name", infraEnvName)

	if err != nil {
		log.Error(err, "couldn't get infraenv name for agentbootstrapconfig")
		return err
	}
	log.V(logutil.DebugLevel).Info("computed infraEnvName", "name", infraEnvName)

	if infraEnvName == "" {
		log.V(logutil.DebugLevel).Info("no infraenv name for agentbootstrapconfig")
		return fmt.Errorf("no infraenv name for agentbootstrapconfig")
	}

	// Query for InfraEnv based on name/namespace and set it if it exists or create it if it doesn't
	infraEnv := &aiv1beta1.InfraEnv{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: infraEnvName, Namespace: config.Namespace}, infraEnv); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}

		infraEnv, err = r.createInfraEnv(ctx, config, infraEnvName, clusterDeployment)
		//TODO: make this more efficient

		if err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
		log.V(logutil.DebugLevel).Info("Created infra env", "name", infraEnv.Name, "namespace", infraEnv.Namespace)
	}

	// Set infraEnv if not already set
	if config.Status.InfraEnvRef == nil {
		config.Status.InfraEnvRef = &corev1.ObjectReference{Name: infraEnv.Name, Namespace: infraEnv.Namespace, Kind: "InfraEnv", APIVersion: infraEnv.APIVersion}
	}
	return nil
}

func (r *AgentBootstrapConfigReconciler) getAgentClusterInstall(ctx context.Context, clusterDeployment *hivev1.ClusterDeployment) (*v1beta1.AgentClusterInstall, error) {
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

func (r *AgentBootstrapConfigReconciler) getClusterDeployment(ctx context.Context, clusterName string) (*hivev1.ClusterDeployment, error) {
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

func (r *AgentBootstrapConfigReconciler) createUserDataSecret(ctx context.Context, config *bootstrapv1alpha1.AgentBootstrapConfig) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: config.Namespace, Name: config.Name}, secret); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, err
		}
		secret.Name = config.Name
		secret.Namespace = config.Namespace

		if err := r.Client.Create(ctx, secret); err != nil {
			return nil, err
		}
	}
	return secret, nil
}

func (r *AgentBootstrapConfigReconciler) setMetal3MachineImage(ctx context.Context, config *bootstrapv1alpha1.AgentBootstrapConfig, machine *clusterv1.Machine) error {
	log := ctrl.LoggerFrom(ctx)
	m3MachineKey := types.NamespacedName{Name: machine.Spec.InfrastructureRef.Name, Namespace: machine.Spec.InfrastructureRef.Namespace}
	log.WithValues("AgentBootstrapConfig Name", config.Name, "AgentBootstrapConfig Namespace", config.Namespace, "Metal3Machine Name", m3MachineKey.Name, "Metal3Machine Namespace", m3MachineKey.Namespace)

	metal3Machine := &metal3.Metal3Machine{}
	if err := r.Client.Get(ctx, m3MachineKey, metal3Machine); err != nil {
		log.Error(err, "couldn't get metal3machine associated with machine and agentbootstrapconfig")
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

		metal3Machine.Spec.Image.URL = config.Status.ISODownloadURL
		metal3Machine.Spec.Image.DiskFormat = &liveIsoFormat
		if err := r.Client.Update(ctx, metal3Machine); err != nil {
			log.Error(err, "couldn't update metal3 machine")
			return err
		}
		log.V(logutil.TraceLevel).Info("Added ISO URLs to metal3 machines", "machine", metal3Machine.Name, "namespace", metal3Machine.Namespace)
	}
	return nil
}
func (r *AgentBootstrapConfigReconciler) getTypedMachineOwner(ctx context.Context, machine *clusterv1.Machine, obj client.Object) error {
	log := ctrl.LoggerFrom(ctx)

	// TODO: can we guess Kind and APIVersion before retrieving it?
	for _, ownerRef := range machine.OwnerReferences {
		err := r.Client.Get(ctx, types.NamespacedName{
			Namespace: machine.Namespace,
			Name:      ownerRef.Name,
		}, obj)
		if err != nil {
			log.V(logutil.TraceLevel).Info(fmt.Sprintf("could not find %T", obj), "name", ownerRef.Name, "namespace", machine.Namespace)
			continue
		}
		gvk := obj.GetObjectKind().GroupVersionKind()
		if ownerRef.APIVersion == gvk.GroupVersion().String() && ownerRef.Kind == gvk.Kind {
			return nil
		}
	}
	return fmt.Errorf("couldn't find %T owner for machine", obj)
}

func (r *AgentBootstrapConfigReconciler) getInfrastructureRefKey(ctx context.Context, machine *clusterv1.Machine) (types.NamespacedName, error) {
	acp := controlplanev1alpha1.AgentControlPlane{}
	err := r.getTypedMachineOwner(ctx, machine, &acp)
	if err != nil {
		// Machine is not owned by ACP, check for MD
		md := clusterv1.MachineDeployment{}
		if err := r.getTypedMachineOwner(ctx, machine, &md); err != nil {
			return types.NamespacedName{}, fmt.Errorf("machine has neither acp nor md owner")
		}
		return types.NamespacedName{
			Namespace: md.Spec.Template.Spec.InfrastructureRef.Namespace,
			Name:      md.Spec.Template.Spec.InfrastructureRef.Name,
		}, nil
	}
	return types.NamespacedName{
		Namespace: acp.Spec.MachineTemplate.InfrastructureRef.Namespace,
		Name:      acp.Spec.MachineTemplate.InfrastructureRef.Name,
	}, nil
}

func (r *AgentBootstrapConfigReconciler) setMetal3MachineTemplateImage(ctx context.Context, config *bootstrapv1alpha1.AgentBootstrapConfig, machine *clusterv1.Machine) error {
	log := ctrl.LoggerFrom(ctx)
	tplKey, err := r.getInfrastructureRefKey(ctx, machine)
	log.WithValues("Metal3MachineTemplate Name", tplKey.Name, "Metal3MachineTemplate Namespace", tplKey.Namespace)

	if err != nil {
		return err
	}
	machineTpl := &metal3.Metal3MachineTemplate{}

	if err := r.Client.Get(ctx, tplKey, machineTpl); err != nil {
		log.Error(err, "couldn't find machine template")
		return err
	}

	if machineTpl.Spec.Template.Spec.Image.URL != config.Status.ISODownloadURL || machineTpl.Spec.Template.Spec.Image.DiskFormat != &liveIsoFormat {
		machineTpl.Spec.Template.Spec.Image.URL = config.Status.ISODownloadURL
		machineTpl.Spec.Template.Spec.Image.DiskFormat = &liveIsoFormat
		if err := r.Client.Update(ctx, machineTpl); err != nil {
			log.Error(err, "couldn't update machine template")
			return err
		}
	}
	return nil
}

func (r *AgentBootstrapConfigReconciler) handleDeletion(ctx context.Context, config *bootstrapv1alpha1.AgentBootstrapConfig, owner *bsutil.ConfigOwner) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	if controllerutil.ContainsFinalizer(config, agentBootstrapConfigFinalizer) {
		// Check if it's a control plane node and if that cluster is being deleted
		if _, isControlPlane := config.Labels[clusterv1.MachineControlPlaneLabel]; isControlPlane && owner.GetDeletionTimestamp().IsZero() {
			// Don't remove finalizer if the controlplane is not being deleted
			err := fmt.Errorf("Agent bootstrap config belongs to control plane that's not being deleted")
			log.Error(err, "unable to delete agent bootstrap config")
			return ctrl.Result{}, err
		}

		// Remove finalizer to delete
		if controllerutil.RemoveFinalizer(config, agentBootstrapConfigFinalizer) {
			if err := r.Client.Update(ctx, config); err != nil {
				log.Error(err, "failed removing the AgentBootstrapConfig finalizer")
				return ctrl.Result{}, err
			}
		}
	}
	return ctrl.Result{}, nil
}

func getInfraEnvName(config *bootstrapv1alpha1.AgentBootstrapConfig) (string, error) {
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
func (r *AgentBootstrapConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bootstrapv1alpha1.AgentBootstrapConfig{}).
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

// Filter machine owned by this agentbootstrapconfig
func (r *AgentBootstrapConfigReconciler) FilterMachine(_ context.Context, o client.Object) []ctrl.Request {
	result := []ctrl.Request{}
	m, ok := o.(*clusterv1.Machine)
	if !ok {
		panic(fmt.Sprintf("Expected a Machine but got a %T", o))
	}
	// m.Spec.ClusterName

	if m.Spec.Bootstrap.ConfigRef != nil && m.Spec.Bootstrap.ConfigRef.GroupVersionKind() == bootstrapv1alpha1.GroupVersion.WithKind("AgentBootstrapConfigSpec") {
		name := client.ObjectKey{Namespace: m.Namespace, Name: m.Spec.Bootstrap.ConfigRef.Name}
		result = append(result, ctrl.Request{NamespacedName: name})
	}
	return result
}

func (r *AgentBootstrapConfigReconciler) createInfraEnv(ctx context.Context, config *bootstrapv1alpha1.AgentBootstrapConfig, infraEnvName string, clusterDeployment *hivev1.ClusterDeployment) (*aiv1beta1.InfraEnv, error) {
	infraEnv := &aiv1beta1.InfraEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name:      infraEnvName,
			Namespace: config.Namespace,
			Labels: map[string]string{
				bootstrapv1alpha1.AgentBootstrapConfigLabel: config.Name,
			},
		},
	}

	clusterName, ok := config.Labels[clusterv1.ClusterNameLabel]
	if ok {
		infraEnv.Labels[clusterv1.ClusterNameLabel] = clusterName
	}

	//TODO: create logic for placeholder pull secret
	var pullSecret *corev1.LocalObjectReference
	if config.Spec.PullSecretRef != nil {
		pullSecret = config.Spec.PullSecretRef
	}
	infraEnv.Spec = aiv1beta1.InfraEnvSpec{
		ClusterRef: &aiv1beta1.ClusterReference{
			Name:      clusterDeployment.Name,
			Namespace: clusterDeployment.Namespace,
		},
		PullSecretRef:    pullSecret,
		SSHAuthorizedKey: config.Spec.SSHAuthorizedKey,
	}
	return infraEnv, r.Create(ctx, infraEnv)
}
