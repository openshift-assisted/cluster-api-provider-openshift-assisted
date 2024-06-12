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
	bootstrapv1alpha1 "github.com/openshift-assisted/cluster-api-agent/bootstrap/api/v1alpha1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// InfraEnvReconciler reconciles a InfraEnv object
type InfraEnvReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *InfraEnvReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aiv1beta1.InfraEnv{}).
		Complete(r)
}
func (r *InfraEnvReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	infraEnv := &aiv1beta1.InfraEnv{}
	if err := r.Client.Get(ctx, req.NamespacedName, infraEnv); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if infraEnv == nil {
		return ctrl.Result{}, nil
	}
	clusterName, ok := infraEnv.Labels[clusterv1.ClusterNameLabel]
	if !ok {
		return ctrl.Result{}, nil
	}

	// Check for ISO
	if infraEnv.Status.ISODownloadURL == "" {
		log.Info("InfraEnv corresponding has no image URL available.", "infra_env_name", infraEnv.Name)
		return ctrl.Result{}, nil
	}
	log.Info("InfraEnv corresponding has image URL available.", "infra_env_name", infraEnv.Name)

	// TODO: list all agentbootstrapconfig that ref this infraenv
	agentBootstrapConfigs := &bootstrapv1alpha1.AgentBootstrapConfigList{}
	if err := r.Client.List(ctx, agentBootstrapConfigs, client.MatchingLabels{clusterv1.ClusterNameLabel: clusterName}); err != nil {
		log.Info("failed to list agentbootstrapconfigs for infraenv", "namespace/name", infraEnv.Name)
		return ctrl.Result{}, err
	}

	log.Info("Found AgentBootstrapConfig for InfraEnv", "infra_env_name", infraEnv.Name, "agent_bootstrap_config_count", len(agentBootstrapConfigs.Items))

	for _, agentBootstrapConfig := range agentBootstrapConfigs.Items {
		if agentBootstrapConfig.Status.InfraEnvRef != nil && agentBootstrapConfig.Status.InfraEnvRef.Name != infraEnv.Name {
			log.Info("InfraEnvRef on agentbootstrap config doesn't match infraenv found", "agentBootstrap.InfraEnvRef", agentBootstrapConfig.Status.InfraEnvRef.Name, "infra env", infraEnv.Name)
			//
			continue
		}
		log.Info("Adding ISO URL to AgentBootstrapConfig", "ISO URL", infraEnv.Status.ISODownloadURL, "agent_bootstrap_config", agentBootstrapConfig.Name)
		// Add ISO to agentBootstrapConfig status
		agentBootstrapConfig.Status.ISODownloadURL = infraEnv.Status.ISODownloadURL

		if err := r.Client.Status().Update(ctx, &agentBootstrapConfig); err != nil {
			log.Error(err, "couldn't update agentbootstrapconfig with iso url", "infra env name", infraEnv.Name, "config", agentBootstrapConfig.Name)
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}
