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
	"crypto/tls"
	"flag"
	"fmt"
	"os"

	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/bootstrap/api/v1alpha1"

	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/assistedinstaller"

	"github.com/kelseyhightower/envconfig"

	controlplanev1alpha2 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha2"

	hivev1 "github.com/openshift/hive/apis/hive/v1"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
	logsv1 "k8s.io/component-base/logs/api/v1"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	metal3 "github.com/metal3-io/cluster-api-provider-metal3/api/v1beta1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"

	bootstrapv1alpha1 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/bootstrap/api/v1alpha1"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/bootstrap/internal/controller"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util/log"
	//+kubebuilder:scaffold:imports
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

var Options struct {
	AssistedInstallerServiceConfig assistedinstaller.ServiceConfig
}

func init() {
	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(metal3.AddToScheme(scheme))
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(bootstrapv1alpha1.AddToScheme(scheme))
	utilruntime.Must(controlplanev1alpha2.AddToScheme(scheme))
	utilruntime.Must(aiv1beta1.AddToScheme(scheme))
	utilruntime.Must(hivev1.AddToScheme(scheme))
	utilruntime.Must(hiveext.AddToScheme(scheme))
	utilruntime.Must(metal3v1alpha1.AddToScheme(scheme))
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
	// Set log level 2 as default.
	if err := pflag.CommandLine.Set("v", "1"); err != nil {
		fmt.Println("Uh-oh, failed to set default log level", err)
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

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
		Port:    9443,
		Host:    "0.0.0.0",
		CertDir: "/tmp/k8s-webhook-server/serving-certs",
	})

	clientConfig := ctrl.GetConfigOrDie()
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
		LeaderElectionID:       "08dff33b.cluster.x-k8s.io",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err := envconfig.Process("", &Options); err != nil {
		setupLog.Error(err, "unable to process environment variables")
		os.Exit(1)
	}

	if err = (&controller.OpenshiftAssistedConfigReconciler{
		Client:                  mgr.GetClient(),
		Scheme:                  mgr.GetScheme(),
		HTTPClientFactory:       &controller.DefaultHTTPClientFactory{},
		AssistedInstallerConfig: Options.AssistedInstallerServiceConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "OpenshiftAssistedConfig")
		os.Exit(1)
	}
	if err = (&v1alpha1.OpenshiftAssistedConfig{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "OpenshiftAssistedConfig")
		os.Exit(1)
	}

	if err = (&controller.AgentReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Agent")
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

	setupLog.V(9).Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
