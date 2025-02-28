package containers

import (
	"errors"
	"fmt"

	"github.com/google/go-containerregistry/pkg/authn"
	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

var ErrImageNotFound = errors.New("image not found")

func (e *RemoteImageRepository) GetImage(imageRef string, keychain authn.Keychain) (v1.Image, error) {
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Image reference: %w", ErrImageNotFound)
	}
	image, err := remote.Image(ref, remote.WithAuthFromKeychain(keychain))
	if err != nil {
		return image, errors.Join(ErrImageNotFound, err)
	}
	return image, nil
}

// GetDigest retrieves the SHA256 digest of an OCI image given its tag notation.
func (e *RemoteImageRepository) GetDigest(imageRef string, keychain authn.Keychain) (string, error) {
	// Parse the image reference
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return "", fmt.Errorf("failed to parse image reference: %w", err)
	}

	// Get the descriptor from the remote registry
	desc, err := remote.Get(ref, remote.WithAuthFromKeychain(keychain))
	if err != nil {
		return "", fmt.Errorf("failed to get remote image descriptor: %w", err)
	}

	// Return the SHA256 digest as a string
	return desc.Digest.String(), nil
}
