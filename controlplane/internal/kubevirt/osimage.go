/*
Copyright 2026.

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

	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const agentServiceConfigName = "agent"

// EnsureOSImageInAgentServiceConfig checks if the AgentServiceConfig has an OS image
// entry for the target OCP major.minor version and CPU architecture. If missing, it
// adds one using the standard RHCOS live ISO mirror URL pattern.
func EnsureOSImageInAgentServiceConfig(ctx context.Context, c client.Client, openshiftVersion string, cpuArch string) error {
	log := ctrl.LoggerFrom(ctx)

	majorMinor := extractMajorMinor(openshiftVersion)
	if majorMinor == "" {
		return fmt.Errorf("cannot extract major.minor from version %q", openshiftVersion)
	}

	if cpuArch == "" || cpuArch == "multi" {
		cpuArch = "x86_64"
	}

	asc := &aiv1beta1.AgentServiceConfig{}
	if err := c.Get(ctx, client.ObjectKey{Name: agentServiceConfigName}, asc); err != nil {
		return fmt.Errorf("failed to get AgentServiceConfig: %w", err)
	}

	for _, img := range asc.Spec.OSImages {
		if img.OpenshiftVersion == majorMinor && img.CPUArchitecture == cpuArch {
			log.V(1).Info("OS image already exists in AgentServiceConfig", "version", majorMinor, "arch", cpuArch)
			return nil
		}
	}

	isoURL := fmt.Sprintf(
		"https://mirror.openshift.com/pub/openshift-v4/%s/dependencies/rhcos/%s/latest/rhcos-live-iso.%s.iso",
		cpuArch, majorMinor, cpuArch,
	)
	rootFSURL := fmt.Sprintf(
		"https://mirror.openshift.com/pub/openshift-v4/%s/dependencies/rhcos/%s/latest/rhcos-live-rootfs.%s.img",
		cpuArch, majorMinor, cpuArch,
	)

	asc.Spec.OSImages = append(asc.Spec.OSImages, aiv1beta1.OSImage{
		OpenshiftVersion: majorMinor,
		Version:          majorMinor,
		Url:              isoURL,
		RootFSUrl:        rootFSURL,
		CPUArchitecture:  cpuArch,
	})

	if err := c.Update(ctx, asc); err != nil {
		return fmt.Errorf("failed to update AgentServiceConfig with OS image for %s/%s: %w", majorMinor, cpuArch, err)
	}

	log.Info("added OS image to AgentServiceConfig", "version", majorMinor, "arch", cpuArch, "url", isoURL)
	return nil
}
