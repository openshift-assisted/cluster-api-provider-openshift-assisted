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
	logutil "github.com/openshift-assisted/cluster-api-agent/util/log"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"

	"github.com/pkg/errors"

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
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	log.WithValues("infra_env", infraEnv.Name, "infra_env_namespace", infraEnv.Namespace)

	if infraEnv.Status.ISODownloadURL == "" {
		log.V(logutil.TraceLevel).Info("image URL not available yet")
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, r.attachISOToAgentBootstrapConfigs(ctx, infraEnv)
}

func (r *InfraEnvReconciler) attachISOToAgentBootstrapConfigs(ctx context.Context, infraEnv *aiv1beta1.InfraEnv) error {
	clusterName, ok := infraEnv.Labels[clusterv1.ClusterNameLabel]
	if !ok {
		return nil
	}
	agentBootstrapConfigs := &bootstrapv1alpha1.AgentBootstrapConfigList{}
	if err := r.Client.List(ctx, agentBootstrapConfigs, client.MatchingLabels{clusterv1.ClusterNameLabel: clusterName}); err != nil {
		return errors.Wrap(err, "failed to list agent bootstrap configs")
	}

	for _, agentBootstrapConfig := range agentBootstrapConfigs.Items {
		// skip ABCs where InfraEnvRef is nil or is set to some other InfraEnv
		if agentBootstrapConfig.Status.InfraEnvRef == nil ||
			(agentBootstrapConfig.Status.InfraEnvRef != nil &&
				agentBootstrapConfig.Status.InfraEnvRef.Name != infraEnv.Name) {
			continue
		}

		// Add ISO to agentBootstrapConfig status
		// TODO: It's unconventional for a controller to set status on a
		// resource that it doesn't own. This looks to me like further
		// indication that the download URL should be in the ABC Spec.
		agentBootstrapConfig.Status.ISODownloadURL = infraEnv.Status.ISODownloadURL
		if err := r.Client.Status().Update(ctx, &agentBootstrapConfig); err != nil {
			return errors.Wrap(err, "failed to update agentbootstrapconfig")
		}
	}
	return nil
}
