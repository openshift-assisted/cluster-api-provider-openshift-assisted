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
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/auth"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/pkg/containers"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	releaseImageSourceAnnotation = "cluster.x-k8s.io/release-image-source"
)

// ensureDigestBasedClusterImageSet orchestrates digest resolution and ClusterImageSet creation/update.
// It checks if digest resolution is needed, resolves it if necessary, and ensures the ClusterImageSet exists.
func (r *ClusterDeploymentReconciler) ensureDigestBasedClusterImageSet(
	ctx context.Context,
	imageSetName string,
	releaseImage string,
	acp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
) error {
	log := ctrl.LoggerFrom(ctx)

	// Check if we need to resolve the digest (only if source changed or digest missing)
	digestImage, needsResolution, err := shouldResolveDigest(ctx, r.Client, imageSetName, releaseImage)
	if err != nil {
		return fmt.Errorf("failed to check ClusterImageSet state: %w", err)
	}

	if needsResolution {
		digestImage, err = r.resolveReleaseImageDigest(ctx, acp, releaseImage)
		if err != nil {
			return fmt.Errorf("failed to resolve release image digest for %s: %w", releaseImage, err)
		}
		log.Info("resolved release image digest", "tag_image", releaseImage, "digest_image", digestImage)
	}

	// Create or update ClusterImageSet with digest-based image and source tracking
	if err = ensureClusterImageSet(ctx, r.Client, imageSetName, releaseImage, digestImage); err != nil {
		return fmt.Errorf("failed to create/update ClusterImageSet: %w", err)
	}

	return nil
}

// resolveReleaseImageDigest retrieves pull secret (if configured) and resolves a tag-based release image to digest-based format.
// Falls back to anonymous authentication when PullSecretRef is not configured, enabling digest resolution for publicly readable images.
// Returns the digest-based image reference or an error if resolution fails.
func (r *ClusterDeploymentReconciler) resolveReleaseImageDigest(
	ctx context.Context,
	acp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	releaseImage string,
) (string, error) {
	var pullSecret []byte
	var err error

	// Use pull secret for authentication if configured, otherwise fall back to anonymous auth
	if acp.Spec.Config.PullSecretRef != nil {
		pullSecret, err = auth.GetPullSecret(r.Client, ctx, acp)
		if err != nil {
			return "", fmt.Errorf("failed to retrieve pull secret: %w", err)
		}
	}
	// If pullSecret is nil (empty byte slice), getReleaseImageWithDigest will use anonymous auth

	// Resolve digest-based image for IDMS compatibility
	digestImage, err := getReleaseImageWithDigest(releaseImage, pullSecret, r.RemoteImage)
	if err != nil {
		return "", fmt.Errorf("failed to resolve digest: %w", err)
	}

	return digestImage, nil
}

// shouldResolveDigest checks if digest resolution is needed.
// Returns: (existingDigest, needsResolution, error)
// needsResolution is true if the source tag changed or no digest exists.
func shouldResolveDigest(ctx context.Context, c client.Client, imageSetName string, currentSource string) (string, bool, error) {
	imageSet := &hivev1.ClusterImageSet{}
	err := c.Get(ctx, client.ObjectKey{Name: imageSetName}, imageSet)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			// ClusterImageSet doesn't exist yet - needs resolution
			return "", true, nil
		}
		return "", false, err
	}

	// Check if source tag has changed
	storedSource := imageSet.Annotations[releaseImageSourceAnnotation]
	if storedSource != currentSource {
		// Source changed - needs re-resolution
		return "", true, nil
	}

	// Check if we have a digest-based image (contains @)
	if !strings.Contains(imageSet.Spec.ReleaseImage, "@") {
		// Tag-based image - needs resolution
		return "", true, nil
	}

	// Source hasn't changed and we have a digest - reuse it
	return imageSet.Spec.ReleaseImage, false, nil
}

// ensureClusterImageSet creates or updates a ClusterImageSet with the given digest-based release image.
// It also stores the source tag in annotations for future comparison.
func ensureClusterImageSet(ctx context.Context, c client.Client, imageSetName string, sourceImage string, digestImage string) error {
	imageSet := &hivev1.ClusterImageSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: imageSetName,
		},
	}

	_, err := controllerutil.CreateOrPatch(ctx, c, imageSet, func() error {
		if imageSet.Annotations == nil {
			imageSet.Annotations = make(map[string]string)
		}
		// Store the source tag for future comparison
		imageSet.Annotations[releaseImageSourceAnnotation] = sourceImage
		imageSet.Spec.ReleaseImage = digestImage
		return nil
	})

	return err
}

// getReleaseImageWithDigest resolves a tag-based release image reference to a digest-based reference.
// This enables compatibility with ImageDigestMirrorSet (IDMS) in disconnected environments.
// Uses anonymous authentication if pullSecret is nil/empty, enabling digest resolution for publicly readable images.
func getReleaseImageWithDigest(image string, pullSecret []byte, remoteImage containers.RemoteImage) (string, error) {
	// Default to anonymous authentication for publicly readable images
	keychain := authn.Anonymous

	// Override with pull secret credentials if provided
	if len(pullSecret) > 0 {
		var err error
		keychain, err = containers.PullSecretKeyChainFromString(string(pullSecret))
		if err != nil {
			return "", fmt.Errorf("failed to create keychain from pull secret: %w", err)
		}
	}

	digest, err := remoteImage.GetDigest(image, keychain)
	if err != nil {
		return "", fmt.Errorf("failed to resolve digest for image %s: %w", image, err)
	}

	repoImage, err := containers.GetRepoImage(image)
	if err != nil {
		return "", err
	}

	return repoImage + "@" + digest, nil
}
