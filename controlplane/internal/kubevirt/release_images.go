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
	"fmt"

	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/pkg/containers"
	imageapi "github.com/openshift/api/image/v1"
	"sigs.k8s.io/yaml"
)

const imageReferencesPath = "/release-manifests/image-references"

// ResolveImageFromPayload extracts a component image reference from the OCP release
// payload. This inspects the release image's image-references manifest in-process
// (no Job needed). The pullSecret is required for registry authentication.
func ResolveImageFromPayload(
	releaseImageRef string,
	pullSecret []byte,
	componentName string,
	remoteImage containers.RemoteImage,
) (string, error) {
	auth, err := containers.PullSecretKeyChainFromString(string(pullSecret))
	if err != nil {
		return "", fmt.Errorf("failed to load auth from pull secret: %w", err)
	}

	image, err := remoteImage.GetImage(releaseImageRef, auth)
	if err != nil {
		return "", fmt.Errorf("failed to get release image %s: %w", releaseImageRef, err)
	}

	extractor, err := containers.NewImageInspector(image)
	if err != nil {
		return "", fmt.Errorf("failed to create image inspector: %w", err)
	}

	fileContent, err := extractor.ExtractFileFromImage(imageReferencesPath)
	if err != nil {
		return "", fmt.Errorf("failed to extract %s from release image: %w", imageReferencesPath, err)
	}

	is := &imageapi.ImageStream{}
	if err := yaml.Unmarshal(fileContent, is); err != nil {
		return "", fmt.Errorf("failed to parse image-references: %w", err)
	}

	for _, tag := range is.Spec.Tags {
		if tag.Name == componentName {
			if tag.From == nil {
				return "", fmt.Errorf("component %q found in image-references but has no From reference", componentName)
			}
			return tag.From.Name, nil
		}
	}

	return "", fmt.Errorf("component %q not found in release image-references", componentName)
}
