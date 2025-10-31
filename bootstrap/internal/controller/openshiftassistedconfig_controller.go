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
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"k8s.io/client-go/tools/reference"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util"

	"github.com/go-logr/logr"

	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/assistedinstaller"
	"github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aimodels "github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"

	logutil "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util/log"

	hivev1 "github.com/openshift/hive/apis/hive/v1"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	bootstrapv1alpha1 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/bootstrap/api/v1alpha1"
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

const (
	openshiftAssistedConfigFinalizer = "openshiftassistedconfig." + bootstrapv1alpha1.Group + "/deprovision"
	retryAfterHigh                   = 60 * time.Second
)

// HTTPClientFactory is an interface for creating HTTP clients
type HTTPClientFactory interface {
	CreateHTTPClient(config assistedinstaller.ServiceConfig, k8sClient client.Client) (*http.Client, error)
}

// DefaultHTTPClientFactory implements HTTPClientFactory using the real assistedinstaller package
type DefaultHTTPClientFactory struct{}

func (f *DefaultHTTPClientFactory) CreateHTTPClient(config assistedinstaller.ServiceConfig, k8sClient client.Client) (*http.Client, error) {
	return assistedinstaller.GetAssistedHTTPClient(config, k8sClient)
}

// OpenshiftAssistedConfigReconciler reconciles a OpenshiftAssistedConfig object
type OpenshiftAssistedConfigReconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	AssistedInstallerConfig assistedinstaller.ServiceConfig

	HttpClient        *http.Client
	HTTPClientFactory HTTPClientFactory
	httpClientMutex   sync.Mutex
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=*,verbs=create;delete;get;list;patch;update;watch
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=*,verbs=create;delete;get;list;patch;update;watch
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=*,verbs=create;delete;get;list;patch;update;watch
// +kubebuilder:rbac:groups=metal3.io,resources=baremetalhosts,verbs=list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;watch;
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=infraenvs,verbs=delete;list;watch;get;update;create
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;create;list;watch
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents,verbs=delete;list;watch;get;update
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=metal3machines;metal3machinetemplates,verbs=get;update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;create
// +kubebuilder:rbac:groups="",resources=services,verbs=list;get;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments,verbs=list;watch
// +kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=agentclusterinstalls;agentclusterinstalls/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinesets;machinesets/status,verbs=get;list;watch;

// Reconciles OpenshiftAssistedConfig
func (r *OpenshiftAssistedConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, rerr error) {
	log := ctrl.LoggerFrom(ctx)

	log.V(logutil.DebugLevel).Info("reconciling OpenshiftAssistedConfig")

	config := &bootstrapv1alpha1.OpenshiftAssistedConfig{}
	if err := r.Client.Get(ctx, req.NamespacedName, config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

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
		log.V(logutil.DebugLevel).Info("finished reconciling OpenshiftAssistedConfig")
	}()

	// Look up the owner of this openshiftassistedconfig if there is one
	configOwner, err := bsutil.GetTypedConfigOwner(ctx, r.Client, config)
	if apierrors.IsNotFound(err) {
		// Could not find the owner yet, this is not an error and will re-reconcile when the owner gets set.
		log.V(logutil.DebugLevel).Info("config owner not found", "name", configOwner.GetName())
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
			log.V(logutil.DebugLevel).
				Info(fmt.Sprintf("%s does not belong to a cluster yet, waiting until it's part of a cluster", configOwner.GetKind()))
			return ctrl.Result{}, nil
		}

		if apierrors.IsNotFound(err) {
			log.V(logutil.DebugLevel).Info("cluster does not exist yet, waiting until it is created")
			return ctrl.Result{}, nil
		}
		log.Error(err, "could not get cluster with metadata")
		return ctrl.Result{}, err
	}

	if !cluster.Status.InfrastructureReady {
		log.V(logutil.DebugLevel).Info("cluster infrastructure is not ready, waiting")
		conditions.MarkFalse(
			config,
			bootstrapv1alpha1.DataSecretAvailableCondition,
			bootstrapv1alpha1.WaitingForClusterInfrastructureReason,
			clusterv1.ConditionSeverityInfo,
			"",
		)

		return ctrl.Result{Requeue: true, RequeueAfter: retryAfterHigh}, nil
	}

	// make sure Assisted Installer resources are reconciled. When they are, we'll get the infraEnv
	// as it's necessary to retrieve ignition from it
	infraEnv, result, err := r.reconcileAssistedResources(ctx, config, cluster)
	if infraEnv == nil {
		return result, err
	}

	s := &corev1.Secret{}
	if err := r.Get(ctx, getSecretObjectKey(config), s); !apierrors.IsNotFound(err) && config.Status.Ready {
		log.V(logutil.DebugLevel).Info("bootstrap config ready and secret already created")
		return ctrl.Result{}, nil
	}

	if infraEnv.Status.InfraEnvDebugInfo.EventsURL == "" {
		log.V(logutil.TraceLevel).Info("infraenv not ready", "infraEnv", infraEnv.Status)
		conditions.MarkFalse(
			config,
			bootstrapv1alpha1.DataSecretAvailableCondition,
			bootstrapv1alpha1.InfraEnvNotReadyReason,
			clusterv1.ConditionSeverityInfo,
			"",
		)
		return ctrl.Result{}, errors.New("infraenv not ready: eventsURL not generated yet")
	}

	ignition, err := r.getIgnition(ctx, infraEnv, log)
	if err != nil {
		log.V(logutil.DebugLevel).Info("error retrieving ignition", "err", err)
		conditions.MarkFalse(
			config,
			bootstrapv1alpha1.DataSecretAvailableCondition,
			bootstrapv1alpha1.WaitingForAssistedInstallerReason,
			clusterv1.ConditionSeverityInfo,
			"failed retrieving ignition: %v", err,
		)
		return ctrl.Result{Requeue: true, RequeueAfter: retryAfter}, err
	}
	log.V(logutil.TraceLevel).Info("ignition retrieved", "bytes", len(ignition))

	secret, err := r.createUserDataSecret(ctx, config, ignition)
	if err != nil {
		log.Error(err, "could not create user data secret", "name", config.Name)
		conditions.MarkFalse(
			config,
			bootstrapv1alpha1.DataSecretAvailableCondition,
			bootstrapv1alpha1.CreatingSecretFailedReason,
			clusterv1.ConditionSeverityWarning,
			"",
		)
		return ctrl.Result{}, err
	}
	log.V(logutil.TraceLevel).Info("secret created", "secret", secret)

	config.Status.Ready = true
	config.Status.DataSecretName = &secret.Name
	conditions.MarkTrue(config, bootstrapv1alpha1.DataSecretAvailableCondition)
	return ctrl.Result{}, rerr
}

func (r *OpenshiftAssistedConfigReconciler) getIgnition(ctx context.Context, infraEnv *aiv1beta1.InfraEnv, log logr.Logger) ([]byte, error) {

	ignitionURL, err := assistedinstaller.GetIgnitionURLFromInfraEnv(r.AssistedInstallerConfig, *infraEnv)
	if err != nil {
		log.V(logutil.TraceLevel).Info("failed to retrieve ignition", "config", r.AssistedInstallerConfig, "infraEnv", infraEnv.Name)
		return nil, fmt.Errorf("error while retrieving ignitionURL: %w", err)
	}

	ignition, err := r.getIgnitionBytes(ctx, ignitionURL)
	if err != nil {
		return nil, err
	}
	return ignition, nil
}

// getHTTPClient returns a lazily-loaded HTTP client or the pre-set test client
func (r *OpenshiftAssistedConfigReconciler) getHTTPClient() (*http.Client, error) {
	r.httpClientMutex.Lock()
	defer r.httpClientMutex.Unlock()

	if r.HttpClient != nil {
		return r.HttpClient, nil
	}

	if r.HTTPClientFactory == nil {
		return nil, errors.New("HTTPClientFactory is not set")
	}

	httpClient, err := r.HTTPClientFactory.CreateHTTPClient(r.AssistedInstallerConfig, r.Client)
	if err != nil {
		return nil, err
	}

	r.HttpClient = httpClient
	return r.HttpClient, nil
}

func (r *OpenshiftAssistedConfigReconciler) getIgnitionBytes(ctx context.Context, ignitionURL *url.URL) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", ignitionURL.String(), nil)
	if err != nil {
		return nil, err
	}

	httpClient, err := r.getHTTPClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ignition request to %s returned status %d", httpReq.URL.String(), httpResp.StatusCode)
	}
	return io.ReadAll(httpResp.Body)
}

// Ensures InfraEnv exists
func (r *OpenshiftAssistedConfigReconciler) ensureInfraEnv(ctx context.Context, config *bootstrapv1alpha1.OpenshiftAssistedConfig, machine *clusterv1.Machine, clusterDeployment *hivev1.ClusterDeployment) (*aiv1beta1.InfraEnv, error) {
	log := ctrl.LoggerFrom(ctx)

	infraEnvName := getInfraEnvName(machine)
	if infraEnvName == "" {
		return nil, fmt.Errorf("no infraenv name for machine %s/%s", machine.Namespace, machine.Name)
	}
	// check if infraEnv already created
	ie := aiv1beta1.InfraEnv{}

	getInfraEnvErr := r.Get(ctx, types.NamespacedName{Name: infraEnvName, Namespace: config.Namespace}, &ie)

	// if found, no need to reconcile, as the OpenshiftAssistedConfig it's immutable
	if getInfraEnvErr == nil {
		log.V(logutil.DebugLevel).Info("infraenv already exists", "name", ie.Name, "namespace", ie.Namespace)
		return &ie, nil
	}

	// return error if the error is not NotFound
	if !apierrors.IsNotFound(getInfraEnvErr) {
		return &ie, getInfraEnvErr
	}

	// if not found, create InfraEnv
	infraEnv := assistedinstaller.GetInfraEnvFromConfig(infraEnvName, config, clusterDeployment)

	// if pullsecret ref is not set, create a fake secret.
	// Assisted Installer requires a pull secret to be set, as it's geared towards OCP
	// To improve UX for OKD users, we will create a fake pull secret
	if infraEnv.Spec.PullSecretRef == nil {
		secret, err := r.createPullsecretSecret(ctx, config)
		if err != nil {
			return &ie, err
		}
		infraEnv.Spec.PullSecretRef = &corev1.LocalObjectReference{
			Name: secret.Name,
		}
	}

	_ = controllerutil.SetOwnerReference(config, infraEnv, r.Scheme)
	_ = controllerutil.SetOwnerReference(machine, infraEnv, r.Scheme)
	err := r.Client.Create(ctx, infraEnv)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return infraEnv, err
	}

	return infraEnv, nil
}

func (r *OpenshiftAssistedConfigReconciler) createPullsecretSecret(ctx context.Context, config *bootstrapv1alpha1.OpenshiftAssistedConfig) (*corev1.Secret, error) {
	secret := assistedinstaller.GenerateFakePullSecret("", config.Namespace)
	if err := controllerutil.SetOwnerReference(config, secret, r.Scheme); err != nil {
		return secret, err
	}
	if err := r.Client.Create(ctx, secret); err != nil && !apierrors.IsAlreadyExists(err) {
		return secret, err
	}

	return secret, nil
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
func (r *OpenshiftAssistedConfigReconciler) createUserDataSecret(ctx context.Context, config *bootstrapv1alpha1.OpenshiftAssistedConfig, ignition []byte) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	if err := r.Client.Get(ctx, getSecretObjectKey(config), secret); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, err
		}
		secret.Name = config.Name
		secret.Namespace = config.Namespace
		secret.Data = map[string][]byte{
			"value":  ignition,
			"format": []byte("ignition"),
		}
		secret.Type = clusterv1.ClusterSecretType
		if err := controllerutil.SetOwnerReference(config, secret, r.Scheme); err != nil {
			return nil, err
		}

		if err := r.Client.Create(ctx, secret); err != nil {
			return nil, err
		}
	}
	return secret, nil
}

func getSecretObjectKey(config *bootstrapv1alpha1.OpenshiftAssistedConfig) client.ObjectKey {
	return client.ObjectKey{Namespace: config.Namespace, Name: config.Name}
}

// Deletes child resources (Agent) and removes finalizer
func (r *OpenshiftAssistedConfigReconciler) handleDeletion(ctx context.Context, config *bootstrapv1alpha1.OpenshiftAssistedConfig, owner *bsutil.ConfigOwner) error {
	log := ctrl.LoggerFrom(ctx)
	if controllerutil.ContainsFinalizer(config, openshiftAssistedConfigFinalizer) {
		// Check if it's a control plane node and if that cluster is being deleted
		if _, isControlPlane := config.Labels[clusterv1.MachineControlPlaneLabel]; isControlPlane &&
			owner.GetDeletionTimestamp().IsZero() {
			// Don't remove finalizer if the controlplane is not being deleted
			err := fmt.Errorf("agent bootstrap config belongs to control plane that's not being deleted")
			log.Error(err, "unable to delete bootstrap config", "config", config.Namespace+"/"+config.Name)
			return err
		}

		// Delete associated agent
		if config.Status.AgentRef != nil {
			if err := r.Client.Delete(ctx, &aiv1beta1.Agent{ObjectMeta: metav1.ObjectMeta{Name: config.Status.AgentRef.Name, Namespace: config.Namespace}}); err != nil &&
				!apierrors.IsNotFound(err) {
				log.Error(err, "failed to delete agent associated with bootstrap config", "config", config.Namespace+"/"+config.Name)
				return err
			}
			config.Status.AgentRef = nil
		}
		if config.Status.InfraEnvRef != nil {
			if err := r.Client.Delete(ctx, &aiv1beta1.InfraEnv{ObjectMeta: metav1.ObjectMeta{Name: config.Status.InfraEnvRef.Name, Namespace: config.Namespace}}); err != nil &&
				!apierrors.IsNotFound(err) {
				log.Error(err, "failed to delete infraenv associated with bootstrap config", "config", config.Namespace+"/"+config.Name)
				return err
			}
			config.Status.InfraEnvRef = nil
		}
		controllerutil.RemoveFinalizer(config, openshiftAssistedConfigFinalizer)
	}
	return nil
}

// Generate InfraEnvName. We will generate one infraEnv each machine, so that we can link back agents to machines
func getInfraEnvName(machine *clusterv1.Machine) string {
	return machine.Name
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
		Watches(
			&aiv1beta1.InfraEnv{},
			handler.EnqueueRequestsFromMapFunc(r.FilterInfraEnv),
		).
		Complete(r)
}

// Filter infraEnv to be relevant  by this openshiftassistedconfig
func (r *OpenshiftAssistedConfigReconciler) FilterInfraEnv(ctx context.Context, o client.Object) []ctrl.Request {
	logger := log.FromContext(ctx)
	result := []ctrl.Request{}
	infraEnv, ok := o.(*aiv1beta1.InfraEnv)
	if !ok {
		logger.V(logutil.DebugLevel).Info("not an InfraEnv, skipping", "object", o.GetName())
		return result
	}
	config := &bootstrapv1alpha1.OpenshiftAssistedConfig{}
	if err := util.GetTypedOwner(ctx, r.Client, infraEnv, config); err != nil {
		return result
	}
	// if owner is bootstrapConfig, let's reconcile it
	result = append(result, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(config)})
	return result
}

// Filter machine owned by this openshiftassistedconfig
func (r *OpenshiftAssistedConfigReconciler) FilterMachine(ctx context.Context, o client.Object) []ctrl.Request {
	logger := log.FromContext(ctx)
	result := []ctrl.Request{}
	m, ok := o.(*clusterv1.Machine)
	if !ok {
		logger.V(logutil.DebugLevel).Info("not a Machine, skipping", "object", o.GetName())
		return result
	}

	if m.Spec.Bootstrap.ConfigRef != nil &&
		m.Spec.Bootstrap.ConfigRef.GroupVersionKind() == bootstrapv1alpha1.GroupVersion.WithKind(
			"OpenshiftAssistedConfig",
		) {
		name := client.ObjectKey{Namespace: m.Namespace, Name: m.Spec.Bootstrap.ConfigRef.Name}
		result = append(result, ctrl.Request{NamespacedName: name})
	}
	return result
}

func (r *OpenshiftAssistedConfigReconciler) reconcileAssistedResources(ctx context.Context, config *bootstrapv1alpha1.OpenshiftAssistedConfig, cluster *clusterv1.Cluster) (*aiv1beta1.InfraEnv, ctrl.Result, error) {
	logger := log.FromContext(ctx)
	// Get the Machine that owns this openshiftassistedconfig
	machine, err := capiutil.GetOwnerMachine(ctx, r.Client, config.ObjectMeta)
	if err != nil {
		logger.Error(err, "could not get machine associated with openshiftassistedconfig", "name", config.Name)
		return nil, ctrl.Result{}, err
	}

	clusterDeployment, err := r.getClusterDeployment(ctx, cluster.GetName())
	if err != nil {
		logger.V(logutil.InfoLevel).Info("could not retrieve ClusterDeployment, requeuing", "cluster", cluster.GetName())
		conditions.MarkFalse(
			config,
			bootstrapv1alpha1.DataSecretAvailableCondition,
			bootstrapv1alpha1.WaitingForAssistedInstallerReason,
			clusterv1.ConditionSeverityInfo,
			"",
		)
		return nil, ctrl.Result{Requeue: true}, nil
	}

	aci, err := r.getAgentClusterInstall(ctx, clusterDeployment)
	if err != nil {
		logger.V(logutil.InfoLevel).Info("could not retrieve AgentClusterInstall, requeuing")
		conditions.MarkFalse(
			config,
			bootstrapv1alpha1.DataSecretAvailableCondition,
			bootstrapv1alpha1.WaitingForAssistedInstallerReason,
			clusterv1.ConditionSeverityInfo,
			"",
		)
		return nil, ctrl.Result{Requeue: true}, nil
	}

	// if added worker after start install, will be treated as day2
	if !capiutil.IsControlPlaneMachine(machine) &&
		!(aci.Status.DebugInfo.State == aimodels.ClusterStatusAddingHosts || aci.Status.DebugInfo.State == aimodels.ClusterStatusPendingForInput || aci.Status.DebugInfo.State == aimodels.ClusterStatusInsufficient || aci.Status.DebugInfo.State == "") {
		logger.V(logutil.DebugLevel).Info("not controlplane machine and installation already started, requeuing")
		conditions.MarkFalse(
			config,
			bootstrapv1alpha1.DataSecretAvailableCondition,
			bootstrapv1alpha1.WaitingForInstallCompleteReason,
			clusterv1.ConditionSeverityInfo,
			"",
		)
		return nil, ctrl.Result{Requeue: true, RequeueAfter: 60 * time.Second}, nil
	}

	infraEnv, err := r.ensureInfraEnv(ctx, config, machine, clusterDeployment)
	if err != nil {
		conditions.MarkFalse(
			config,
			bootstrapv1alpha1.DataSecretAvailableCondition,
			bootstrapv1alpha1.InfraEnvFailedReason,
			clusterv1.ConditionSeverityWarning,
			"",
		)
		return nil, ctrl.Result{}, err
	}

	// Set infraEnv ref if not already set
	if config.Status.InfraEnvRef == nil {
		ref, err := reference.GetReference(r.Scheme, infraEnv)
		if err != nil {
			return nil, ctrl.Result{}, err
		}
		config.Status.InfraEnvRef = ref
	}
	return infraEnv, ctrl.Result{}, nil
}
