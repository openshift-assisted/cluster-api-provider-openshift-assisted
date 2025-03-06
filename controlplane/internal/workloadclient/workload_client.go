package workloadclient

import (
	"context"
	"errors"
	"fmt"

	"github.com/openshift-assisted/cluster-api-agent/util"
	configv1 "github.com/openshift/api/config/v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type WorkloadClusterClientGenerator struct{}

//go:generate mockgen -destination=mock_clientgenerator.go -package=workloadclient -source workload_client.go ClientGenerator
type ClientGenerator interface {
	GetWorkloadClusterClient(kubeconfig []byte) (client.Client, error)
}

func NewWorkloadClusterClientGenerator() *WorkloadClusterClientGenerator {
	return &WorkloadClusterClientGenerator{}
}

func (w *WorkloadClusterClientGenerator) GetWorkloadClusterClient(kubeconfig []byte) (client.Client, error) {
	clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfig)
	if err != nil {
		return nil, errors.Join(err, fmt.Errorf("failed to get clientconfig from kubeconfig data"))
	}

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, errors.Join(err, fmt.Errorf("failed to get restconfig for kube client"))
	}

	schemes := runtime.NewScheme()
	configv1.AddToScheme(schemes)
	corev1.AddToScheme(schemes)
	targetClient, err := client.New(restConfig, client.Options{Scheme: schemes})
	if err != nil {
		return nil, err
	}
	return targetClient, nil
}

func GetWorkloadClientFromClusterName(ctx context.Context, client client.Client,
	workloadClusterClientGenerator ClientGenerator,
	clusterName, clusterNamespace string) (client.Client, error) {

	kubeconfig, err := util.GetWorkloadKubeconfig(ctx, client, clusterName, clusterNamespace)
	if err != nil {
		return nil, err
	}

	workloadClient, err := workloadClusterClientGenerator.GetWorkloadClusterClient(kubeconfig)
	if err != nil {
		err = errors.Join(err, fmt.Errorf("failed to establish client for workload cluster from kubeconfig"))
		return nil, err
	}
	return workloadClient, nil
}
