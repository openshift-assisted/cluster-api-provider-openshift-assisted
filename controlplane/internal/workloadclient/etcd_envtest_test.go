/*
Copyright 2026.

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

package workloadclient

import (
	"context"
	"fmt"
	"testing"

	"go.etcd.io/etcd/api/v3/etcdserverpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestProtectEtcdLeadershipWithEnvtest(t *testing.T) {
	// Envtest provides a real API server for Pod selection, while the etcd
	// connections remain mocked because envtest does not run kubelets or Pod
	// port-forward endpoints.
	testEnv := &envtest.Environment{}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := testEnv.Stop(); err != nil {
			t.Errorf("failed to stop envtest: %v", err)
		}
	})

	ctx := context.Background()
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: etcdPodNamespace},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	for _, pod := range []*corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "00-discovery",
				Namespace: etcdPodNamespace,
				Labels:    map[string]string{"app": "etcd"},
			},
			Spec: corev1.PodSpec{
				NodeName:   "node-discovery",
				Containers: []corev1.Container{{Name: "etcd", Image: "quay.io/example/etcd"}},
			},
			Status: corev1.PodStatus{
				Phase:      corev1.PodRunning,
				Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "01-source",
				Namespace: etcdPodNamespace,
				Labels:    map[string]string{"app": "etcd"},
			},
			Spec: corev1.PodSpec{
				NodeName:   "node-source",
				Containers: []corev1.Container{{Name: "etcd", Image: "quay.io/example/etcd"}},
			},
			Status: corev1.PodStatus{
				Phase:      corev1.PodRunning,
				Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			},
		},
	} {
		createdPod, err := clientset.CoreV1().Pods(etcdPodNamespace).Create(ctx, pod, metav1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		createdPod.Status = pod.Status
		if _, err := clientset.CoreV1().Pods(etcdPodNamespace).UpdateStatus(ctx, createdPod, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	kubeconfig, err := kubeconfigForEnvtest(cfg)
	if err != nil {
		t.Fatal(err)
	}

	members := []*etcdserverpb.Member{
		{ID: 1, Name: "node-source"},
		{ID: 2, Name: "node-target"},
	}
	discoveryClient := &mockEtcdClient{
		members:         members,
		statusResponses: []*clientv3.StatusResponse{statusResponse(1, 1)},
	}
	sourceClient := &mockEtcdClient{
		members:         members,
		statusResponses: []*clientv3.StatusResponse{statusResponse(1, 1)},
	}
	var discoveryClosed, sourceClosed bool
	discovery := testEtcdConnection(discoveryClient, "https://discovery", "00-discovery", "node-discovery", &discoveryClosed)
	source := testEtcdConnection(sourceClient, "https://source", "01-source", "node-source", &sourceClosed)

	generator := &WorkloadClusterClientGenerator{
		etcdConnectionFactory: func(ctx context.Context, kubeconfig []byte, _, targetNodeName string) (*etcdConnection, error) {
			clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfig)
			if err != nil {
				return nil, err
			}
			restConfig, err := clientConfig.ClientConfig()
			if err != nil {
				return nil, err
			}
			clientset, err := kubernetes.NewForConfig(restConfig)
			if err != nil {
				return nil, err
			}
			pod, err := selectRunningEtcdPod(ctx, clientset.CoreV1().Pods(etcdPodNamespace), "", targetNodeName)
			if err != nil {
				return nil, err
			}
			if targetNodeName == "" {
				if pod.Name != discovery.podName {
					return nil, fmt.Errorf("selected unexpected discovery pod: got %s, want %s", pod.Name, discovery.podName)
				}
				return discovery, nil
			}
			if pod.Spec.NodeName != source.nodeName {
				return nil, fmt.Errorf("selected unexpected etcd pod node: got %s, want %s", pod.Spec.NodeName, source.nodeName)
			}
			return source, nil
		},
	}

	if err := generator.ProtectEtcdLeadership(ctx, kubeconfig, "node-source", "node-target"); err != nil {
		t.Fatal(err)
	}
	if len(sourceClient.moveLeaderCalls) != 1 || sourceClient.moveLeaderCalls[0] != 2 {
		t.Fatalf("expected leadership transfer to member 2, got %v", sourceClient.moveLeaderCalls)
	}
	if discoveryClient.moveLeaderCalls != nil {
		t.Fatalf("leadership transfer used the discovery connection: %v", discoveryClient.moveLeaderCalls)
	}
	if !discoveryClosed || !sourceClosed {
		t.Fatalf("expected both etcd connections to close, discovery=%t source=%t", discoveryClosed, sourceClosed)
	}
}

func kubeconfigForEnvtest(cfg *rest.Config) ([]byte, error) {
	return clientcmd.Write(clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"envtest": {
				Server:                   cfg.Host,
				CertificateAuthorityData: cfg.CAData,
				InsecureSkipTLSVerify:    cfg.Insecure,
			},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"envtest": {
				ClientCertificateData: cfg.CertData,
				ClientKeyData:         cfg.KeyData,
				Token:                 cfg.BearerToken,
			},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"envtest": {
				Cluster:  "envtest",
				AuthInfo: "envtest",
			},
		},
		CurrentContext: "envtest",
	})
}
