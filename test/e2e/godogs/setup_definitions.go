package godogs

import (
	"context"
	"fmt"
	"os"

	"github.com/cucumber/godog"
	"github.com/openshift-assisted/cluster-api-agent/test/workloads"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Installer struct {
	Client     client.Client
	Workloads  []string
	Kubeconfig string
}

func (i *Installer) kubernetesClusterExists(ctx context.Context, exists string) error {
	kubeconfig := os.Getenv("KUBECONFIG")
	/* 	config := ctrl.GetConfigOrDie()
	   	if exists == "a" && kubeconfig == "" && config == nil {
	   		return fmt.Errorf("no KUBECONFIG set for existing Kubernetes cluster")
	   	}
	   	mgr, err := ctrl.NewManager(config, manager.Options{})
	   	if err != nil {
	   		return fmt.Errorf("failed to create new client")
	   	}
	   	i.Client = mgr.GetClient() */
	i.Kubeconfig = kubeconfig
	return nil
}

func (i *Installer) services(ctx context.Context, action string) error {
	switch action {
	case "install":
		return i.installWorkloads()
	case "uninstall":
		return i.uninstallWorkloads()
	default:
		return fmt.Errorf("unknown action %s", action)
	}
}

func (i *Installer) installWorkloads() error {
	return workloads.Install(i.Kubeconfig)
}

func (i *Installer) uninstallWorkloads() error {
	return workloads.Uninstall(i.Kubeconfig)
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	installer := &Installer{}
	ctx.Step(`^([a-z])+ Kubernetes management cluster$`, installer.kubernetesClusterExists)
	ctx.Step(`^I want to ([\w]+) all services onto the cluster$`, installer.services)
}
