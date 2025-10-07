package controller

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/bootstrap/internal/ignition"
	logutil "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util/log"

	bootstrapv1alpha1 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/bootstrap/api/v1alpha1"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/models"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	retryAfter               = 20 * time.Second
	metal3ProviderIDLabelKey = "metal3.io/uuid"
)

// AgentReconciler reconciles an Agent object
type AgentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *AgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aiv1beta1.Agent{}).
		Complete(r)
}

// Reconciles Agent resource
func (r *AgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	agent := &aiv1beta1.Agent{}
	if err := r.Client.Get(ctx, req.NamespacedName, agent); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	machine, err := r.getMachineFromAgent(ctx, agent)
	if err != nil {
		log.Error(err, "cannot find machine for agent", "agent", agent)
		return ctrl.Result{}, err
	}

	if machine.Spec.Bootstrap.ConfigRef == nil {
		log.V(logutil.DebugLevel).Info("agent doesn't belong to CAPI cluster", "agent", agent)
		return ctrl.Result{}, nil
	}

	config, err := r.ensureBootstrapConfigReference(ctx, machine, agent.Name)
	if err != nil {
		log.Error(err, "failed to ensure Agent Bootstrap Config references this agent")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, r.setAgentFields(ctx, agent, machine, config)
}

func (r *AgentReconciler) setAgentFields(ctx context.Context, agent *aiv1beta1.Agent, machine *clusterv1.Machine, config *bootstrapv1alpha1.OpenshiftAssistedConfig) error {
	logger := ctrl.LoggerFrom(ctx)
	role := models.HostRoleWorker
	if _, ok := machine.Labels[clusterv1.MachineControlPlaneLabel]; ok {
		role = models.HostRoleMaster
	}

	ignitionConfigOverrides, err := getIgnitionConfig(config)
	if err != nil {
		return err
	}

	approvable, err := r.canApproveAgent(ctx, agent)
	if err != nil {
		return err
	}

	// Check if any changes are needed
	if agent.Spec.Role == role &&
		agent.Spec.IgnitionConfigOverrides == ignitionConfigOverrides &&
		agent.Spec.Approved == approvable {
		return nil
	}
	agent.Spec.Role = role
	agent.Spec.IgnitionConfigOverrides = ignitionConfigOverrides
	agent.Spec.Approved = approvable
	installerArgs := make([]string, 0, len(config.Spec.KernelArguments))
	for _, karg := range config.Spec.KernelArguments {
		arg := fmt.Sprintf("--%s-karg", karg.Operation)
		installerArgs = append(installerArgs, arg, karg.Value)
	}
	jsonBytes, err := json.Marshal(installerArgs)
	if err != nil {
		logger.V(logutil.DebugLevel).Info("failed to marshal installer args", "error", err)
	} else {
		agent.Spec.InstallerArgs = string(jsonBytes)
	}
	return r.Client.Update(ctx, agent)
}

func (r *AgentReconciler) canApproveAgent(ctx context.Context, agent *aiv1beta1.Agent) (bool, error) {
	agentList := &aiv1beta1.AgentList{}
	if err := r.Client.List(ctx, agentList, client.MatchingLabels{aiv1beta1.InfraEnvNameLabel: agent.Labels[aiv1beta1.InfraEnvNameLabel]}); err != nil {
		return false, err
	}

	for _, existingAgent := range agentList.Items {
		if existingAgent.Name != agent.Name &&
			existingAgent.Spec.Approved {
			log := ctrl.LoggerFrom(ctx)
			log.V(logutil.DebugLevel).Info(
				"not approving agent: another agent is already approved with the same infraenv",
				"agent", agent.Name,
				"infraenv", agent.Labels[aiv1beta1.InfraEnvNameLabel])
			return false, nil
		}
	}

	return true, nil
}

func getIgnitionConfig(config *bootstrapv1alpha1.OpenshiftAssistedConfig) (string, error) {
	// get labels and set them as KUBELET_EXTRA_LABELS in ignition
	extraLabels := strings.Join(config.Spec.NodeRegistration.KubeletExtraLabels, ",")
	content := `#!/bin/bash
echo "CUSTOM_KUBELET_LABELS=` + extraLabels + `" | tee -a /etc/kubernetes/kubelet-env >/dev/null
`
	b64Content := base64.StdEncoding.EncodeToString([]byte(content))
	kubeletCustomLabels := ignition.CreateIgnitionFile("/usr/local/bin/kubelet_custom_labels",
		"root", "data:text/plain;charset=utf-8;base64,"+b64Content, 493, true)
	return ignition.GetIgnitionConfigOverrides(kubeletCustomLabels)
}

func (r *AgentReconciler) ensureBootstrapConfigReference(ctx context.Context, machine *clusterv1.Machine, agentName string) (*bootstrapv1alpha1.OpenshiftAssistedConfig, error) {
	config := &bootstrapv1alpha1.OpenshiftAssistedConfig{}
	if err := r.Client.Get(ctx,
		client.ObjectKey{
			Name:      machine.Spec.Bootstrap.ConfigRef.Name,
			Namespace: machine.Spec.Bootstrap.ConfigRef.Namespace},
		config); err != nil {
		return nil, err
	}
	if config.Status.AgentRef == nil {
		config.Status.AgentRef = &corev1.LocalObjectReference{Name: agentName}
		return config, r.Client.Status().Update(ctx, config)
	}
	return config, nil
}

func (r *AgentReconciler) getMachineFromAgent(ctx context.Context, agent *aiv1beta1.Agent) (*clusterv1.Machine, error) {
	infraEnvName, ok := agent.Labels[aiv1beta1.InfraEnvNameLabel]
	if !ok {
		return nil, fmt.Errorf("no %s label on Agent %s", aiv1beta1.InfraEnvNameLabel, agent.GetNamespace()+"/"+agent.GetName())
	}
	infraEnv := aiv1beta1.InfraEnv{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: infraEnvName, Namespace: agent.GetNamespace()}, &infraEnv); err != nil {
		return nil, err
	}
	machine := &clusterv1.Machine{}
	if err := util.GetTypedOwner(ctx, r.Client, &infraEnv, machine); err != nil {
		return nil, err
	}
	return machine, nil
}
