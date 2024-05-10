package controller

import (
	"context"
	"errors"
	"strings"
	"time"

	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/openshift-assisted/cluster-api-agent/bootstrap/api/v1beta1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func (r *AgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	agent := &aiv1beta1.Agent{}
	err := r.Get(ctx, req.NamespacedName, agent)
	if err != nil {
		log.Error(err, "unable to fetch Agent")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// if we find an agent, we must ensure it is controlled by our provider
	clusterDeploymentKey := client.ObjectKey{
		Namespace: agent.Spec.ClusterDeploymentName.Namespace,
		Name:      agent.Spec.ClusterDeploymentName.Name,
	}
	clusterDeployment := &hivev1.ClusterDeployment{}
	if err := r.Client.Get(ctx, clusterDeploymentKey, clusterDeployment); err != nil {
		log.Error(err, "unable to fetch Agent")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	clusterName, ok := clusterDeployment.Labels[clusterv1.ClusterNameLabel]
	if !ok {
		log.Error(err, "clusterdeployment does not belong to a CAPI cluster")
		return ctrl.Result{}, nil
	}
	agentBootstrapConfigList := v1beta1.AgentBootstrapConfigList{}
	if err := r.Client.List(ctx, &agentBootstrapConfigList, client.MatchingLabels{clusterv1.ClusterNameLabel: clusterName}); err != nil {
		log.Error(err, "agentboostrapconfig not found for cluster", "cluster", clusterName)
		return ctrl.Result{}, err
	}
	if agent.Status.Inventory.Interfaces == nil {
		log.Info("agent doesn't have interfaces yet", "agent name", agent.Name)
		return ctrl.Result{RequeueAfter: 20 * time.Second}, nil
	}

	bmhs := &bmh_v1alpha1.BareMetalHostList{}
	if err := r.Client.List(ctx, bmhs); err != nil {
		log.Error(err, "can't get bmhs for agent", "cluster", clusterName)
		return ctrl.Result{}, err
	}
	var bmhUID string
	for _, bmh := range bmhs.Items {
		for _, agentInterface := range agent.Status.Inventory.Interfaces {
			if agentInterface.MacAddress != "" && strings.EqualFold(bmh.Spec.BootMACAddress, agentInterface.MacAddress) {
				bmhUID = string(bmh.GetUID())
			}
		}
	}
	if bmhUID == "" {
		log.Info("no bmh UID match for agent", "cluster", clusterName)
		return ctrl.Result{}, errors.New("no bmh UID match for agent")
	}

	agent.Spec.NodeLabels = map[string]string{"metal3.io/uuid": bmhUID}
	agent.Spec.Approved = true
	if err := r.Client.Update(ctx, agent); err != nil {
		log.Error(err, "couldn't update agent", "name", agent.Name, "namespace", agent.Namespace)
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}
