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

package setup

import (
	"context"

	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/internal/tlsconfig"
	configv1 "github.com/openshift/api/config/v1"
	crtls "github.com/openshift/controller-runtime-common/pkg/tls"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// ResolveTLSConfig returns the appropriate TLS configuration for the cluster.
// On OpenShift it reads the cluster-wide TLS profile; on vanilla Kubernetes it
// returns the default (Intermediate) profile.
func ResolveTLSConfig(ctx context.Context, restConfig *rest.Config, isOpenShift bool) (tlsconfig.TLSConfigResult, error) {
	if !isOpenShift {
		return tlsconfig.DefaultTLSConfig(), nil
	}
	return tlsconfig.ResolveOpenShiftTLSConfig(ctx, restConfig)
}

// SetupSecurityProfileWatcher registers a watcher that triggers a graceful
// shutdown (via cancel) when the cluster-wide TLS profile or adherence policy
// changes. It is a no-op on non-OpenShift clusters.
func SetupSecurityProfileWatcher(mgr manager.Manager, result tlsconfig.TLSConfigResult, isOpenShift bool, cancel context.CancelFunc) error {
	if !isOpenShift {
		return nil
	}

	tlsLog := ctrl.Log.WithName("tls-profile-watcher")
	return (&crtls.SecurityProfileWatcher{
		Client:                    mgr.GetClient(),
		InitialTLSAdherencePolicy: result.TLSAdherencePolicy,
		InitialTLSProfileSpec:     result.TLSProfileSpec,
		OnAdherencePolicyChange: func(_ context.Context, oldPolicy, newPolicy configv1.TLSAdherencePolicy) {
			tlsLog.Info("TLS adherence policy changed, shutting down to reload",
				"oldPolicy", oldPolicy, "newPolicy", newPolicy)
			cancel()
		},
		OnProfileChange: func(_ context.Context, oldProfile, newProfile configv1.TLSProfileSpec) {
			tlsLog.Info("TLS profile changed, shutting down to reload",
				"oldProfile", oldProfile, "newProfile", newProfile)
			cancel()
		},
	}).SetupWithManager(mgr)
}
