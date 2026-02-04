package controller

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/bootstrap/internal/ignition"
	logutil "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util/log"

	bootstrapv1alpha2 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/bootstrap/api/v1alpha2"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/models"

	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// validVarNamePattern matches valid shell variable names to prevent injection attacks
var validVarNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

const (
	retryAfter = 20 * time.Second
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
	if err := r.Get(ctx, req.NamespacedName, agent); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	machine, err := r.getMachineFromAgent(ctx, agent)
	if err != nil {
		log.Error(err, "cannot find machine for agent", "agent", agent)
		return ctrl.Result{}, err
	}

	if !machine.Spec.Bootstrap.ConfigRef.IsDefined() {
		log.V(logutil.DebugLevel).Info("agent doesn't belong to CAPI cluster", "agent", agent)
		return ctrl.Result{}, nil
	}

	config := &bootstrapv1alpha2.OpenshiftAssistedConfig{}
	if err := r.Get(ctx,
		client.ObjectKey{
			Name:      machine.Spec.Bootstrap.ConfigRef.Name,
			Namespace: machine.Namespace},
		config); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, r.setAgentFields(ctx, agent, machine, config)
}

func (r *AgentReconciler) setAgentFields(ctx context.Context, agent *aiv1beta1.Agent, machine *clusterv1.Machine, config *bootstrapv1alpha2.OpenshiftAssistedConfig) error {
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

	specModified := agent.Spec.Role != role ||
		agent.Spec.IgnitionConfigOverrides != ignitionConfigOverrides ||
		agent.Spec.Approved != approvable

	if !specModified {
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
		return r.Update(ctx, agent)
	}

	agent.Spec.InstallerArgs = string(jsonBytes)
	return r.Update(ctx, agent)
}

func (r *AgentReconciler) canApproveAgent(ctx context.Context, agent *aiv1beta1.Agent) (bool, error) {
	agentList := &aiv1beta1.AgentList{}
	if err := r.List(ctx, agentList, client.MatchingLabels{aiv1beta1.InfraEnvNameLabel: agent.Labels[aiv1beta1.InfraEnvNameLabel]}); err != nil {
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

func getIgnitionConfig(config *bootstrapv1alpha2.OpenshiftAssistedConfig) (string, error) {
	dynamic, static, err := parseLabels(config.Spec.NodeRegistration.KubeletExtraLabels)
	if err != nil {
		return "", fmt.Errorf("invalid kubelet extra labels: %w", err)
	}

	providerIDVarName, providerIDStatic, err := parseEnvVarRef(config.Spec.NodeRegistration.ProviderID)
	if err != nil {
		return "", fmt.Errorf("invalid providerID: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("#!/bin/bash\n")

	// Collect all dynamic variables that need to be resolved
	dynamicVars := make(map[string]struct{})
	for _, varName := range dynamic {
		dynamicVars[varName] = struct{}{}
	}
	if providerIDVarName != "" {
		dynamicVars[providerIDVarName] = struct{}{}
	}

	// Resolve each dynamic variable from metadata_env
	for varName := range dynamicVars {
		sb.WriteString(fmt.Sprintf(`%s=""
if [ -f /etc/metadata_env ]; then
    %s=$(/usr/bin/grep "^%s=" /etc/metadata_env | /usr/bin/cut -d'=' -f2-)
fi
`, varName, varName, varName))
	}

	// Build labels string: static labels as-is, dynamic labels use ${VAR}
	labelParts := make([]string, 0, len(static)+len(dynamic))
	for key, value := range static {
		labelParts = append(labelParts, key+"="+value)
	}
	for key, varName := range dynamic {
		labelParts = append(labelParts, key+"=${"+varName+"}")
	}

	sb.WriteString(fmt.Sprintf(`echo "CUSTOM_KUBELET_LABELS=%s" | tee -a /etc/kubernetes/kubelet-env >/dev/null
`, strings.Join(labelParts, ",")))

	// Write KUBELET_PROVIDERID if specified
	if providerIDVarName != "" {
		sb.WriteString(fmt.Sprintf(`echo "KUBELET_PROVIDERID=${%s}" | tee -a /etc/kubernetes/kubelet-env >/dev/null
`, providerIDVarName))
	} else if providerIDStatic != "" {
		sb.WriteString(fmt.Sprintf(`echo "KUBELET_PROVIDERID=%s" | tee -a /etc/kubernetes/kubelet-env >/dev/null
`, providerIDStatic))
	}

	b64Content := base64.StdEncoding.EncodeToString([]byte(sb.String()))
	kubeletCustomLabels := ignition.CreateIgnitionFile("/usr/local/bin/kubelet_custom_labels",
		"root", "data:text/plain;charset=utf-8;base64,"+b64Content, 493, true)

	opts := ignition.IgnitionOptions{
		NodeNameEnvVar: config.Spec.NodeRegistration.Name,
	}
	return ignition.GetIgnitionConfigOverrides(opts, kubeletCustomLabels)
}

// parseEnvVarRef parses a value that may be an environment variable reference.
// If the value starts with $, it's treated as a variable reference and the variable name is returned.
// Otherwise, the static value is returned.
// Returns (varName, staticValue, error) where only one of varName or staticValue is non-empty.
func parseEnvVarRef(value string) (varName, staticValue string, err error) {
	if value == "" {
		return "", "", nil
	}

	if !strings.HasPrefix(value, "$") {
		return "", value, nil
	}

	// Strip $, {, } to get the variable name
	varName = strings.TrimPrefix(value, "$")
	varName = strings.TrimPrefix(varName, "{")
	varName = strings.TrimSuffix(varName, "}")

	// Validate variable name to prevent shell injection
	if !validVarNamePattern.MatchString(varName) {
		return "", "", fmt.Errorf("invalid variable name %q: must contain only letters, digits, and underscores, and start with a letter or underscore", varName)
	}

	return varName, "", nil
}

// parseLabels splits labels into dynamic (variable) and static maps.
// If value starts with $, strip $, {, } to get the variable name.
// Returns an error if a variable name is invalid (must match ^[A-Za-z_][A-Za-z0-9_]*$).
func parseLabels(labels []string) (dynamic, static map[string]string, err error) {
	dynamic = make(map[string]string)
	static = make(map[string]string)

	for _, label := range labels {
		parts := strings.SplitN(label, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := parts[0], parts[1]

		varName, staticValue, err := parseEnvVarRef(value)
		if err != nil {
			return nil, nil, fmt.Errorf("in label %q: %w", label, err)
		}

		if varName != "" {
			dynamic[key] = varName
			continue
		}
		static[key] = staticValue
	}
	return dynamic, static, nil
}

func (r *AgentReconciler) getMachineFromAgent(ctx context.Context, agent *aiv1beta1.Agent) (*clusterv1.Machine, error) {
	infraEnvName, ok := agent.Labels[aiv1beta1.InfraEnvNameLabel]
	if !ok {
		return nil, fmt.Errorf("no %s label on Agent %s", aiv1beta1.InfraEnvNameLabel, agent.GetNamespace()+"/"+agent.GetName())
	}
	infraEnv := aiv1beta1.InfraEnv{}
	if err := r.Get(ctx, client.ObjectKey{Name: infraEnvName, Namespace: agent.GetNamespace()}, &infraEnv); err != nil {
		return nil, err
	}

	machine := &clusterv1.Machine{}
	if err := util.GetTypedOwner(ctx, r.Client, &infraEnv, machine); err != nil {
		return nil, err
	}
	return machine, nil
}
