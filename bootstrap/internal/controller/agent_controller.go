package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/openshift-assisted/cluster-api-agent/bootstrap/internal/ignition"
	logutil "github.com/openshift-assisted/cluster-api-agent/util/log"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	metal3 "github.com/metal3-io/cluster-api-provider-metal3/api/v1beta1"
	bootstrapv1alpha1 "github.com/openshift-assisted/cluster-api-agent/bootstrap/api/v1alpha1"
	"github.com/openshift-assisted/cluster-api-agent/util"
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
	if err := r.Get(ctx, req.NamespacedName, agent); err != nil {
		log.Error(err, "unable to fetch Agent")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	log.WithValues("agent", agent.Name, "agent_namespace", agent.Namespace)

	bmh, err := r.findBMH(ctx, agent)
	if err != nil {
		log.Error(err, "can't find BMH for agent")
		return ctrl.Result{}, err
	}

	machine, err := r.getMachineFromBMH(ctx, bmh)
	if err != nil {
		log.Error(err, "can't find machine for agent")
		return ctrl.Result{}, err
	}

	if machine.Spec.Bootstrap.ConfigRef == nil {
		log.V(logutil.TraceLevel).Info("agent doesn't belong to CAPI cluster")
		return ctrl.Result{}, nil
	}

	if err := r.ensureBootstrapConfigReference(ctx, machine, agent.Name); err != nil {
		log.Error(err, "failed to ensure Agent Bootstrap Config references this agent")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, r.setAgentFields(ctx, agent, bmh, machine)
}

func (r *AgentReconciler) setAgentFields(ctx context.Context, agent *aiv1beta1.Agent, bmh *metal3v1alpha1.BareMetalHost, machine *clusterv1.Machine) error {
	role := models.HostRoleWorker
	if _, ok := machine.Labels[clusterv1.MachineControlPlaneLabel]; ok {
		role = models.HostRoleMaster
	}

	ignitionConfigOverrides, err := getIgnitionConfig()
	if err != nil {
		return err
	}

	agent.Spec.NodeLabels = map[string]string{metal3ProviderIDLabelKey: getProviderID(bmh)}
	agent.Spec.Role = role
	agent.Spec.IgnitionConfigOverrides = ignitionConfigOverrides
	agent.Spec.Approved = true
	return r.Client.Update(ctx, agent)
}

func getIgnitionConfig() (string, error) {
	capiSuccessFile := ignition.CreateIgnitionFile("/run/cluster-api/bootstrap-success.complete",
		"root", "data:text/plain;charset=utf-8;base64,c3VjY2Vzcw==", 420, true)
	return ignition.GetIgnitionConfigOverrides(capiSuccessFile)
}

func (r *AgentReconciler) ensureBootstrapConfigReference(ctx context.Context, machine *clusterv1.Machine, agentName string) error {
	config := &bootstrapv1alpha1.OpenshiftAssistedConfig{}
	if err := r.Client.Get(ctx,
		client.ObjectKey{
			Name:      machine.Spec.Bootstrap.ConfigRef.Name,
			Namespace: machine.Spec.Bootstrap.ConfigRef.Namespace},
		config); err != nil {
		return err
	}

	if config.Status.AgentRef == nil {
		config.Status.AgentRef = &corev1.LocalObjectReference{Name: agentName}
		// Set this agent as a ref on the agent bootstrap config
		return r.Client.Status().Update(ctx, config)
	}
	return nil
}

func getProviderID(bmh *metal3v1alpha1.BareMetalHost) string {
	return string(bmh.GetUID())
}

func (r *AgentReconciler) getMachineFromBMH(
	ctx context.Context,
	bmh *metal3v1alpha1.BareMetalHost,
) (*clusterv1.Machine, error) {
	metal3Machine := &metal3.Metal3Machine{}
	if err := util.GetTypedOwner(ctx, r.Client, bmh, metal3Machine); err != nil {
		return nil, err
	}
	machine := &clusterv1.Machine{}
	if err := util.GetTypedOwner(ctx, r.Client, metal3Machine, machine); err != nil {
		return nil, err
	}
	return machine, nil
}

func (r *AgentReconciler) findBMH(
	ctx context.Context,
	agent *aiv1beta1.Agent,
) (*metal3v1alpha1.BareMetalHost, error) {
	if agent.Status.Inventory.Interfaces == nil {
		return nil, errors.New("agent doesn't have inventory yet")
	}

	bmhs := &metal3v1alpha1.BareMetalHostList{}
	if err := r.Client.List(ctx, bmhs); err != nil {
		return nil, err
	}

	for _, bmh := range bmhs.Items {
		for _, agentInterface := range agent.Status.Inventory.Interfaces {
			if agentInterface.MacAddress != "" &&
				strings.EqualFold(bmh.Spec.BootMACAddress, agentInterface.MacAddress) {
				return &bmh, nil
			}
		}
	}

	return nil, fmt.Errorf(
		"found %d BMHs, but none matched any of the MacAddresses from the agent's %d interfaces",
		len(bmhs.Items),
		len(agent.Status.Inventory.Interfaces),
	)
}
