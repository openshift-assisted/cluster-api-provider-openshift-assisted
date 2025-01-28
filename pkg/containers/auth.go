package containers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
)

// PullSecretKeyChain implements the authn.Keychain interface
type PullSecretKeyChain struct {
	credentials map[string]authn.AuthConfig
}

// Resolve extracts credentials for a given registry
func (kc *PullSecretKeyChain) Resolve(target authn.Resource) (authn.Authenticator, error) {
	authConfig, found := kc.credentials[target.RegistryStr()]
	if !found {
		return authn.Anonymous, nil // No credentials, use anonymous access
	}
	return authn.FromConfig(authConfig), nil
}

// Load a PullSecretKeyChain from a pullsecret string
func PullSecretKeyChainFromString(pullSecret string) (authn.Keychain, error) {
	var dockerConfig struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}

	if err := json.Unmarshal([]byte(pullSecret), &dockerConfig); err != nil {
		return nil, fmt.Errorf("failed to parse pullsecret: %w", err)
	}

	credentials := make(map[string]authn.AuthConfig)
	for registry, entry := range dockerConfig.Auths {
		decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
		if err != nil {
			return nil, fmt.Errorf("failed to decode auth for %s: %w", registry, err)
		}
		parts := strings.SplitN(string(decoded), ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid auth format for %s", registry)
		}

		credentials[registry] = authn.AuthConfig{
			Username: parts[0],
			Password: parts[1],
		}
	}

	return &PullSecretKeyChain{credentials: credentials}, nil
}
