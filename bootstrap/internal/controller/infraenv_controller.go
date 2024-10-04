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
	"net/url"
	"os"
	"time"

	bootstrapv1alpha1 "github.com/openshift-assisted/cluster-api-agent/bootstrap/api/v1alpha1"
	logutil "github.com/openshift-assisted/cluster-api-agent/util/log"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"

	"github.com/pkg/errors"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	abcInfraEnvRefFieldName      = ".status.infraEnvRef.name"
	abcInfraEnvRefFieldNamespace = ".status.infraEnvRef.namespace"
)

type InfraEnvControllerConfig struct {
	// UseInternalImageURL when set to false means that we'll use the InfraEnv's iso download URL
	// as is. When set to true, it'll use the assisted-image-service's internal IP as part of the
	// download URL.
	UseInternalImageURL bool `envconfig:"USE_INTERNAL_IMAGE_URL" default:"false"`
	// ImageServiceName is the Service CR name for the assisted-image-service
	ImageServiceName string `envconfig:"IMAGE_SERVICE_NAME" default:"assisted-image-service"`
	// ImageServiceNamespace is the namespace that the Service CR for the assisted-image-service is in
	ImageServiceNamespace string `envconfig:"IMAGE_SERVICE_NAMESPACE"`
}

// InfraEnvReconciler reconciles a InfraEnv object
type InfraEnvReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Config InfraEnvControllerConfig
}

func filterRefName(rawObj client.Object) []string {
	abc, ok := rawObj.(*bootstrapv1alpha1.AgentBootstrapConfig)
	if !ok || abc.Status.InfraEnvRef == nil {
		return nil
	}
	return []string{abc.Status.InfraEnvRef.Name}
}

func filterRefNamespace(rawObj client.Object) []string {
	abc, ok := rawObj.(*bootstrapv1alpha1.AgentBootstrapConfig)
	if !ok || abc.Status.InfraEnvRef == nil {
		return nil
	}
	return []string{abc.Status.InfraEnvRef.Namespace}
}

// SetupWithManager sets up the controller with the Manager.
func (r *InfraEnvReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.TODO(), &bootstrapv1alpha1.AgentBootstrapConfig{}, abcInfraEnvRefFieldNamespace, filterRefNamespace); err != nil {
		return err
	}
	if err := mgr.GetFieldIndexer().IndexField(context.TODO(), &bootstrapv1alpha1.AgentBootstrapConfig{}, abcInfraEnvRefFieldName, filterRefName); err != nil {
		return err
	}
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
	log = log.WithValues("infra_env", infraEnv.Name, "infra_env_namespace", infraEnv.Namespace)

	if infraEnv.Status.ISODownloadURL == "" {
		log.V(logutil.TraceLevel).Info("image URL not available yet")
		// requeue so we'll make sure we'll have the ISO available.
		// NOTE: We should not need this, as InfraEnv should notify us when changing status
		return ctrl.Result{Requeue: true, RequeueAfter: retryAfter}, nil
	}
	err := r.attachISOToAgentBootstrapConfigs(ctx, infraEnv)
	if err != nil {
		return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 5}, err
	}
	return ctrl.Result{}, nil

}

func (r *InfraEnvReconciler) attachISOToAgentBootstrapConfigs(ctx context.Context, infraEnv *aiv1beta1.InfraEnv) error {
	log := ctrl.LoggerFrom(ctx)

	/*clusterName, ok := infraEnv.Labels[clusterv1.ClusterNameLabel]
	if !ok {
		return nil
	}
	*/
	agentBootstrapConfigs := &bootstrapv1alpha1.AgentBootstrapConfigList{}
	/*if err := r.Client.List(ctx, agentBootstrapConfigs, client.MatchingLabels{clusterv1.ClusterNameLabel: clusterName}); err != nil {
		return errors.Wrap(err, "failed to list agent bootstrap configs")
	}*/
	if err := r.Client.List(ctx, agentBootstrapConfigs, client.MatchingFields{abcInfraEnvRefFieldName: infraEnv.Name, abcInfraEnvRefFieldNamespace: infraEnv.Namespace}); err != nil {
		return errors.Wrap(err, "failed to list agent bootstrap configs")
	}

	log.V(logutil.TraceLevel).Info("listing agentBoostrapConfigs", "items found", len(agentBootstrapConfigs.Items), "LIST AgentBootstrapConfigs", agentBootstrapConfigs)
	downloadURL, err := r.getISOURL(ctx, infraEnv.Status.ISODownloadURL)
	log.V(logutil.TraceLevel).Info("ISO URL from INFRAENV", "ISO URL", downloadURL)

	if err != nil {
		log.V(logutil.TraceLevel).Info("error retrieving downloadURL from infraenv", "infraenv", infraEnv)
		return err
	}

	var errorIfSkipped error = nil
	for _, agentBootstrapConfig := range agentBootstrapConfigs.Items {
		if agentBootstrapConfig.Status.InfraEnvRef == nil {
			log.V(logutil.TraceLevel).Info("skipping agentbootstrapconfig because no infraenv ref", "abc", agentBootstrapConfig.Name)
			errorIfSkipped = errors.New("skipped an infraenv, need to retry") // infraenv would change and retrigger though
			continue
		}

		agentBootstrapConfig.Status.ISODownloadURL = downloadURL
		if err := r.Client.Status().Update(ctx, &agentBootstrapConfig); err != nil {
			return errors.Wrap(err, "failed to update agentbootstrapconfig")
		}
		log.V(logutil.TraceLevel).Info("setting infraenv ref to agentbootstrapconfig", "abc", agentBootstrapConfig.Name)
	}
	return errorIfSkipped
}

func (r *InfraEnvReconciler) getISOURL(ctx context.Context, originalURL string) (string, error) {
	if !r.Config.UseInternalImageURL {
		return originalURL, nil
	}

	if r.Config.ImageServiceNamespace == "" {
		// No user-provided override, assume the assisted-image-service is running in the same namespace as this pod
		ns, found := os.LookupEnv("NAMESPACE")
		if !found {
			return "",
				errors.New("unable to determine internal ip of assisted-image-service: no namespace provided for assisted-image-service service")
		}
		r.Config.ImageServiceNamespace = ns
	}

	svc := &corev1.Service{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: r.Config.ImageServiceName, Namespace: r.Config.ImageServiceNamespace}, svc); err != nil {
		return "", errors.Wrap(err, "failed to find assisted image service service")
	}

	if svc.Spec.ClusterIP == "" || len(svc.Spec.Ports) < 1 {
		return "", fmt.Errorf("failed to get internal image service URL, either cluster IP or Ports were missing from Service")
	}

	isoURL, err := url.Parse(originalURL)
	if err != nil {
		return "", errors.Wrapf(err, "failed to parse InfraEnv ISO download URL %s", originalURL)
	}

	isoURL.Scheme = "http"
	isoURL.Host = fmt.Sprintf("%s:%d", svc.Spec.ClusterIP, svc.Spec.Ports[0].Port)
	return isoURL.String(), nil
}
