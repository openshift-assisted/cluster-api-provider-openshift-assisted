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

// Package tlsconfig provides cluster-wide TLS configuration resolution for
// OpenShift and vanilla Kubernetes environments.
package tlsconfig

import (
	"context"
	"crypto/tls"
	"fmt"

	configv1 "github.com/openshift/api/config/v1"
	crtls "github.com/openshift/controller-runtime-common/pkg/tls"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var log = ctrl.Log.WithName("tlsconfig")

// TLSConfigResult holds the resolved TLS configuration.
type TLSConfigResult struct {
	// TLSConfig is a function that configures a tls.Config with the resolved
	// cipher suites and minimum TLS version. Intended for use with
	// controller-runtime's TLSOpts.
	TLSConfig func(*tls.Config)

	// TLSAdherencePolicy is the cluster-wide TLS adherence policy from
	// apiserver.config.openshift.io/cluster.
	TLSAdherencePolicy configv1.TLSAdherencePolicy

	// TLSProfileSpec is the resolved TLS profile specification (ciphers and
	// minimum TLS version).
	TLSProfileSpec configv1.TLSProfileSpec
}

// DefaultTLSConfig returns a TLSConfigResult using the default (Intermediate)
// TLS profile. Suitable for vanilla Kubernetes clusters.
func DefaultTLSConfig() TLSConfigResult {
	defaultProfileSpec := *configv1.TLSProfiles[libgocrypto.DefaultTLSProfileType]
	tlsConfig, _ := crtls.NewTLSConfigFromProfile(defaultProfileSpec)

	return TLSConfigResult{
		TLSConfig:      tlsConfig,
		TLSProfileSpec: defaultProfileSpec,
	}
}

// ResolveOpenShiftTLSConfig reads the cluster-wide TLS profile from the
// APIServer resource and returns the appropriate TLS configuration.
func ResolveOpenShiftTLSConfig(ctx context.Context, restConfig *rest.Config) (TLSConfigResult, error) {
	scheme := runtime.NewScheme()
	if err := configv1.Install(scheme); err != nil {
		return TLSConfigResult{}, fmt.Errorf("registering configv1 scheme: %w", err)
	}

	k8sClient, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return TLSConfigResult{}, fmt.Errorf("creating controller-runtime client: %w", err)
	}

	return resolveOpenShiftTLSConfig(ctx, k8sClient)
}

func resolveOpenShiftTLSConfig(ctx context.Context, k8sClient client.Client) (TLSConfigResult, error) {
	adherencePolicy, err := crtls.FetchAPIServerTLSAdherencePolicy(ctx, k8sClient)
	if err != nil {
		return TLSConfigResult{}, fmt.Errorf("fetching TLS adherence policy from APIServer: %w", err)
	}

	profileSpec, err := crtls.FetchAPIServerTLSProfile(ctx, k8sClient)
	if err != nil {
		return TLSConfigResult{}, fmt.Errorf("fetching TLS profile from APIServer: %w", err)
	}

	if !libgocrypto.ShouldHonorClusterTLSProfile(adherencePolicy) {
		log.V(1).Info("TLS adherence policy does not require honoring cluster profile, using defaults",
			"policy", adherencePolicy)
		result := DefaultTLSConfig()
		result.TLSAdherencePolicy = adherencePolicy
		return result, nil
	}

	log.Info("honoring cluster-wide TLS profile",
		"policy", adherencePolicy,
		"minTLSVersion", profileSpec.MinTLSVersion)

	tlsConfig, unsupported := crtls.NewTLSConfigFromProfile(profileSpec)
	supportedCiphers := len(profileSpec.Ciphers) - len(unsupported)
	if len(unsupported) > 0 {
		log.Info("some ciphers from the cluster TLS profile are not supported",
			"unsupportedCiphers", unsupported, "supportedCiphers", supportedCiphers)
	}
	if supportedCiphers == 0 && len(profileSpec.Ciphers) > 0 {
		return TLSConfigResult{}, fmt.Errorf("none of the ciphers from the cluster TLS profile are supported: %v", unsupported)
	}

	return TLSConfigResult{
		TLSConfig:          tlsConfig,
		TLSAdherencePolicy: adherencePolicy,
		TLSProfileSpec:     profileSpec,
	}, nil
}
