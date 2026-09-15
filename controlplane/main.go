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

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/upgrade"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/workloadclient"

	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/pkg/containers"

	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/version"

	bootstrapv1alpha2 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/bootstrap/api/v1alpha2"
	configv1 "github.com/openshift/api/config/v1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	capiv1beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1" //nolint:staticcheck
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
	logsv1 "k8s.io/component-base/logs/api/v1"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/conversion"

	controlplanev1alpha2 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha2"
	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	controlplanecontroller "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/controller"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/internal/openshift"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/internal/setup"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util/log"
)

var (
	scheme     = runtime.NewScheme()
	logOptions = logs.NewOptions()

	// Options
	metricsAddr          string
	enableLeaderElection bool
	probeAddr            string
	secureMetrics        bool
	enableHTTP2          bool
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(controlplanev1alpha2.AddToScheme(scheme))
	utilruntime.Must(controlplanev1alpha3.AddToScheme(scheme))
	utilruntime.Must(capiv1beta1.AddToScheme(scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(hivev1.AddToScheme(scheme))
	utilruntime.Must(hiveext.AddToScheme(scheme))
	utilruntime.Must(aiv1beta1.AddToScheme(scheme))
	utilruntime.Must(bootstrapv1alpha2.AddToScheme(scheme))
	utilruntime.Must(configv1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func initFlags(fs *pflag.FlagSet) {
	logsv1.AddFlags(logOptions, fs)
	fs.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	fs.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	fs.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	fs.BoolVar(&secureMetrics, "metrics-secure", false,
		"If set the metrics endpoint is served securely")
	fs.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
}

func main() {
	initFlags(pflag.CommandLine)
	pflag.CommandLine.SetNormalizeFunc(cliflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	// Set log level 3 (info) as default.
	if err := pflag.CommandLine.Set("v", "3"); err != nil {
		fmt.Println("failed to set default log level", err)
		os.Exit(1)
	}

	pflag.Parse()

	// Validate and apply log configuration
	if err := logsv1.ValidateAndApply(logOptions, nil); err != nil {
		fmt.Printf("error validating log options: %v\n", err)
		os.Exit(1)
	}

	ctrl.SetLogger(klog.NewKlogr())
	setupLog := ctrl.Log.WithName("setup")
	setupLog.V(log.DebugLevel).Info("logging initialized")

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	tlsOpts := []func(*tls.Config){}
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	clientConfig := ctrl.GetConfigOrDie()

	isOpenShift, err := openshift.IsOpenShift(context.Background(), clientConfig)
	if err != nil {
		setupLog.Error(err, "unable to detect cluster type")
		os.Exit(1)
	}

	tlsResult, err := setup.ResolveTLSConfig(context.Background(), clientConfig, isOpenShift)
	if err != nil {
		setupLog.Error(err, "unable to resolve TLS config")
		os.Exit(1)
	}
	if tlsResult.TLSConfig != nil {
		tlsOpts = append(tlsOpts, tlsResult.TLSConfig)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
		Port:    9443,
		Host:    "0.0.0.0",
		CertDir: "/tmp/k8s-webhook-server/serving-certs",
	})

	mgr, err := ctrl.NewManager(clientConfig, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   metricsAddr,
			SecureServing: secureMetrics,
			TLSOpts:       tlsOpts,
		},
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "09dff44b.cluster.x-k8s.io",
		Client: client.Options{
			Cache: &client.CacheOptions{
				DisableFor: []client.Object{
					&corev1.ConfigMap{},
				},
			},
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}
	// Register CAPI v1beta1↔v1beta2 conversion webhook so our controller can serve
	// CRD conversions without depending on HyperShift's webhook.
	// Use a direct (non-cached) client for CRD lookups so conversion works even
	// before the manager's cache has synced (critical during startup).
	directClient, err := client.New(mgr.GetConfig(), client.Options{Scheme: mgr.GetScheme()})
	if err != nil {
		setupLog.Error(err, "unable to create direct client for conversion webhook")
		os.Exit(1)
	}
	capiv1beta1.SetAPIVersionGetter(func(gk schema.GroupKind) (string, error) {
		// First check registered scheme versions
		versions := mgr.GetScheme().VersionsForGroupKind(gk)
		if gk.Group == "" && len(versions) == 0 {
			for _, gv := range mgr.GetScheme().PrioritizedVersionsAllGroups() {
				candidate := schema.GroupKind{Group: gv.Group, Kind: gk.Kind}
				if vs := mgr.GetScheme().VersionsForGroupKind(candidate); len(vs) > 0 {
					return candidate.WithVersion(vs[0].Version).GroupVersion().String(), nil
				}
			}
		}
		if len(versions) > 0 {
			return gk.WithVersion(versions[0].Version).GroupVersion().String(), nil
		}
		// Fallback: look up CRD for types not in our scheme (e.g. CAPOA/CAPK types)
		groupsToTry := []string{gk.Group}
		if gk.Group == "" {
			groupsToTry = []string{
				"bootstrap.cluster.x-k8s.io",
				"controlplane.cluster.x-k8s.io",
				"infrastructure.cluster.x-k8s.io",
				"cluster.x-k8s.io",
			}
		}
		kindPlural := strings.ToLower(gk.Kind) + "s"
		for _, group := range groupsToTry {
			crdName := fmt.Sprintf("%s.%s", kindPlural, group)
			crd := &apiextensionsv1.CustomResourceDefinition{}
			if err := directClient.Get(context.Background(), client.ObjectKey{Name: crdName}, crd); err != nil {
				continue
			}
			for _, v := range crd.Spec.Versions {
				if v.Served {
					return schema.GroupVersion{Group: group, Version: v.Name}.String(), nil
				}
			}
		}
		return "", fmt.Errorf("no versions registered for GroupKind %s", gk)
	})
	// Register the /convert handler for CAPI CRD conversion on our webhook server
	mgr.GetWebhookServer().Register("/convert", conversion.NewWebhookHandler(mgr.GetScheme(), conversion.NewRegistry()))
	setupLog.Info("registered CAPI CRD conversion webhook on /convert")

	releaseImageRepository := containers.NewRemoteImageRepository()
	clientGenerator := workloadclient.NewWorkloadClusterClientGenerator()
	if err = (&controlplanecontroller.OpenshiftAssistedControlPlaneReconciler{
		Client:             mgr.GetClient(),
		Scheme:             mgr.GetScheme(),
		K8sVersionDetector: version.NewKubernetesVersionDetector(releaseImageRepository),
		UpgradeFactory:     upgrade.NewOpenshiftUpgradeFactory(releaseImageRepository, clientGenerator),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "OpenshiftAssistedControlPlane")
		os.Exit(1)
	}
	if err = (&controlplanecontroller.ClusterDeploymentReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		RemoteImage: releaseImageRepository,
		APIReader:   mgr.GetAPIReader(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterDeployment")
		os.Exit(1)
	}
	if err = (&controlplanecontroller.AgentClusterInstallReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AgentClusterInstall")
		os.Exit(1)
	}
	if err = (&controlplanev1alpha3.OpenshiftAssistedControlPlane{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "OpenshiftAssistedControlPlane")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	ctx := ctrl.SetupSignalHandler()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := setup.SetupSecurityProfileWatcher(mgr, tlsResult, isOpenShift, cancel); err != nil {
		setupLog.Error(err, "unable to create TLS security profile watcher")
		os.Exit(1)
	}

	if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		return ensureCRDContractLabels(ctx, mgr.GetAPIReader(), mgr.GetClient())
	})); err != nil {
		setupLog.Error(err, "unable to add CRD contract label runnable")
		os.Exit(1)
	}

	setupLog.V(log.DebugLevel).Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// ensureCRDContractLabels ensures CAPI contract version labels exist on
// provider CRDs. The CAPI conversion webhook needs these to resolve
// which provider API version corresponds to each CAPI contract version.
// The CAPI core controller may strip them on startup if its embedded
// CRD templates lack the labels, so we re-apply on every CAPOA startup.
func ensureCRDContractLabels(ctx context.Context, reader client.Reader, c client.Client) error {
	log := ctrl.Log.WithName("crd-labels")

	crdLabels := map[string]map[string]string{
		"kubevirtclusters.infrastructure.cluster.x-k8s.io":                          {"cluster.x-k8s.io/v1beta1": "v1alpha1", "cluster.x-k8s.io/v1beta2": "v1alpha1"},
		"kubevirtmachines.infrastructure.cluster.x-k8s.io":                          {"cluster.x-k8s.io/v1beta1": "v1alpha1", "cluster.x-k8s.io/v1beta2": "v1alpha1"},
		"kubevirtmachinetemplates.infrastructure.cluster.x-k8s.io":                  {"cluster.x-k8s.io/v1beta1": "v1alpha1", "cluster.x-k8s.io/v1beta2": "v1alpha1"},
		"openshiftassistedconfigs.bootstrap.cluster.x-k8s.io":                       {"cluster.x-k8s.io/v1beta1": "v1alpha2", "cluster.x-k8s.io/v1beta2": "v1alpha2"},
		"openshiftassistedconfigtemplates.bootstrap.cluster.x-k8s.io":               {"cluster.x-k8s.io/v1beta1": "v1alpha2", "cluster.x-k8s.io/v1beta2": "v1alpha2"},
		"openshiftassistedcontrolplanes.controlplane.cluster.x-k8s.io":              {"cluster.x-k8s.io/v1beta1": "v1alpha3", "cluster.x-k8s.io/v1beta2": "v1alpha3"},
	}

	for crdName, requiredLabels := range crdLabels {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := reader.Get(ctx, client.ObjectKey{Name: crdName}, crd); err != nil {
			log.V(1).Info("CRD not found, skipping label check", "crd", crdName)
			continue
		}

		needsUpdate := false
		if crd.Labels == nil {
			crd.Labels = make(map[string]string)
		}
		for k, v := range requiredLabels {
			if crd.Labels[k] != v {
				crd.Labels[k] = v
				needsUpdate = true
			}
		}
		if needsUpdate {
			if err := c.Update(ctx, crd); err != nil {
				log.Error(err, "failed to update CRD contract labels", "crd", crdName)
			} else {
				log.Info("ensured CAPI contract labels on CRD", "crd", crdName)
			}
		}
	}
	return nil
}
