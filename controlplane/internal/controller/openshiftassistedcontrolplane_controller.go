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
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/kubevirt"
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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apiserver/pkg/storage/names"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/controllers/external"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/conditions"
	utilconversion "sigs.k8s.io/cluster-api/util/conversion"
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

func isKubeVirtInfra(cluster *clusterv1.Cluster) bool {
	return cluster != nil && cluster.Spec.InfrastructureRef.Kind == "KubevirtCluster"
}

// resolveInfraVersion determines the API version for the infrastructure provider
// referenced by the Cluster. Since CAPI v1beta2 ContractVersionedObjectReference
// does not carry an API version, we use a static mapping for known providers.
// The CAPI contract resolution pattern (GetObjectFromContractVersionedRef) requires
// a client.Reader and performs a full CRD lookup on each call. For KubevirtMachine
// listing/patching in hot reconcile paths, the static mapping avoids that overhead.
// If a new provider is added, extend the mapping below.
func resolveInfraVersion(ref clusterv1.ContractVersionedObjectReference) string {
	// Known provider version mappings
	knownVersions := map[string]string{
		"infrastructure.cluster.x-k8s.io": "v1alpha1",
		"bootstrap.cluster.x-k8s.io":     "v1alpha2",
		"controlplane.cluster.x-k8s.io":  "v1alpha3",
	}
	if v, ok := knownVersions[ref.APIGroup]; ok {
		return v
	}
	return "v1alpha1"
}

// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;update
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
// +kubebuilder:rbac:groups=config.openshift.io,resources=apiservers,verbs=get;list;watch

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
				string(controlplanev1alpha3.ControlPlaneAvailableCondition),
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
		log.V(logutil.DebugLevel).Info("deleting OpenshiftAssistedControlPlane")
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

	if !cluster.Spec.ControlPlaneEndpoint.IsValid() {
		if err := r.propagateControlPlaneEndpoint(ctx, cluster); err != nil {
			log.Info("control plane endpoint not valid, waiting for infrastructure provider", "error", err)
		}
		return ctrl.Result{RequeueAfter: time.Second * 10}, nil
	}
	if cluster.Status.Initialization.InfrastructureProvisioned == nil || !*cluster.Status.Initialization.InfrastructureProvisioned {
		if err := r.ensureInfrastructureProvisioned(ctx, cluster); err != nil {
			log.V(1).Info("could not ensure infrastructureProvisioned", "error", err)
			if !isInfrastructureProvisioned(cluster) {
				return ctrl.Result{RequeueAfter: time.Second * 5}, nil
			}
		} else {
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
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

		// Reconcile infra machine status (providerID + Ready condition) once the
		// tenant cluster is accessible. CAPK cannot do this itself because it doesn't
		// have access to the workload cluster kubeconfig.
		if isKubeVirtInfra(cluster) {
			if syncErr := r.reconcileInfraMachineStatus(ctx, oacp); syncErr != nil {
				ctrl.LoggerFrom(ctx).V(1).Info("could not reconcile infra machine status, will retry", "error", syncErr)
				result.RequeueAfter = 30 * time.Second
			}
		}
	}
	return result, r.reconcileReplicas(ctx, oacp, cluster)
}

func isInfrastructureProvisioned(cluster *clusterv1.Cluster) bool {
	if cluster.Status.Initialization.InfrastructureProvisioned != nil {
		return *cluster.Status.Initialization.InfrastructureProvisioned
	}
	for _, c := range cluster.Status.Conditions {
		if c.Type == clusterv1.ClusterInfrastructureReadyCondition {
			return c.Status == metav1.ConditionTrue
		}
	}
	return false
}

// ensureInfrastructureProvisioned checks if the infrastructure cluster is ready
// and sets Cluster.status.initialization.infrastructureProvisioned. This is
// needed because the CAPI Machine controller gates on this field, and the infra
// provider (CAPK) may fail to set it due to CRD validation issues.
func (r *OpenshiftAssistedControlPlaneReconciler) ensureInfrastructureProvisioned(ctx context.Context, cluster *clusterv1.Cluster) error {
	infraRef := cluster.Spec.InfrastructureRef
	if infraRef.Kind == "" || infraRef.Name == "" {
		return fmt.Errorf("infrastructure ref not set")
	}
	infraObj := &unstructured.Unstructured{}
	infraObj.SetGroupVersionKind(schema.GroupVersionKind{
		Group: infraRef.APIGroup, Version: resolveInfraVersion(infraRef), Kind: infraRef.Kind,
	})
	if err := r.Get(ctx, types.NamespacedName{Name: infraRef.Name, Namespace: cluster.Namespace}, infraObj); err != nil {
		return err
	}
	ready, _, _ := unstructured.NestedBool(infraObj.Object, "status", "ready")
	if !ready {
		return fmt.Errorf("infrastructure not ready")
	}
	trueVal := true
	patch := client.MergeFrom(cluster.DeepCopy())
	cluster.Status.Initialization.InfrastructureProvisioned = &trueVal
	if err := r.Status().Patch(ctx, cluster, patch); err != nil {
		return err
	}
	ctrl.LoggerFrom(ctx).Info("set Cluster infrastructureProvisioned=true")
	return nil
}

// matchInternalIPFromUnstructured returns the first InternalIP from an
// unstructured address slice that exists in ipMap. Returns "" if no match.
func matchInternalIPFromUnstructured(addresses []interface{}, ipMap map[string]string) string {
	for _, addr := range addresses {
		addrMap, ok := addr.(map[string]interface{})
		if !ok {
			continue
		}
		addrStr, ok := addrMap["address"].(string)
		if !ok || addrStr == "" {
			continue
		}
		if addrMap["type"] == "InternalIP" {
			if name, found := ipMap[addrStr]; found {
				return name
			}
		}
	}
	return ""
}

// findKVMachineByNodeIP returns the KubevirtMachine name whose InternalIP
// matches one of the node's InternalIP addresses.
func findKVMachineByNodeIP(node corev1.Node, kvMachines *unstructured.UnstructuredList) string {
	for i := range kvMachines.Items {
		kvm := &kvMachines.Items[i]
		addresses, _, _ := unstructured.NestedSlice(kvm.Object, "status", "addresses")
		for _, nodeAddr := range node.Status.Addresses {
			if nodeAddr.Type != corev1.NodeInternalIP {
				continue
			}
			for _, a := range addresses {
				aMap, ok := a.(map[string]interface{})
				if !ok {
					continue
				}
				aAddr, ok := aMap["address"].(string)
				if !ok {
					continue
				}
				if aMap["type"] == "InternalIP" && aAddr == nodeAddr.Address {
					return kvm.GetName()
				}
			}
		}
	}
	return ""
}

// reconcileInfraMachineStatus patches KubevirtMachine providerID and Ready condition
// after the tenant cluster is installed. CAPK cannot do this itself because its
// node-matching logic requires workload cluster access that isn't configured.
func (r *OpenshiftAssistedControlPlaneReconciler) reconcileInfraMachineStatus(ctx context.Context, oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane) error {
	log := ctrl.LoggerFrom(ctx)

	// Get tenant cluster kubeconfig
	kubeconfigSecret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Name: oacp.Name + "-kubeconfig", Namespace: oacp.Namespace}, kubeconfigSecret); err != nil {
		return err
	}
	kubeconfigData, ok := kubeconfigSecret.Data["value"]
	if !ok {
		return fmt.Errorf("kubeconfig secret missing 'value' key")
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		return fmt.Errorf("failed to parse tenant kubeconfig: %w", err)
	}
	restConfig.Timeout = 10 * time.Second

	tenantClient, err := client.New(restConfig, client.Options{})
	if err != nil {
		return fmt.Errorf("failed to create tenant client: %w", err)
	}

	// List nodes in tenant cluster
	nodeList := &corev1.NodeList{}
	if err := tenantClient.List(ctx, nodeList); err != nil {
		return fmt.Errorf("failed to list tenant nodes: %w", err)
	}

	// Build IP → node name map
	nodeByIP := make(map[string]string)
	for _, node := range nodeList.Items {
		for _, addr := range node.Status.Addresses {
			if addr.Type == corev1.NodeInternalIP {
				nodeByIP[addr.Address] = node.Name
			}
		}
	}

	// List KubevirtMachines
	kvMachines := &unstructured.UnstructuredList{}
	kvMachines.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "infrastructure.cluster.x-k8s.io",
		Version: "v1alpha1",
		Kind:    "KubevirtMachineList",
	})
	if err := r.List(ctx, kvMachines, client.InNamespace(oacp.Namespace), client.MatchingLabels{
		clusterv1.ClusterNameLabel: oacp.Labels[clusterv1.ClusterNameLabel],
	}); err != nil {
		return fmt.Errorf("failed to list KubevirtMachines: %w", err)
	}

	for i := range kvMachines.Items {
		kvm := &kvMachines.Items[i]

		providerID, _, _ := unstructured.NestedString(kvm.Object, "spec", "providerID")
		initProvisioned, _, _ := unstructured.NestedBool(kvm.Object, "status", "initialization", "provisioned")
		if providerID != "" && initProvisioned {
			continue
		}

		addresses, _, _ := unstructured.NestedSlice(kvm.Object, "status", "addresses")
		matchedNode := matchInternalIPFromUnstructured(addresses, nodeByIP)
		if matchedNode == "" {
			continue
		}

		// Patch spec (providerID) and status (initialization + conditions) separately.
		// Must use separate snapshots because r.Patch() for spec overwrites kvm with
		// the server response, wiping in-memory status modifications.
		specPatch := client.MergeFrom(kvm.DeepCopy())
		_ = unstructured.SetNestedField(kvm.Object, fmt.Sprintf("kubevirt://%s", kvm.GetName()), "spec", "providerID")
		if err := r.Patch(ctx, kvm, specPatch); err != nil {
			log.V(1).Info("failed to patch KubevirtMachine providerID", "name", kvm.GetName(), "error", err)
			continue
		}

		// Re-read the object to get fresh state after spec patch, then patch status
		if err := r.Get(ctx, client.ObjectKeyFromObject(kvm), kvm); err != nil {
			log.V(1).Info("failed to re-read KubevirtMachine", "name", kvm.GetName(), "error", err)
			continue
		}
		statusPatch := client.MergeFrom(kvm.DeepCopy())
		_ = unstructured.SetNestedField(kvm.Object, true, "status", "initialization", "provisioned")
		if err := r.Status().Patch(ctx, kvm, statusPatch); err != nil {
			log.V(1).Info("failed to patch KubevirtMachine status", "name", kvm.GetName(), "error", err)
			continue
		}
		log.Info("patched KubevirtMachine with providerID and Ready condition", "name", kvm.GetName(), "node", matchedNode)
	}

	// Also set providerID on tenant cluster nodes so CAPI can match them to Machines.
	// Without a cloud-controller-manager, nodes don't get providerIDs automatically.
	for _, node := range nodeList.Items {
		if node.Spec.ProviderID != "" {
			continue
		}
		matchedVM := findKVMachineByNodeIP(node, kvMachines)
		if matchedVM == "" {
			continue
		}
		providerID := fmt.Sprintf("kubevirt://%s", matchedVM)
		nodePatch := client.MergeFrom(node.DeepCopy())
		node.Spec.ProviderID = providerID
		if err := tenantClient.Patch(ctx, &node, nodePatch); err != nil {
			log.V(1).Info("failed to set providerID on tenant node", "node", node.Name, "error", err)
			continue
		}
		log.Info("set providerID on tenant node", "node", node.Name, "providerID", providerID)
	}

	return nil
}

// propagateControlPlaneEndpoint reads the controlPlaneEndpoint from the
// infrastructure cluster object (e.g., KubevirtCluster) and copies it to the
// CAPI Cluster spec. CAPI core does not auto-propagate this field; it is the
// control plane provider's responsibility.
func (r *OpenshiftAssistedControlPlaneReconciler) propagateControlPlaneEndpoint(ctx context.Context, cluster *clusterv1.Cluster) error {
	infraRef := cluster.Spec.InfrastructureRef
	if infraRef.Kind == "" || infraRef.Name == "" {
		return fmt.Errorf("infrastructure ref not set on cluster")
	}

	infraObj := &unstructured.Unstructured{}
	infraObj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   infraRef.APIGroup,
		Version: resolveInfraVersion(infraRef),
		Kind:    infraRef.Kind,
	})

	if err := r.Get(ctx, types.NamespacedName{Name: infraRef.Name, Namespace: cluster.Namespace}, infraObj); err != nil {
		return fmt.Errorf("failed to get infrastructure object: %w", err)
	}

	host, _, _ := unstructured.NestedString(infraObj.Object, "spec", "controlPlaneEndpoint", "host")
	port, _, _ := unstructured.NestedInt64(infraObj.Object, "spec", "controlPlaneEndpoint", "port")
	if host == "" || port == 0 {
		return fmt.Errorf("infrastructure object has no controlPlaneEndpoint set")
	}

	cluster.Spec.ControlPlaneEndpoint.Host = host
	cluster.Spec.ControlPlaneEndpoint.Port = int32(port)
	if err := r.Update(ctx, cluster); err != nil {
		return fmt.Errorf("failed to update cluster controlPlaneEndpoint: %w", err)
	}
	ctrl.LoggerFrom(ctx).Info("propagated controlPlaneEndpoint from infrastructure provider", "host", host, "port", port)
	return nil
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

	upgradeConditionMessage, err := upgrader.GetUpgradeStatus(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Always sync the tenant's actual running version into status
	currentVersion, err := upgrader.GetCurrentVersion(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get OpenShift version from ClusterVersion: %w", err)
	}
	oacp.Status.DistributionVersion = currentVersion

	// Check if the tenant's desired version already matches what we want
	isDesiredVersionUpdated, err := upgrader.IsDesiredVersionUpdated(ctx, oacp.Spec.DistributionVersion)
	if err != nil {
		return ctrl.Result{}, err
	}

	// First: detect if the tenant upgraded itself past spec (BEFORE checking upgrade-in-progress,
	// because a stale spec can trigger a downgrade that shows as "in progress").
	specVersion, specErr := semver.ParseTolerant(oacp.Spec.DistributionVersion)
	statusVersion, statusErr := semver.ParseTolerant(oacp.Status.DistributionVersion)

	if specErr != nil && oacp.Spec.DistributionVersion != "" {
		log.Info("spec.distributionVersion is not valid semver, skipping upgrade",
			"distributionVersion", oacp.Spec.DistributionVersion, "error", specErr)
		return ctrl.Result{}, nil
	}

	if specErr == nil && statusErr == nil && statusVersion.GT(specVersion) {
		// Tenant upgraded itself past spec — log but do not mutate spec.
		// spec.distributionVersion is user-managed; status reflects the observed version.
		log.Info("tenant cluster version exceeds spec.distributionVersion",
			"spec", oacp.Spec.DistributionVersion, "status", oacp.Status.DistributionVersion)
		return ctrl.Result{}, nil
	}

	// If spec matches status (tenant already at desired version) and no upgrade in progress → done
	if isWorkloadClusterRunningDesiredVersion(oacp) && !isUpdateInProgress {
		if conditions.IsFalse(oacp, string(controlplanev1alpha3.UpgradeCompletedCondition)) {
			setConditionTrue(oacp, controlplanev1alpha3.UpgradeCompletedCondition)
		}
		return ctrl.Result{}, nil
	}

	// If an upgrade is in progress (either tenant-initiated or management-initiated), observe it
	if isUpdateInProgress {
		setUpgradeStatus(oacp, true, upgradeConditionMessage)
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	// Only push an upgrade when spec.distributionVersion is NEWER than what the tenant
	// is currently running. Never push a downgrade.
	specIsNewer := specErr == nil && statusErr == nil && specVersion.GT(statusVersion)

	if specIsNewer && !isDesiredVersionUpdated {
		log.Info("pushing management-initiated upgrade to tenant cluster",
			"from", oacp.Status.DistributionVersion, "to", oacp.Spec.DistributionVersion)
		return ctrl.Result{RequeueAfter: 1 * time.Minute},
			upgrader.UpdateClusterVersionDesiredUpdate(
				ctx,
				oacp.Spec.DistributionVersion,
				architecture,
				getUpgradeOptions(oacp, pullSecret)...,
			)
	}

	// Tenant's desired version matches spec but hasn't completed yet — waiting
	if isDesiredVersionUpdated && !isWorkloadClusterRunningDesiredVersion(oacp) {
		setUpgradeStatus(oacp, false, upgradeConditionMessage)
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	return ctrl.Result{}, nil
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

	// Clean up DNS forwarding rule (cluster-scoped, won't be garbage-collected)
	cluster, clusterErr := capiutil.GetOwnerCluster(ctx, r.Client, oacp.ObjectMeta)
	if clusterErr != nil {
		return fmt.Errorf("could not fetch owner cluster during deletion: %w", clusterErr)
	}
	if isKubeVirtInfra(cluster) {
		if err := kubevirt.RemoveDNSForwardingRule(ctx, r.Client, oacp.Name, oacp.Namespace); err != nil {
			return fmt.Errorf("could not remove DNS forwarding rule: %w", err)
		}
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
	annotations := map[string]string{}

	// Only set the BMAC role annotation for BareMetal platform (Metal3/CAPM3)
	if !isKubeVirtInfra(cluster) {
		annotations["bmac.agent-install.openshift.io/role"] = "master"
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

	// Note: by setting the controller ownerRef on creation we signal to the Machine controller that this is not a stand-alone Machine.
	_ = controllerutil.SetControllerReference(oacp, desiredMachine, r.Scheme)

	// Set the in-place mutable fields.
	// When we create a new Machine we will just create the Machine with those fields.
	// When we update an existing Machine will we update the fields on the existing Machine (in-place mutate).

	desiredMachine.Labels = util.ControlPlaneMachineLabelsForCluster(oacp, cluster.Name)

	// Propagate watch-filter label from the Cluster so the CAPI controller
	// (when running with --watch-filter) will reconcile child Machines.
	if wf, ok := cluster.Labels[clusterv1.WatchLabel]; ok {
		desiredMachine.Labels[clusterv1.WatchLabel] = wf
	}

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
		cd.Spec.Platform = clusterDeploymentPlatform()
		cd.Spec.PullSecretRef = oacp.Spec.Config.PullSecretRef

		return nil
	})

	return err
}

// clusterDeploymentPlatform returns the Hive Platform spec for the ClusterDeployment.
// All assisted-installer deployments use AgentBareMetal at the ClusterDeployment level
// regardless of the actual infrastructure provider. The real platform differentiation
// happens at the AgentClusterInstall.Spec.PlatformType level.
func clusterDeploymentPlatform() hivev1.Platform {
	return hivev1.Platform{
		AgentBareMetal: &agent.BareMetalPlatform{},
	}
}

func (r *OpenshiftAssistedControlPlaneReconciler) reconcileReplicas(ctx context.Context, oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane, cluster *clusterv1.Cluster) error {
	log := ctrl.LoggerFrom(ctx)
	ownerGK := schema.GroupKind{Group: controlplanev1alpha3.Group, Kind: "OpenshiftAssistedControlPlane"}
	machines, err := collections.GetFilteredMachinesForCluster(ctx, r.Client, cluster, collections.OwnedMachines(oacp, ownerGK))
	if err != nil {
		return err
	}

	numMachines := machines.Len()
	desiredReplicas := int(oacp.Spec.Replicas)
	machinesToCreate := desiredReplicas - numMachines

	// Signal ControlPlaneInitialized once all control plane Machines exist.
	// In the Assisted Installer flow, workers boot from the same InfraEnv ISO and
	// register with the assisted-service (not the cluster's kube-apiserver), so they
	// can be provisioned in parallel with the control plane. Setting this early
	// ungates CAPI's MachineDeployment controller to create worker Machines immediately,
	// enabling fully declarative day-0 cluster creation with masters + workers together.
	if machinesToCreate <= 0 && (oacp.Status.Initialization.ControlPlaneInitialized == nil || !*oacp.Status.Initialization.ControlPlaneInitialized) {
		log.Info("all control plane machines created, setting ControlPlaneInitialized to ungate worker provisioning")
		oacp.Status.Initialization.ControlPlaneInitialized = ptr.To(true)
	}

	if machinesToCreate > 0 {
		// For KubeVirt platform, serialize machine creation to avoid a race condition
		// where multiple VMs boot simultaneously and some fail to read their config drive.
		// Only create the next machine once all existing machines have their bootstrap
		// data secret populated (DataSecretName set by CAPI Machine controller).
		if isKubeVirtInfra(cluster) && numMachines > 0 {
			for _, m := range machines {
				if m.Spec.Bootstrap.DataSecretName == nil {
					log.V(logutil.DebugLevel).Info("KubeVirt: waiting for existing machine bootstrap data before creating next machine",
						"machine", m.Name)
					return nil
				}
			}
		}

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

	// TODO: Stale VM recovery is planned for a future PR. The recovery.go module
	// handles VMs that booted but failed Ignition config drive detection.
	// It is disabled during installation to prevent disrupting active installations
	// when kubelet temporarily stops posting status.

	log.V(logutil.DebugLevel).Info("updating replica status", "oacp", oacp, "machines", machines)

	r.updateReplicaStatus(ctx, oacp, machines)
	return nil
}

func (r *OpenshiftAssistedControlPlaneReconciler) scaleUpControlPlane(ctx context.Context, oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane, cluster *clusterv1.Cluster, failureDomain string) (*clusterv1.Machine, error) {
	apiServiceIP := kubevirt.GetAPIServiceClusterIP(ctx, r.Client, oacp.Name, oacp.Namespace)
	if isKubeVirtInfra(cluster) && apiServiceIP == "" {
		ctrl.LoggerFrom(ctx).Info("waiting for API service to be created before scaling up")
		return nil, fmt.Errorf("API service not yet available for KubeVirt cluster, will retry")
	}
	name := names.SimpleNameGenerator.GenerateName(oacp.Name + "-")
	machine, infraObj, err := r.generateMachine(ctx, oacp, name, cluster, failureDomain)
	if err != nil {
		return nil, err
	}
	infraCoreDNSIP := ""
	if isKubeVirtInfra(cluster) {
		infraCoreDNSIP = kubevirt.GetInfraCoreDNSIP(ctx, r.Client)
	}
	bootstrapConfig := r.generateOpenshiftAssistedConfig(oacp, cluster.Name, name, apiServiceIP, infraCoreDNSIP, cluster)
	_ = controllerutil.SetOwnerReference(oacp, bootstrapConfig, r.Scheme)
	if err := r.Create(ctx, bootstrapConfig); err != nil {
		setConditionFalse(oacp, controlplanev1alpha3.MachinesCreatedCondition, controlplanev1alpha3.BootstrapTemplateCloningFailedReason,
			"error creating bootstrap config: %v", err)
		if deleteInfraErr := r.Delete(ctx, infraObj); deleteInfraErr != nil {
			err = errors.Join(err, deleteInfraErr)
		}
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
		if deleteInfraErr := r.Delete(ctx, infraObj); deleteInfraErr != nil {
			err = errors.Join(err, deleteInfraErr)
		}
		return nil, err
	}

	// The CAPI CRD on this cluster uses v1beta1 ObjectReference schema (with apiVersion)
	// rather than v1beta2 ContractVersionedObjectReference (with apiGroup). The Go types
	// set APIGroup, but the CRD strips it since that field doesn't exist in its schema.
	// Patch the Machine to set apiVersion on both refs so the CAPI controller can resolve them.
	patchData := fmt.Sprintf(`{"spec":{"bootstrap":{"configRef":{"apiVersion":"%s"}},"infrastructureRef":{"apiVersion":"%s"}}}`,
		bootstrapv1alpha2.GroupVersion.String(),
		infraObj.GroupVersionKind().GroupVersion().String(),
	)
	if patchErr := r.Patch(ctx, machine, client.RawPatch(types.MergePatchType, []byte(patchData))); patchErr != nil {
		ctrl.LoggerFrom(ctx).Error(patchErr, "failed to patch Machine apiVersion refs, cleaning up", "machine", machine.Name)
		if deleteBootstrapErr := r.Delete(ctx, bootstrapConfig); deleteBootstrapErr != nil {
			patchErr = errors.Join(patchErr, deleteBootstrapErr)
		}
		if deleteInfraErr := r.Delete(ctx, infraObj); deleteInfraErr != nil {
			patchErr = errors.Join(patchErr, deleteInfraErr)
		}
		if deleteMachineErr := r.Delete(ctx, machine); deleteMachineErr != nil {
			patchErr = errors.Join(patchErr, deleteMachineErr)
		}
		return nil, patchErr
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

func (r *OpenshiftAssistedControlPlaneReconciler) generateMachine(ctx context.Context, oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane, name string, cluster *clusterv1.Cluster, failureDomain string) (*clusterv1.Machine, *unstructured.Unstructured, error) {
	machine := r.computeDesiredMachine(oacp, name, cluster, failureDomain)
	infraObj, infraRef, err := r.createInfraMachine(ctx, oacp, machine.Name, cluster.Name, cluster)
	if err != nil {
		return nil, nil, err
	}
	machine.Spec.InfrastructureRef = infraRef
	return machine, infraObj, nil
}

func (r *OpenshiftAssistedControlPlaneReconciler) createInfraMachine(ctx context.Context, oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane, machineName, clusterName string, cluster *clusterv1.Cluster) (*unstructured.Unstructured, clusterv1.ContractVersionedObjectReference, error) {
	log := ctrl.LoggerFrom(ctx)

	// Since the cloned resource should eventually have a controller ref for the Machine, we create an
	// OwnerReference here without the Controller field set
	infraCloneOwner := &metav1.OwnerReference{
		APIVersion: controlplanev1alpha3.GroupVersion.String(),
		Kind:       openshiftAssistedControlPlaneKind,
		Name:       oacp.Name,
		UID:        oacp.UID,
	}

	// Fetch the infrastructure template using contract-based API version resolution
	// instead of hardcoding an API version
	template, err := external.GetObjectFromContractVersionedRef(ctx, r.Client, oacp.Spec.MachineTemplate.InfrastructureRef, oacp.Namespace)
	if err != nil {
		setConditionFalse(oacp, controlplanev1alpha3.MachinesCreatedCondition, controlplanev1alpha3.InfrastructureTemplateCloningFailedReason,
			"error fetching infrastructure template: %v", err)
		return nil, clusterv1.ContractVersionedObjectReference{}, err
	}

	templateRef := &corev1.ObjectReference{
		APIVersion: template.GetAPIVersion(),
		Kind:       template.GetKind(),
		Name:       template.GetName(),
		Namespace:  template.GetNamespace(),
	}

	infraMachine, err := external.GenerateTemplate(&external.GenerateTemplateInput{
		Template:    template,
		TemplateRef: templateRef,
		Namespace:   oacp.Namespace,
		Name:        machineName,
		OwnerRef:    infraCloneOwner,
		ClusterName: clusterName,
		Labels:      util.ControlPlaneMachineLabelsForCluster(oacp, clusterName),
		Annotations: oacp.Spec.MachineTemplate.ObjectMeta.Annotations,
	})
	if err != nil {
		setConditionFalse(oacp, controlplanev1alpha3.MachinesCreatedCondition, controlplanev1alpha3.InfrastructureTemplateCloningFailedReason,
			"error generating infrastructure clone: %v", err)
		return nil, clusterv1.ContractVersionedObjectReference{}, err
	}

	// For KubeVirt platform, enforce correct networking configuration on the cloned
	// infrastructure machine before creation. This ensures bridge binding, the OVN DHCP
	// annotation, and correct eviction strategy regardless of the template's settings.
	if isKubeVirtInfra(cluster) {
		if err := kubevirt.EnforceNetworkingRequirements(infraMachine, oacp); err != nil {
			log.Error(err, "failed to enforce networking requirements on infrastructure machine")
			setConditionFalse(oacp, controlplanev1alpha3.MachinesCreatedCondition, controlplanev1alpha3.InfrastructureTemplateCloningFailedReason,
				"error enforcing networking requirements: %v", err)
			return nil, clusterv1.ContractVersionedObjectReference{}, err
		}
		log.V(logutil.InfoLevel).Info("enforced bridge networking requirements on infrastructure machine", "machine", machineName)
	}

	if err := r.Create(ctx, infraMachine); err != nil {
		setConditionFalse(oacp, controlplanev1alpha3.MachinesCreatedCondition, controlplanev1alpha3.InfrastructureTemplateCloningFailedReason,
			"error creating infrastructure clone: %v", err)
		return nil, clusterv1.ContractVersionedObjectReference{}, err
	}

	return infraMachine, clusterv1.ContractVersionedObjectReference{
		APIGroup: infraMachine.GroupVersionKind().Group,
		Kind:     infraMachine.GetKind(),
		Name:     infraMachine.GetName(),
	}, nil
}


func (r *OpenshiftAssistedControlPlaneReconciler) generateOpenshiftAssistedConfig(oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane, clusterName string, name string, apiServiceIP string, infraCoreDNSIP string, cluster *clusterv1.Cluster) *bootstrapv1alpha2.OpenshiftAssistedConfig {
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
	// Skip the conversion data annotation to avoid corrupting the bootstrap config's TypeMeta during conversion
	for k, v := range oacp.Annotations {
		if k == utilconversion.DataAnnotation {
			continue
		}
		if _, exists := annotations[k]; !exists {
			annotations[k] = v
		}
	}

	// For KubeVirt platform, auto-generate ignition overrides if not explicitly set by user.
	// This handles DNS resolution, NetworkManager config, IPv4 preference, and placeholder
	// manifests — all platform implementation details that users shouldn't need to specify.
	if isKubeVirtInfra(cluster) {
		sshKey := oacp.Spec.Config.SSHAuthorizedKey

		tenantClusterName := oacp.Spec.Config.ClusterName
		if tenantClusterName == "" {
			tenantClusterName = oacp.Name
		}

		// Only inject DNS/resolv.conf config for pod networking (no VIPs).
		// Bridge networking VMs get DNS from DHCP and don't need the infra CoreDNS.
		discoveryAPIServiceIP := apiServiceIP
		if kubevirt.IsBridgeNetworking(oacp.Spec.Config.APIVIPs, oacp.Spec.Config.IngressVIPs) {
			discoveryAPIServiceIP = ""
		}

		infraCoreDNSAddr := infraCoreDNSIP
		if infraCoreDNSAddr == "" {
			infraCoreDNSAddr = "172.30.0.10"
		}

		if _, exists := annotations[bootstrapv1alpha2.DiscoveryIgnitionOverrideAnnotation]; !exists {
			if override, err := kubevirt.KubeVirtDiscoveryIgnitionOverride(sshKey, discoveryAPIServiceIP, tenantClusterName, oacp.Spec.Config.BaseDomain, infraCoreDNSAddr); err == nil && override != "" {
				annotations[bootstrapv1alpha2.DiscoveryIgnitionOverrideAnnotation] = override
			}
		}

		if _, exists := annotations[bootstrapv1alpha2.IgnitionOverrideAnnotation]; !exists {
			if override, err := kubevirt.KubeVirtInstallIgnitionOverride(sshKey); err == nil && override != "" {
				annotations[bootstrapv1alpha2.IgnitionOverrideAnnotation] = override
			}
		}
	}

	spec := *oacp.Spec.OpenshiftAssistedConfigSpec.DeepCopy()

	// For KubeVirt platform, inherit PullSecretRef and SSHAuthorizedKey from Config
	// if not explicitly set in OpenshiftAssistedConfigSpec. Users typically set
	// spec.config.pullSecretRef (for ClusterDeployment) and expect it to also apply
	// to InfraEnv creation via the OAC.
	if isKubeVirtInfra(cluster) {
		if spec.PullSecretRef == nil && oacp.Spec.Config.PullSecretRef != nil {
			spec.PullSecretRef = oacp.Spec.Config.PullSecretRef.DeepCopy()
		}
		if spec.SSHAuthorizedKey == "" && oacp.Spec.Config.SSHAuthorizedKey != "" {
			spec.SSHAuthorizedKey = oacp.Spec.Config.SSHAuthorizedKey
		}
	}

	bootstrapConfig := &bootstrapv1alpha2.OpenshiftAssistedConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   oacp.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: spec,
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
