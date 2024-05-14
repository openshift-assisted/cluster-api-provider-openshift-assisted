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
	"github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aimodels "github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"time"

	hivev1 "github.com/openshift/hive/apis/hive/v1"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metal3 "github.com/metal3-io/cluster-api-provider-metal3/api/v1beta1"
	bootstrapv1beta1 "github.com/openshift-assisted/cluster-api-agent/bootstrap/api/v1beta1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	bsutil "sigs.k8s.io/cluster-api/bootstrap/util"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

//TODO: Use caches

// AgentBootstrapConfigReconciler reconciles a AgentBootstrapConfigSpec object
type AgentBootstrapConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *AgentBootstrapConfigReconciler) getMachineTemplate(ctx context.Context, machineDeployment clusterv1.MachineDeployment) *metal3.Metal3MachineTemplate {
	log := ctrl.LoggerFrom(ctx)
	machineTemplateRef := machineDeployment.Spec.Template.Spec.InfrastructureRef
	log.Info("machine template ref", "groupversionkind", machineTemplateRef.GroupVersionKind())
	if machineTemplateRef.GroupVersionKind() == metal3.GroupVersion.WithKind("Metal3MachineTemplate") {
		nsName := types.NamespacedName{
			Name:      machineTemplateRef.Name,
			Namespace: machineDeployment.Namespace,
		}
		machineTemplate := &metal3.Metal3MachineTemplate{}
		if err := r.Client.Get(ctx, nsName, machineTemplate); err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("machine template not found", "namespacedname", nsName)
				return nil
			}
			return nil
		}
		return machineTemplate
	}
	return nil
}

// +kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=agentclusterinstalls,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=agentclusterinstalls/status,verbs=get
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=metal3machines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch;create;update;patch;delete
//metal3machinetemplates" in API group "infrastructure.cluster.x-k8s.io"
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=metal3machines;metal3machines/status,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=agentbootstrapconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=agentbootstrapconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=agentbootstrapconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=infraenvs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=infraenvs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=metal3.io,resources=baremetalhosts,verbs=get;list;watch;update;patch

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

	log.Info("Reconciling AgentBootstrapConfig")
	defer (func() {
		log.Info("Finished reconciling AgentBootstrapConfig")

	})()

	config := &bootstrapv1beta1.AgentBootstrapConfig{}
	log.Info("Getting AgentBootstrapConfig", "namespacedname", req.NamespacedName)
	if err := r.Client.Get(ctx, req.NamespacedName, config); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	// Initialize the patch helper.
	patchHelper, err := patch.NewHelper(config, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	// Look up the owner of this agentbootstrapconfig if there is one
	configOwner, err := bsutil.GetTypedConfigOwner(ctx, r.Client, config)
	if apierrors.IsNotFound(err) {
		// Could not find the owner yet, this is not an error and will rereconcile when the owner gets set.
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "failed to get owner")
	}
	if configOwner == nil {
		return ctrl.Result{}, nil
	}

	log.Info("found config owner", "name", configOwner.GetName())

	// Attempt to Patch the KubeadmConfig object and status after each reconciliation if no error occurs.
	defer func() {
		// always update the readyCondition; the summary is represented using the "1 of x completed" notation.
		conditions.SetSummary(config,
			conditions.WithConditions(
				bootstrapv1beta1.DataSecretAvailableCondition,
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
	}()

	clusterName, ok := config.Labels[clusterv1.ClusterNameLabel]
	if !ok {
		return ctrl.Result{}, fmt.Errorf("cluster name label not found in config")
	}
	clusterDeployments := hivev1.ClusterDeploymentList{}
	if err := r.Client.List(ctx, &clusterDeployments, client.MatchingLabels{clusterv1.ClusterNameLabel: clusterName}); err != nil {
		return ctrl.Result{}, err
	}
	if len(clusterDeployments.Items) != 1 {
		log.Info("found more or less than 1 cluster deployments. exactly one is needed", "cluster_name", clusterName)
		// if no ClusterDeployment we should pause this machine?
		return ctrl.Result{Requeue: true, RequeueAfter: 10 * time.Second}, nil
	}

	clusterDeployment := clusterDeployments.Items[0]
	if clusterDeployment.Spec.ClusterInstallRef == nil {
		log.Info("cluster deployment does not reference ACI", "name", clusterDeployment.Name, "namespace", clusterDeployment.Namespace)
		return ctrl.Result{Requeue: true, RequeueAfter: 10 * time.Second}, nil
	}
	objKey := types.NamespacedName{
		Namespace: clusterDeployment.Namespace,
		Name:      clusterDeployment.Spec.ClusterInstallRef.Name,
	}
	aci := v1beta1.AgentClusterInstall{}
	if err := r.Client.Get(ctx, objKey, &aci); err != nil {
		log.Info("could not retrieve ACI", "name", objKey.Name, "namespace", objKey.Namespace)
		return ctrl.Result{}, err
	}

	// Get the Machine associated with this agentbootstrapconfig

	// TODO: change the way we get this, for now it has the same name but that may not be the case - we should change to fetch by spec's reference to this agentbootstrapconfig
	machine, err := util.GetMachineByName(ctx, r.Client, config.Namespace, config.Name)
	if err != nil {
		log.Error(err, "couldn't get machine associated with agentbootstrapconfig", "name", config.Name)
		return ctrl.Result{}, err
	}

	// if added worker after start install, will be treated as day2
	if !util.IsControlPlaneMachine(machine) && !(aci.Status.DebugInfo.State == aimodels.ClusterStatusAddingHosts || aci.Status.DebugInfo.State == aimodels.ClusterStatusPendingForInput || aci.Status.DebugInfo.State == aimodels.ClusterStatusInsufficient || aci.Status.DebugInfo.State == "") {
		log.Info("not controlplane machine and installation already started, requeuing")
		return ctrl.Result{Requeue: true, RequeueAfter: 60 * time.Second}, nil
	}

	// IF not installing yet or finished, can deal with the machine
	// IF installing, requeue
	infraEnvName, err := getInfraEnvName(config)
	if err != nil {
		log.Error(err, "couldn't get infraenv name for agentbootstrapconfig", "name", config.Name)
		return ctrl.Result{}, err
	}
	log.Info("computed infraEnvName", "name", infraEnvName)

	if infraEnvName == "" {
		log.Info("no infraenv name for agentbootstrapconfig", "name", config.Name)
		return ctrl.Result{}, nil
	}

	// Query for InfraEnv based on name/namespace and set it if it exists or create it if it doesn't
	infraEnv := &aiv1beta1.InfraEnv{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: infraEnvName, Namespace: config.Namespace}, infraEnv); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "couldn't get infraenv for agentbootstrapconfig", "agentbootstrap config name", config.Name, "infra env name", infraEnv)
			return ctrl.Result{}, err
		}

		infraEnv, err = r.createInfraEnv(ctx, config, infraEnvName, &clusterDeployment)
		//TODO: make this more efficient
		if !apierrors.IsAlreadyExists(err) {
			log.Error(err, "couldn't create infraenv", "name", config.Name)
			return ctrl.Result{}, err
		}
		log.Info("Created infra env", "name", infraEnv.Name, "namespace", infraEnv.Namespace)
	}

	// Set infraEnv if not already set
	if config.Spec.InfraEnvRef == nil {
		config.Spec.InfraEnvRef = &corev1.ObjectReference{Name: infraEnv.Name, Namespace: infraEnv.Namespace, Kind: "InfraEnv", APIVersion: infraEnv.APIVersion}
		if err := r.Client.Update(ctx, config); err != nil {
			log.Error(err, "couldn't update agent config", "name", config.Name)
			return ctrl.Result{}, err
		}
	}

	if config.Status.ISODownloadURL == "" {
		return ctrl.Result{}, nil
	}

	// Get Metal3 Machine owned by this Machine that is related to this agentbootstrapconfig
	metal3Machine := &metal3.Metal3Machine{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: machine.Spec.InfrastructureRef.Name, Namespace: machine.Namespace}, metal3Machine); err != nil {
		log.Error(err, "couldn't get metal3machine associated with machine and agentbootstrapconfig", "agentbootstrapconfig name", config.Name, "machine name", machine.Name)
		return ctrl.Result{}, err
	}

	// TODO: check if it's a control plane or worker
	log.Info("Found metal3 machine owned by machine", "name", metal3Machine.Name, "namespace", metal3Machine.Namespace)
	if metal3Machine.Spec.Image.URL == "" || metal3Machine.Spec.Image.URL != config.Status.ISODownloadURL {
		// Change metal3 image url if it's empty or it doesn't match the agent bootstrap config iso download url
		log.Info("adding ISO URL to metal3 machine because it's either empty or doesn't match the iso download URL", "original URL", metal3Machine.Spec.Image.URL, "new URL", config.Status.ISODownloadURL)
		metal3Machine.Spec.Image.URL = config.Status.ISODownloadURL
		if err := r.Client.Update(ctx, metal3Machine); err != nil {
			log.Error(err, "couldn't update metal3 machine", "name", metal3Machine.Name, "namespace", metal3Machine.Namespace)
			return ctrl.Result{}, err
		}
		log.Info("Added ISO URLs to metal3 machines", "machine", metal3Machine.Name, "namespace", metal3Machine.Namespace)
	}

	// create secret
	secret := &corev1.Secret{}
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: config.Namespace, Name: config.Name}, secret); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "couldn't get secret", "name", config.Name, "namespace", config.Namespace)
			return ctrl.Result{}, err
		}
		secret.Name = config.Name
		secret.Namespace = config.Namespace

		if err := r.Client.Create(ctx, secret); err != nil {
			log.Error(err, "couldn't create secret", "name", config.Name, "namespace", config.Namespace)
			return ctrl.Result{}, err
		}
	}

	config.Status.Ready = true
	config.Status.DataSecretName = &config.Name
	conditions.MarkTrue(config, bootstrapv1beta1.DataSecretAvailableCondition)
	return ctrl.Result{}, rerr
}

func (r *AgentBootstrapConfigReconciler) isReferencingACI(clusterDeployment *hivev1.ClusterDeployment) bool {
	return clusterDeployment.Spec.ClusterInstallRef != nil
}

func getInfraEnvName(config *bootstrapv1beta1.AgentBootstrapConfig) (string, error) {
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
		For(&bootstrapv1beta1.AgentBootstrapConfig{}).
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

	if m.Spec.Bootstrap.ConfigRef != nil && m.Spec.Bootstrap.ConfigRef.GroupVersionKind() == bootstrapv1beta1.GroupVersion.WithKind("AgentBootstrapConfigSpec") {
		name := client.ObjectKey{Namespace: m.Namespace, Name: m.Spec.Bootstrap.ConfigRef.Name}
		result = append(result, ctrl.Request{NamespacedName: name})
	}
	return result
}

func (r *AgentBootstrapConfigReconciler) createInfraEnv(ctx context.Context, config *bootstrapv1beta1.AgentBootstrapConfig, infraEnvName string, clusterDeployment *hivev1.ClusterDeployment) (*aiv1beta1.InfraEnv, error) {
	infraEnv := &aiv1beta1.InfraEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name:      infraEnvName,
			Namespace: config.Namespace,
			Labels: map[string]string{
				bootstrapv1beta1.AgentBootstrapConfigLabel: config.Name,
			},
		},
	}

	clusterName, ok := config.Labels[clusterv1.ClusterNameLabel]
	if ok {
		infraEnv.Labels[clusterv1.ClusterNameLabel] = clusterName
	}

	var pullSecret *corev1.LocalObjectReference
	if config.Spec.PullSecretRef != nil {
		pullSecret = config.Spec.PullSecretRef
	} else {
		//TODO: create logic for placeholder pull secret
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

/*
func (r *AgentBootstrapConfigReconciler) getACIFromClusterDeployment(ctx context.Context, clusterDeployment *hivev1.ClusterDeployment) (*v1beta1.AgentClusterInstall, error) {
	log := ctrl.LoggerFrom(ctx)

	if !r.isReferencingACI(clusterDeployment) {
		return nil, errors.Errorf("clusterDeployment not referencing agentClusterInstall")
	}
	aciNamespacedName := types.NamespacedName{
		Name:      clusterDeployment.Spec.ClusterInstallRef.Name,
		Namespace: clusterDeployment.Namespace,
	}
	aci := &v1beta1.AgentClusterInstall{}
	err := r.Get(ctx, aciNamespacedName, aci)
	if err != nil {
		log.Info("error while getting agentClusterInstall", "error", err)
		return nil, err
	}
	return aci, nil
}
*/
