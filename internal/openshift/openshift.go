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

package openshift

import (
	"context"
	"fmt"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
)

var log = ctrl.Log.WithName("openshift")

const pollInterval = 5 * time.Second

// IsOpenShift checks whether the cluster is an OpenShift cluster by looking for
// the apiservers.config.openshift.io resource via the discovery API. If the API
// server is temporarily unreachable it retries until ctx is cancelled.
func IsOpenShift(ctx context.Context, restConfig *rest.Config) (bool, error) {
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return false, fmt.Errorf("creating kubernetes clientset: %w", err)
	}

	return isOpenShiftWithDiscovery(ctx, clientset.Discovery())
}

func isOpenShiftWithDiscovery(ctx context.Context, disc discovery.DiscoveryInterface) (bool, error) {
	groupVersion := configv1.GroupVersion.String()
	var result bool

	err := wait.PollUntilContextCancel(ctx, pollInterval, true, func(ctx context.Context) (bool, error) {
		resourceList, err := disc.ServerResourcesForGroupVersion(groupVersion)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			log.V(1).Info("API server not reachable yet, retrying", "error", err)
			return false, nil
		}

		for _, resource := range resourceList.APIResources {
			if resource.Name == "apiservers" {
				result = true
				return true, nil
			}
		}

		return true, nil
	})
	if err != nil {
		return false, fmt.Errorf("waiting for API server: %w", err)
	}

	return result, nil
}
