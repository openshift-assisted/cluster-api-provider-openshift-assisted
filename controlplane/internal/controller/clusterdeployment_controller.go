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
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/auth"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/imageregistry"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/kubevirt"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/release"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/pkg/containers"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util"
	logutil "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util/log"

	configv1 "github.com/openshift/api/config/v1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	aimodels "github.com/openshift/assisted-service/models"
	hivev1 "github.com/openshift/hive/apis/hive/v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	InstallConfigOverrides             = aiv1beta1.Group + "/install-config-overrides"
	defaultBaremetalBaselineCapability = "None"
	defaultBaselineCapability          = "vCurrent"
)

var (
	defaultBaremetalAdditionalCapabilities = []configv1.ClusterVersionCapability{"baremetal", "Console", "Insights", "OperatorLifecycleManager", "Ingress", "marketplace", "NodeTuning", "DeploymentConfig"}
)

// ClusterDeploymentReconciler reconciles a ClusterDeployment object
type ClusterDeploymentReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	RemoteImage containers.RemoteImage
	APIReader   client.Reader
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Only watch ClusterDeployments that have the CAPI cluster label
	clusterLabelPredicate, err := predicate.LabelSelectorPredicate(metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      clusterv1.ClusterNameLabel,
				Operator: metav1.LabelSelectorOpExists,
			},
		},
	})
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&hivev1.ClusterDeployment{}).
		WithEventFilter(clusterLabelPredicate).
		Watches(&controlplanev1alpha3.OpenshiftAssistedControlPlane{}, &handler.EnqueueRequestForObject{}).
		Watches(&clusterv1.MachineDeployment{}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}

// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=openshiftassistedcontrolplanes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinedeployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=agentclusterinstalls,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterimagesets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agentserviceconfigs,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=infraenvs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cdi.kubevirt.io,resources=datavolumes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes/custom-host,verbs=create;update;patch
// +kubebuilder:rbac:groups=operator.openshift.io,resources=ingresscontrollers,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=operator.openshift.io,resources=dnses,verbs=get;list;watch;update;patch

func (r *ClusterDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	clusterDeployment := &hivev1.ClusterDeployment{}
	if err := r.Get(ctx, req.NamespacedName, clusterDeployment); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	log.WithValues("cluster_deployment", clusterDeployment.Name, "cluster_deployment_namespace", clusterDeployment.Namespace)
	log.V(logutil.DebugLevel).Info("reconciling ClusterDeployment")

	acp := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: clusterDeployment.Namespace, Name: clusterDeployment.Name}, acp); err != nil {
		log.V(logutil.DebugLevel).Info("OpenshiftAssistedControlPlane not found for ClusterDeployment", "clusterDeployment", clusterDeployment.Name)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	log.WithValues("openshiftassisted_control_plane", acp.Name, "openshiftassisted_control_plane_namespace", acp.Namespace)

	arch, err := getArchitectureFromBootstrapConfigs(ctx, r.Client, acp)
	if err != nil {
		return ctrl.Result{}, err
	}

	releaseImage := getReleaseImage(*acp, arch)

	pullSecret, err := auth.GetPullSecret(r.Client, ctx, acp)
	if err != nil {
		log.Error(err, "failed to get pull secret for digest resolution", "releaseImage", releaseImage)
		return ctrl.Result{}, fmt.Errorf("failed to get pull secret for digest resolution: %w", err)
	}

	releaseImageWithDigest, err := release.GetReleaseImageWithDigest(releaseImage, pullSecret, r.RemoteImage)
	if err != nil {
		log.Error(err, "failed to resolve release image digest", "releaseImage", releaseImage)
		return ctrl.Result{}, fmt.Errorf("failed to resolve release image digest for %s: %w", releaseImage, err)
	}

	log.V(logutil.InfoLevel).Info("resolved release image digest", "tagBasedImage", releaseImage, "digestBasedImage", releaseImageWithDigest)

	if err = ensureClusterImageSet(ctx, r.Client, clusterDeployment.Name, releaseImageWithDigest); err != nil {
		log.Error(err, "failed creating ClusterImageSet")
		return ctrl.Result{}, err
	}

	cluster, err := capiutil.GetOwnerCluster(ctx, r.Client, acp.ObjectMeta)
	if err != nil {
		log.Error(err, "failed to retrieve owner Cluster")
		return ctrl.Result{}, err
	}
	if cluster == nil {
		log.V(logutil.DebugLevel).Info("owner Cluster not set yet, requeuing")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	if acp.Spec.Config.ImageRegistryRef != nil {
		if err := r.createImageRegistry(ctx, acp.Spec.Config.ImageRegistryRef.Name, acp.Namespace); err != nil {
			log.Error(err, "failed to create image registry config manifest")
			return ctrl.Result{}, err
		}
	}

	if isKubeVirtInfra(cluster) {
		result, err := r.reconcileKubeVirtInfra(ctx, acp, clusterDeployment, releaseImageWithDigest)
		if err != nil || !result.IsZero() {
			return result, err
		}
	}

	if err := r.ensureAgentClusterInstall(ctx, clusterDeployment, *acp, cluster); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.updateClusterDeploymentRef(ctx, clusterDeployment); err != nil {
		return ctrl.Result{}, err
	}

	// Requeue periodically during installation to update the DNS proxy apps zone
	// with virt-launcher pod IPs once VMs are running.
	if isKubeVirtInfra(cluster) &&
		kubevirt.IsPodNetworking(acp.Spec.Config.APIVIPs, acp.Spec.Config.IngressVIPs) &&
		!conditions.IsTrue(acp, string(controlplanev1alpha3.KubeconfigAvailableCondition)) {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

// reconcileKubeVirtInfra handles all KubeVirt-specific infrastructure setup:
// OS image, golden PVC, external access services/routes, DNS proxy, manifests,
// infra credentials, and MCS proxy.
func (r *ClusterDeploymentReconciler) reconcileKubeVirtInfra(
	ctx context.Context,
	acp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	clusterDeployment *hivev1.ClusterDeployment,
	releaseImageWithDigest string,
) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	infraNS := acp.Namespace

	arch, err := getArchitectureFromBootstrapConfigs(ctx, r.Client, acp)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := kubevirt.EnsureOSImageInAgentServiceConfig(ctx, r.Client, acp.Spec.DistributionVersion, arch); err != nil {
		log.Error(err, "failed to ensure OS image in AgentServiceConfig")
		return ctrl.Result{}, err
	}

	infraResult, err := kubevirt.GetInfraClusterClient(ctx, r.Client, acp, r.Scheme)
	if err != nil {
		log.Error(err, "failed to get infra cluster client")
		return ctrl.Result{}, err
	}
	infraNamespace := acp.Namespace
	if infraResult.IsRemote {
		infraNamespace = infraResult.Namespace
	}

	if result, err := r.ensureGoldenPVC(ctx, acp, infraResult, infraNamespace, releaseImageWithDigest); err != nil || !result.IsZero() {
		return result, err
	}

	serviceIPs, err := kubevirt.EnsureExternalAccessServices(ctx, infraResult.Client, acp, clusterDeployment.Spec.ClusterName, infraNamespace)
	if err != nil {
		log.Error(err, "failed to create external access services")
		return ctrl.Result{}, err
	}

	if err := kubevirt.EnsureIngressControllerWildcardPolicy(ctx, infraResult.Client); err != nil {
		log.V(1).Info("could not ensure IngressController wildcard policy (may require cluster-admin, assuming pre-configured)", "error", err)
	}
	if err := kubevirt.EnsureExternalRoutes(ctx, infraResult.Client, acp, clusterDeployment.Spec.ClusterName, infraNamespace); err != nil {
		log.Error(err, "failed to ensure external routes")
		return ctrl.Result{}, err
	}

	manifestDNSIPs := r.reconcileDNSProxy(ctx, acp, infraResult, clusterDeployment, serviceIPs, infraNamespace)

	pullSecretData := []byte{}
	if acp.Spec.Config.PullSecretRef != nil {
		pullSecret := &corev1.Secret{}
		if err := r.Get(ctx, client.ObjectKey{
			Name:      acp.Spec.Config.PullSecretRef.Name,
			Namespace: acp.Namespace,
		}, pullSecret); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to read pull secret %s: %w", acp.Spec.Config.PullSecretRef.Name, err)
		}
		pullSecretData = pullSecret.Data[".dockerconfigjson"]
	}
	oseCliImage, err := kubevirt.ResolveImageFromPayload(releaseImageWithDigest, pullSecretData, "cli", &containers.RemoteImageRepository{})
	if err != nil {
		log.Error(err, "failed to resolve CLI image from release payload")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	if oseCliImage == "" {
		log.V(1).Info("waiting for CLI image resolution from release payload")
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}

	infraCoreDNSIP := kubevirt.GetInfraCoreDNSIP(ctx, infraResult.Client)

	if _, err := kubevirt.EnsureKubeVirtManifests(ctx, r.Client, acp, infraNS, manifestDNSIPs, oseCliImage, releaseImageWithDigest, pullSecretData, &containers.RemoteImageRepository{}, infraCoreDNSIP); err != nil {
		log.Error(err, "failed to create KubeVirt platform manifests")
		return ctrl.Result{}, err
	}
	if err := kubevirt.EnsureInfraCredentialsManifests(ctx, r.Client, acp); err != nil {
		log.Error(err, "failed to create KubeVirt infra credentials manifests")
		return ctrl.Result{}, err
	}

	if kubevirt.IsPodNetworking(acp.Spec.Config.APIVIPs, acp.Spec.Config.IngressVIPs) {
		if _, err := kubevirt.EnsureMCSProxy(ctx, infraResult.Client, acp, clusterDeployment.Spec.ClusterName, infraNamespace, ""); err != nil {
			log.Error(err, "failed to ensure MCS proxy")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *ClusterDeploymentReconciler) ensureGoldenPVC(
	ctx context.Context,
	acp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	infraResult *kubevirt.InfraClientResult,
	infraNamespace string,
	releaseImageWithDigest string,
) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	goldenPullSecretName := ""
	if acp.Spec.Config.PullSecretRef != nil {
		goldenPullSecretName = acp.Spec.Config.PullSecretRef.Name
	}

	if infraResult.IsRemote {
		if err := kubevirt.EnsurePullSecretOnInfra(ctx, r.Client, infraResult.Client, acp.Namespace, infraNamespace, goldenPullSecretName); err != nil {
			log.Error(err, "failed to ensure pull secret on infra cluster")
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
	}

	ready, err := kubevirt.EnsureRHCOSGoldenPVC(ctx, r.Client, infraResult.Client, acp, releaseImageWithDigest, goldenPullSecretName, infraNamespace, "")
	if err != nil {
		log.Error(err, "failed to ensure RHCOS golden PVC")
		return ctrl.Result{}, err
	}
	if !ready {
		log.V(1).Info("waiting for RHCOS golden PVC to be ready")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

// reconcileDNSProxy sets up DNS proxy for pod-networking topologies and returns
// the ServiceIPs to use for manifest generation. For bridge networking, returns
// the service IPs unchanged.
func (r *ClusterDeploymentReconciler) reconcileDNSProxy(
	ctx context.Context,
	acp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	infraResult *kubevirt.InfraClientResult,
	clusterDeployment *hivev1.ClusterDeployment,
	serviceIPs *kubevirt.ServiceIPs,
	infraNamespace string,
) *kubevirt.ServiceIPs {
	log := ctrl.LoggerFrom(ctx)
	needsDNSProxy := kubevirt.IsPodNetworking(acp.Spec.Config.APIVIPs, acp.Spec.Config.IngressVIPs)
	if !needsDNSProxy {
		log.Info("VIPs configured, skipping pod-networking setup (bridge networking)")
		return serviceIPs
	}

	kubeconfigAvailable := conditions.IsTrue(acp, string(controlplanev1alpha3.KubeconfigAvailableCondition))
	manifestDNSIPs := serviceIPs

	if !kubeconfigAvailable {
		bootstrapIP, bErr := kubevirt.GetBootstrapPodIP(ctx, infraResult.Client, clusterDeployment.Spec.ClusterName, infraNamespace)
		if bErr != nil {
			log.V(1).Info("could not resolve bootstrap pod IP, falling back to service ClusterIP", "error", bErr)
		} else if bootstrapIP != "" {
			log.Info("using bootstrap pod IP for DNS resolution during installation", "bootstrapIP", bootstrapIP)
			manifestDNSIPs = &kubevirt.ServiceIPs{APIClusterIP: bootstrapIP}
			if serviceIPs != nil {
				manifestDNSIPs.IngressClusterIP = serviceIPs.IngressClusterIP
			}
		}
	} else {
		log.V(1).Info("kubeconfig available, using service ClusterIP for DNS")
	}

	routerIPs, _ := kubevirt.GetRouterNodeIPs(ctx, infraResult.Client, clusterDeployment.Spec.ClusterName, infraNamespace)

	apiIP := ""
	if manifestDNSIPs != nil {
		apiIP = manifestDNSIPs.APIClusterIP
	}
	dnsConfig := &kubevirt.DNSProxyConfig{
		APIIP:      apiIP,
		IngressIPs: routerIPs,
	}
	if err := kubevirt.EnsureDNSProxy(ctx, infraResult.Client, acp, dnsConfig, infraNamespace); err != nil {
		log.V(1).Info("could not ensure DNS proxy (best-effort)", "error", err)
	}

	dnsProxyIP := kubevirt.GetDNSProxyClusterIP(ctx, infraResult.Client, infraNamespace)
	if dnsProxyIP != "" {
		if err := kubevirt.EnsureDNSForwardingRule(ctx, r.Client, clusterDeployment.Spec.ClusterName, acp.Spec.Config.BaseDomain, dnsProxyIP, infraNamespace); err != nil {
			log.V(1).Info("could not ensure DNS forwarding rule (best-effort)", "error", err)
		}
	}

	if len(routerIPs) == 0 && !kubeconfigAvailable {
		log.V(1).Info("DNS proxy apps zone has no ingress IPs yet, will update on next reconcile")
	}

	return manifestDNSIPs
}

func (r *ClusterDeploymentReconciler) ensureAgentClusterInstall(
	ctx context.Context,
	clusterDeployment *hivev1.ClusterDeployment,
	oacp controlplanev1alpha3.OpenshiftAssistedControlPlane,
	cluster *clusterv1.Cluster,
) error {
	log := ctrl.LoggerFrom(ctx)

	workerNodes := r.getWorkerNodesCount(ctx, cluster)
	clusterNetwork, serviceNetwork := getClusterNetworks(cluster)
	additionalManifests := getClusterAdditionalManifestRefs(oacp, cluster)

	aci := &hiveext.AgentClusterInstall{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterDeployment.Name,
			Namespace: clusterDeployment.Namespace,
		},
	}
	mutate := func() error {
		aci.Labels = util.ControlPlaneMachineLabelsForCluster(
			&oacp,
			clusterDeployment.Labels[clusterv1.ClusterNameLabel],
		)
		aci.Labels[hiveext.ClusterConsumerLabel] = openshiftAssistedControlPlaneKind

		if err := controllerutil.SetOwnerReference(&oacp, aci, r.Scheme); err != nil {
			log.V(logutil.WarningLevel).Info("failed to set owner reference on AgentClusterInstall", "error", err.Error())
		}

		aci.Spec.ClusterDeploymentRef = corev1.LocalObjectReference{Name: clusterDeployment.Name}
		aci.Spec.PlatformType = getAgentClusterInstallPlatformType(oacp, cluster)
		if aci.Spec.PlatformType == hiveext.ExternalPlatformType {
			aci.Spec.ExternalPlatformSpec = getExternalPlatformSpec(oacp, cluster)
		} else {
			aci.Spec.ExternalPlatformSpec = nil
		}
		aci.Spec.ProvisionRequirements = hiveext.ProvisionRequirements{
			ControlPlaneAgents: int(oacp.Spec.Replicas),
			WorkerAgents:       workerNodes,
		}
		aci.Spec.DiskEncryption = oacp.Spec.Config.DiskEncryption
		aci.Spec.MastersSchedulable = oacp.Spec.Config.MastersSchedulable
		aci.Spec.Proxy = oacp.Spec.Config.Proxy
		aci.Spec.SSHPublicKey = oacp.Spec.Config.SSHAuthorizedKey
		aci.Spec.ImageSetRef = &hivev1.ClusterImageSetReference{Name: clusterDeployment.Name}
		aci.Spec.Networking.ClusterNetwork = clusterNetwork
		aci.Spec.Networking.ServiceNetwork = serviceNetwork
		aci.Spec.Networking.NetworkType = oacp.Spec.Config.NetworkType
		// ManifestsConfigMapRefs is immutable after the assisted-service starts installation.
		// Keep updating until we detect a non-empty installation state.
		installNotStarted := aci.Status.DebugInfo.State == "" ||
			aci.Status.DebugInfo.State == aimodels.ClusterStatusInsufficient ||
			aci.Status.DebugInfo.State == aimodels.ClusterStatusReady ||
			aci.Status.DebugInfo.State == aimodels.ClusterStatusPendingForInput
		if installNotStarted {
			aci.Spec.ManifestsConfigMapRefs = additionalManifests
		}

		if kubevirt.IsBridgeNetworking(oacp.Spec.Config.APIVIPs, oacp.Spec.Config.IngressVIPs) {
			aci.Spec.APIVIPs = oacp.Spec.Config.APIVIPs
			aci.Spec.IngressVIPs = oacp.Spec.Config.IngressVIPs
		}
		installConfigOverride, err := getInstallConfigOverride(&oacp, aci)
		if err != nil {
			return err
		}
		if installConfigOverride != "" {
			if aci.Annotations == nil {
				aci.Annotations = make(map[string]string)
			}
			aci.Annotations[InstallConfigOverrides] = installConfigOverride
		}

		// Set ignitionEndpoint for KubeVirt platform to enable day-2 worker addition.
		// OVN-Kubernetes blocks port 22624 cluster-wide, so workers use a proxy service.
		// Defer until AdminPasswordSecretRef is set on the ACI metadata — this confirms
		// assisted-service completed its post-install metadata flow. Setting it earlier
		// triggers a bug where assisted-service's parseIgnitionEndpoint sets CaCertificate=""
		// for HTTP endpoints, which fails validation and blocks the entire reconcile loop
		// (including admin password secret creation).
		if isKubeVirtInfra(cluster) &&
			aci.Spec.ClusterMetadata != nil && aci.Spec.ClusterMetadata.AdminPasswordSecretRef != nil {
			mcsProxySvcName := clusterDeployment.Labels[clusterv1.ClusterNameLabel] + "-" + kubevirt.MCSProxyServiceName
			ignitionURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d/config/worker",
				mcsProxySvcName, clusterDeployment.Namespace, kubevirt.MCSProxyPort)
			aci.Spec.IgnitionEndpoint = &hiveext.IgnitionEndpoint{Url: ignitionURL}
		}

		return nil
	}

	if _, err := controllerutil.CreateOrPatch(ctx, r.Client, aci, mutate); err != nil {
		log.Error(err, "failed to create or update AgentClusterInstall")
		return err
	}

	return nil
}

func (r *ClusterDeploymentReconciler) getWorkerNodesCount(ctx context.Context, cluster *clusterv1.Cluster) int {
	log := ctrl.LoggerFrom(ctx)
	count := 0

	mdList := clusterv1.MachineDeploymentList{}
	if err := r.List(ctx, &mdList, client.MatchingLabels{clusterv1.ClusterNameLabel: cluster.Name}); err != nil {
		log.Error(err, "failed to list MachineDeployments", "cluster", cluster.Name)
		return count
	}

	for _, md := range mdList.Items {
		count += int(*md.Spec.Replicas)
	}
	return count
}

// Returns release image from OpenshiftAssistedControlPlane. It will compute it starting from Spec.DistributionVersion and
// possibly cluster.x-k8s.io/release-image-repository-override annotation.
// Expected patterns:
// quay.io/openshift-release-dev/ocp-release:4.17.0-rc.2-x86_64
// quay.io/okd/scos-release:4.18.0-okd-scos.ec.1
// Can be overridden with annotation: cluster.x-k8s.io/release-image-repository-override=quay.io/myorg/myrepo
func getReleaseImage(oacp controlplanev1alpha3.OpenshiftAssistedControlPlane, architecture string) string {
	releaseImageRepository, ok := oacp.Annotations[release.ReleaseImageRepositoryOverrideAnnotation]
	if !ok {
		releaseImageRepository = ""
	}
	return release.GetReleaseImage(oacp.Spec.DistributionVersion, releaseImageRepository, architecture)
}


func (r *ClusterDeploymentReconciler) updateClusterDeploymentRef(
	ctx context.Context,
	cd *hivev1.ClusterDeployment,
) error {
	expectedRef := &hivev1.ClusterInstallLocalReference{
		Group:   hiveext.Group,
		Version: hiveext.Version,
		Kind:    "AgentClusterInstall",
		Name:    cd.Name,
	}

	// Skip update if ClusterInstallRef is already correctly set.
	// This avoids triggering Hive's immutability validation for spec.clusterMetadata
	// which rejects any update to ClusterDeployment after installation.
	if cd.Spec.ClusterInstallRef != nil &&
		cd.Spec.ClusterInstallRef.Group == expectedRef.Group &&
		cd.Spec.ClusterInstallRef.Version == expectedRef.Version &&
		cd.Spec.ClusterInstallRef.Kind == expectedRef.Kind &&
		cd.Spec.ClusterInstallRef.Name == expectedRef.Name {
		return nil
	}

	cd.Spec.ClusterInstallRef = expectedRef
	return r.Update(ctx, cd)
}

func ensureClusterImageSet(ctx context.Context, c client.Client, imageSetName string, releaseImage string) error {
	imageSet := &hivev1.ClusterImageSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: imageSetName,
		},
	}

	_, err := controllerutil.CreateOrPatch(ctx, c, imageSet, func() error {
		imageSet.Spec.ReleaseImage = releaseImage
		return nil
	})

	return err
}

func getClusterNetworks(cluster *clusterv1.Cluster) ([]hiveext.ClusterNetworkEntry, []string) {
	clusterNetwork := make([]hiveext.ClusterNetworkEntry, 0, len(cluster.Spec.ClusterNetwork.Pods.CIDRBlocks))
	for _, cidrBlock := range cluster.Spec.ClusterNetwork.Pods.CIDRBlocks {
		clusterNetwork = append(clusterNetwork, hiveext.ClusterNetworkEntry{CIDR: cidrBlock, HostPrefix: 23})
	}

	serviceNetwork := cluster.Spec.ClusterNetwork.Services.CIDRBlocks

	// If no networks are specified, use defaults for KubeVirt tenant clusters.
	// Pod CIDR must not overlap with the infra cluster's pod network (10.128.0.0/14)
	// because VMs get real IPs from the infra pod network.
	// Service CIDR MUST differ from the infra cluster's service CIDR (172.30.0.0/16).
	// Once the tenant's OVN-Kubernetes starts, it claims the tenant service CIDR and
	// routes all matching traffic internally. If the tenant uses the same CIDR as
	// the infra cluster, masters lose connectivity to infra services (CoreDNS at
	// 172.30.0.10, API Service) causing the installation to stall permanently.
	// Using 172.31.0.0/16 avoids this conflict while keeping resolv.conf pointing
	// at the infra CoreDNS (172.30.0.10) working throughout the installation.
	if isKubeVirtInfra(cluster) && len(clusterNetwork) == 0 {
		clusterNetwork = []hiveext.ClusterNetworkEntry{
			{CIDR: "10.132.0.0/14", HostPrefix: 23},
		}
	}
	if isKubeVirtInfra(cluster) && len(serviceNetwork) == 0 {
		serviceNetwork = []string{"172.31.0.0/16"}
	}

	return clusterNetwork, serviceNetwork
}

func getClusterAdditionalManifestRefs(acp controlplanev1alpha3.OpenshiftAssistedControlPlane, cluster *clusterv1.Cluster) []hiveext.ManifestsConfigMapReference {
	var additionalManifests []hiveext.ManifestsConfigMapReference
	if len(acp.Spec.Config.ManifestsConfigMapRefs) > 0 {
		additionalManifests = append(additionalManifests, acp.Spec.Config.ManifestsConfigMapRefs...)
	}

	if acp.Spec.Config.ImageRegistryRef != nil {
		additionalManifests = append(additionalManifests, hiveext.ManifestsConfigMapReference{Name: imageregistry.ImageConfigMapName})
	}

	if isKubeVirtInfra(cluster) {
		podNet := kubevirt.IsPodNetworking(acp.Spec.Config.APIVIPs, acp.Spec.Config.IngressVIPs)
		bridgeNet := kubevirt.IsBridgeNetworking(acp.Spec.Config.APIVIPs, acp.Spec.Config.IngressVIPs)

		// CCM provides LoadBalancer services — only relevant for pod-networking clusters
		// (no VIPs). Bridge-networking clusters use keepalived VIPs directly.
		if acp.Spec.Config.CloudControllerManager != nil && acp.Spec.Config.CloudControllerManager.Enabled && podNet {
			additionalManifests = append(additionalManifests, hiveext.ManifestsConfigMapReference{Name: kubevirt.CCMManifestsConfigMapName})
		}
		if acp.Spec.Config.CSIDriver != nil && acp.Spec.Config.CSIDriver.Enabled {
			additionalManifests = append(additionalManifests, hiveext.ManifestsConfigMapReference{Name: kubevirt.CSIManifestsConfigMapName})
		}
		if (acp.Spec.Config.CSIDriver != nil && acp.Spec.Config.CSIDriver.Enabled) ||
			(acp.Spec.Config.CloudControllerManager != nil && acp.Spec.Config.CloudControllerManager.Enabled) {
			additionalManifests = append(additionalManifests, hiveext.ManifestsConfigMapReference{Name: kubevirt.InfraCredentialsCMName})
		}
		if podNet {
			additionalManifests = append(additionalManifests, hiveext.ManifestsConfigMapReference{Name: kubevirt.TenantDNSFwdConfigName})
			additionalManifests = append(additionalManifests, hiveext.ManifestsConfigMapReference{Name: kubevirt.PodNetDNSFixManifestsConfigMapName})
			additionalManifests = append(additionalManifests, hiveext.ManifestsConfigMapReference{Name: kubevirt.NetworkMTUConfigMapName})
			additionalManifests = append(additionalManifests, hiveext.ManifestsConfigMapReference{Name: kubevirt.MCSManifestsConfigName})
		}
		// Resolv fix manifest - ensures DNS works on first boot for bridge networking (BareMetal platform)
		if bridgeNet {
			additionalManifests = append(additionalManifests, hiveext.ManifestsConfigMapReference{Name: kubevirt.ResolvFixManifestsConfigMapName})
		}
	}

	return additionalManifests
}

func (r *ClusterDeploymentReconciler) createImageRegistry(ctx context.Context, registryName, registryNamespace string) error {
	registryConfigmap := &corev1.ConfigMap{}
	if err := r.Get(ctx, client.ObjectKey{Name: registryName, Namespace: registryNamespace}, registryConfigmap); err != nil {
		return err
	}

	spokeImageRegistryData, err := imageregistry.GenerateImageRegistryData(registryConfigmap, registryNamespace)
	if err != nil {
		return err
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      imageregistry.ImageConfigMapName,
			Namespace: registryNamespace,
		},
	}
	_, err = controllerutil.CreateOrPatch(ctx, r.Client, cm, func() error {
		cm.Data = spokeImageRegistryData
		return nil
	})

	return err
}

type InstallConfigOverride struct {
	Capability configv1.ClusterVersionCapabilitiesSpec `json:"capabilities,omitempty"`
}

// getAgentClusterInstallPlatformType determines the PlatformType for the AgentClusterInstall
// based on the OACP platform configuration and VIP settings.
func getAgentClusterInstallPlatformType(oacp controlplanev1alpha3.OpenshiftAssistedControlPlane, cluster *clusterv1.Cluster) hiveext.PlatformType {
	if isKubeVirtInfra(cluster) {
		// For bridge-network clusters with VIPs, use BareMetal platform type.
		// The assisted-service doesn't support VIPs with External platform
		// (webhooks reject both UMN=true+VIPs and UMN=false+External).
		// BareMetal allows keepalived-managed VIPs with cluster-managed networking.
		if kubevirt.IsBridgeNetworking(oacp.Spec.Config.APIVIPs, oacp.Spec.Config.IngressVIPs) {
			return hiveext.PlatformType(configv1.BareMetalPlatformType)
		}
		// Pod-networking clusters without VIPs use None platform type.
		// Using None instead of External avoids kubelet's --cloud-provider=external flag,
		// which adds a node.cloudprovider.kubernetes.io/uninitialized taint that blocks
		// CNO scheduling during bootstrap (CNO doesn't tolerate it, creating a deadlock).
		return hiveext.PlatformType(configv1.NonePlatformType)
	}
	// BareMetal (default): use BareMetal if VIPs are configured, None otherwise.
	if kubevirt.IsBridgeNetworking(oacp.Spec.Config.APIVIPs, oacp.Spec.Config.IngressVIPs) {
		return hiveext.PlatformType(configv1.BareMetalPlatformType)
	}
	return hiveext.PlatformType(configv1.NonePlatformType)
}

// getExternalPlatformSpec returns the ExternalPlatformSpec for the ACI based on the OACP platform.
func getExternalPlatformSpec(oacp controlplanev1alpha3.OpenshiftAssistedControlPlane, cluster *clusterv1.Cluster) *hiveext.ExternalPlatformSpec {
	spec := &hiveext.ExternalPlatformSpec{}
	if isKubeVirtInfra(cluster) {
		spec.PlatformName = "KubeVirt"
		// CCM External mode only for pod-networking (no VIPs). Bridge-networking
		// clusters use BareMetal platform type, not External, so this doesn't apply.
		if oacp.Spec.Config.CloudControllerManager != nil &&
			oacp.Spec.Config.CloudControllerManager.Enabled &&
			kubevirt.IsPodNetworking(oacp.Spec.Config.APIVIPs, oacp.Spec.Config.IngressVIPs) {
			spec.CloudControllerManager = hiveext.CloudControllerManagerTypeExternal
		}
	}
	return spec
}

// isBaremetalPlatform checks if the AgentClusterInstall is configured for a baremetal platform.
// Returns true if the platform type is BareMetalPlatformType, false otherwise.
func isBaremetalPlatform(aci *hiveext.AgentClusterInstall) bool {
	return aci.Spec.PlatformType == hiveext.BareMetalPlatformType
}

// getInstallConfigOverride merges install config override from annotations with capabilities-based overrides.
// It returns the final merged install config override as a JSON string, or empty string if no overrides are needed.
// The function combines user-provided install config overrides from annotations with automatically
// generated capabilities configuration based on the cluster platform type.
func getInstallConfigOverride(oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane, aci *hiveext.AgentClusterInstall) (string, error) {
	installConfigOverrideStr := oacp.Annotations[controlplanev1alpha3.InstallConfigOverrideAnnotation]

	capabilitiesCfgOverride, err := getInstallConfigOverrideForCapabilities(oacp, aci)
	if err != nil {
		return "", err
	}
	// if no override and no capabilities, no need to merge install config override
	if installConfigOverrideStr == "" && capabilitiesCfgOverride == "" {
		return "", nil
	}
	if capabilitiesCfgOverride == "" {
		return installConfigOverrideStr, nil
	}
	if installConfigOverrideStr == "" {
		return capabilitiesCfgOverride, nil
	}

	installConfigOverrideStr, err = mergeJson(installConfigOverrideStr, capabilitiesCfgOverride)
	if err != nil {
		return "", err
	}
	return installConfigOverrideStr, nil
}

// getInstallConfigOverrideForCapabilities generates install config override JSON for OpenShift capabilities configuration.
// It automatically sets appropriate capabilities based on the platform type:
// - Baremetal platforms: Sets baseline to "None" and includes default baremetal capabilities
// - Non-baremetal platforms: Sets baseline to "vCurrent" and only includes user-specified capabilities
// Returns empty string if no capabilities configuration is needed (non-baremetal with empty capabilities).
func getInstallConfigOverrideForCapabilities(oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane, aci *hiveext.AgentClusterInstall) (string, error) {
	var installCfgOverride InstallConfigOverride
	if isCapabilitiesEmpty(oacp.Spec.Config.Capabilities) && !isBaremetalPlatform(aci) {
		return "", nil
	}
	baselineCapability, err := getBaselineCapability(oacp.Spec.Config.Capabilities.BaselineCapability, isBaremetalPlatform(aci))
	if err != nil {
		return "", err
	}
	installCfgOverride.Capability.BaselineCapabilitySet = configv1.ClusterVersionCapabilitySet(baselineCapability)

	additionalEnabledCapabilities := getAdditionalCapabilities(oacp.Spec.Config.Capabilities.AdditionalEnabledCapabilities, isBaremetalPlatform(aci))
	installCfgOverride.Capability.AdditionalEnabledCapabilities = additionalEnabledCapabilities

	installCfgOverrideBytes, err := json.Marshal(installCfgOverride)
	if err != nil {
		return "", err
	}
	installCfgOverrideStr := string(installCfgOverrideBytes)
	return installCfgOverrideStr, nil
}

// mergeJson performs a deep merge of two JSON strings. When both JSONs have the
// same key, the second JSON's value takes precedence for scalar values. For
// nested objects, they are recursively merged so that keys unique to each object
// are preserved.
func mergeJson(json1, json2 string) (string, error) {
	var base map[string]interface{}
	if err := json.Unmarshal([]byte(json1), &base); err != nil {
		return "", fmt.Errorf("failed to unmarshal json1: %w", err)
	}
	var overlay map[string]interface{}
	if err := json.Unmarshal([]byte(json2), &overlay); err != nil {
		return "", fmt.Errorf("failed to unmarshal json2: %w", err)
	}
	deepMerge(base, overlay)
	mergedJson, err := json.Marshal(base)
	if err != nil {
		return "", fmt.Errorf("failed to marshal merged json: %w", err)
	}
	return string(mergedJson), nil
}

// deepMerge recursively merges overlay into base. For map values present in
// both, it recurses. For all other types, overlay wins.
func deepMerge(base, overlay map[string]interface{}) {
	for key, overlayVal := range overlay {
		baseVal, exists := base[key]
		if !exists {
			base[key] = overlayVal
			continue
		}
		baseMap, baseIsMap := baseVal.(map[string]interface{})
		overlayMap, overlayIsMap := overlayVal.(map[string]interface{})
		if baseIsMap && overlayIsMap {
			deepMerge(baseMap, overlayMap)
		} else {
			base[key] = overlayVal
		}
	}
}

func getBaselineCapability(capability string, isBaremetalPlatform bool) (string, error) {
	baselineCapability := capability
	if baselineCapability == "None" || baselineCapability == "vCurrent" {
		return baselineCapability, nil
	}

	if baselineCapability == "" {
		baselineCapability = defaultBaselineCapability
		if isBaremetalPlatform {
			baselineCapability = defaultBaremetalBaselineCapability
		}
		return baselineCapability, nil
	}

	var baselineCapabilityRegexp = regexp.MustCompile(`v4\.[0-9]+`)
	if !baselineCapabilityRegexp.MatchString(baselineCapability) {
		return "",
			fmt.Errorf("invalid baseline capability set, must be one of: None, vCurrent, or v4.x. Got: [%s]", baselineCapability)
	}
	return baselineCapability, nil
}

func getAdditionalCapabilities(specifiedAdditionalCapabilities []string, isBaremetalPlatform bool) []configv1.ClusterVersionCapability {
	additionalCapabilitiesList := []configv1.ClusterVersionCapability{}
	if isBaremetalPlatform {
		additionalCapabilitiesList = append([]configv1.ClusterVersionCapability{}, defaultBaremetalAdditionalCapabilities...)
	}

	for _, capability := range specifiedAdditionalCapabilities {
		// Ignore MAPI for baremetal MNO clusters and ignore duplicates
		if (strings.EqualFold(capability, "MachineAPI") && isBaremetalPlatform) || slices.Contains(additionalCapabilitiesList, configv1.ClusterVersionCapability(capability)) {
			continue
		}
		additionalCapabilitiesList = append(additionalCapabilitiesList, configv1.ClusterVersionCapability(capability))
	}

	if len(additionalCapabilitiesList) < 1 {
		return nil
	}

	return additionalCapabilitiesList
}

func isCapabilitiesEmpty(capabilities controlplanev1alpha3.Capabilities) bool {
	return equality.Semantic.DeepEqual(capabilities, controlplanev1alpha3.Capabilities{})
}
