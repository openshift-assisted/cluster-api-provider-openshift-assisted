package workloadclient

import (
	configv1 "github.com/openshift/api/config/v1"
	"github.com/pkg/errors"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type WorkloadClusterClientGenerator struct{}

type ClientGenerator interface {
	GetWorkloadClusterClient(kubeconfig []byte) (client.Client, error)
}

func NewWorkloadClusterClientGenerator() *WorkloadClusterClientGenerator {
	return &WorkloadClusterClientGenerator{}
}

func (w *WorkloadClusterClientGenerator) GetWorkloadClusterClient(kubeconfig []byte) (client.Client, error) {
	clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfig)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get clientconfig from kubeconfig data")
	}

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get restconfig for kube client")
	}

	schemes := runtime.NewScheme()
	utilruntime.Must(configv1.AddToScheme(schemes))
	utilruntime.Must(corev1.AddToScheme(schemes))

	targetClient, err := client.New(restConfig, client.Options{Scheme: schemes})
	if err != nil {
		return nil, err
	}
	return targetClient, nil
}
