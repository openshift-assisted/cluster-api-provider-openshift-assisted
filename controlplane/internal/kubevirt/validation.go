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

package kubevirt

import (
	"context"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	assistedServiceNamespace  = "multicluster-engine"
	assistedServiceDeployment = "assisted-service"
	dnsWildcardValidation     = "dns-wildcard-not-configured"
	disabledValidationsEnvVar = "DISABLED_HOST_VALIDATIONS"
)

// EnsureDNSWildcardValidationDisabled patches the assisted-service deployment
// to disable the "dns-wildcard-not-configured" host validation.
//
// When using Route-based access, the tenant's apps domain is a subdomain of the
// infra cluster's wildcard DNS. Assisted Service flags this as an error because
// it sees a wildcard DNS record for the apps domain. This is a false positive
// for the Route-based topology.
func EnsureDNSWildcardValidationDisabled(ctx context.Context, c client.Client) error {
	deploy := &appsv1.Deployment{}
	key := client.ObjectKey{Name: assistedServiceDeployment, Namespace: assistedServiceNamespace}
	if err := c.Get(ctx, key, deploy); err != nil {
		return fmt.Errorf("failed to get assisted-service deployment: %w", err)
	}

	containers := deploy.Spec.Template.Spec.Containers
	if len(containers) == 0 {
		return nil
	}

	// Find the main container and check/update the env var
	for i, container := range containers {
		if container.Name != "assisted-service" && container.Name != "service" {
			continue
		}
		for j, env := range container.Env {
			if env.Name == disabledValidationsEnvVar {
				if strings.Contains(env.Value, dnsWildcardValidation) {
					return nil // already disabled
				}
				if env.Value == "" {
					containers[i].Env[j].Value = dnsWildcardValidation
				} else {
					containers[i].Env[j].Value = env.Value + "," + dnsWildcardValidation
				}
				return c.Update(ctx, deploy)
			}
		}
		// Env var not found, add it
		containers[i].Env = append(containers[i].Env, corev1.EnvVar{
			Name:  disabledValidationsEnvVar,
			Value: dnsWildcardValidation,
		})
		return c.Update(ctx, deploy)
	}

	// No matching container found, try the first one
	if len(containers) > 0 {
		containers[0].Env = append(containers[0].Env, corev1.EnvVar{
			Name:  disabledValidationsEnvVar,
			Value: dnsWildcardValidation,
		})
		return c.Update(ctx, deploy)
	}

	return nil
}
