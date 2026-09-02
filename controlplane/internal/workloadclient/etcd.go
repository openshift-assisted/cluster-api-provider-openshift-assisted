package workloadclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"go.etcd.io/etcd/api/v3/etcdserverpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

const (
	etcdClientSecretName   = "etcd-client"
	etcdCAConfigMapName    = "etcd-ca-bundle"
	etcdTLSSecretNamespace = "openshift-config"
	etcdPodNamespace       = "openshift-etcd"
	etcdClientPort         = 2379
	etcdDialTimeout        = 10 * time.Second
	etcdOpTimeout          = 30 * time.Second
	etcdSetupTimeout       = 30 * time.Second
)

type etcdMemberManager interface {
	MemberList(ctx context.Context, opts ...clientv3.OpOption) (*clientv3.MemberListResponse, error)
	MemberRemove(ctx context.Context, id uint64) (*clientv3.MemberRemoveResponse, error)
	AlarmList(ctx context.Context) (*clientv3.AlarmResponse, error)
	MoveLeader(ctx context.Context, transfereeID uint64) (*clientv3.MoveLeaderResponse, error)
	Status(ctx context.Context, endpoint string) (*clientv3.StatusResponse, error)
}

type podLister interface {
	List(ctx context.Context, opts metav1.ListOptions) (*corev1.PodList, error)
}

type etcdConnection struct {
	client   *clientv3.Client
	stopChn  chan struct{}
	endpoint string
}

func (c *etcdConnection) close() {
	c.client.Close()
	close(c.stopChn)
}

func connectToEtcd(ctx context.Context, kubeconfig []byte, excludeNodeName string) (*etcdConnection, error) {
	setupCtx, cancel := context.WithTimeout(ctx, etcdSetupTimeout)
	defer cancel()

	clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get rest config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	tlsConfig, err := buildEtcdTLSConfig(setupCtx, clientset)
	if err != nil {
		return nil, fmt.Errorf("failed to build etcd TLS config: %w", err)
	}

	podName, err := findRunningEtcdPod(setupCtx, clientset.CoreV1().Pods(etcdPodNamespace), excludeNodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to find running etcd pod: %w", err)
	}

	localPort, stopChan, err := startPortForward(setupCtx, restConfig, clientset, podName)
	if err != nil {
		return nil, fmt.Errorf("failed to start port-forward to etcd: %w", err)
	}

	endpoint := fmt.Sprintf("https://localhost:%d", localPort)
	etcdClient, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{endpoint},
		TLS:         tlsConfig,
		DialTimeout: etcdDialTimeout,
	})
	if err != nil {
		close(stopChan)
		return nil, fmt.Errorf("failed to create etcd client: %w", err)
	}

	return &etcdConnection{client: etcdClient, stopChn: stopChan, endpoint: endpoint}, nil
}

func (w *WorkloadClusterClientGenerator) RemoveEtcdMember(ctx context.Context, kubeconfig []byte, memberName string) error {
	conn, err := connectToEtcd(ctx, kubeconfig, memberName)
	if err != nil {
		return err
	}
	defer conn.close()

	opCtx, cancel := context.WithTimeout(ctx, etcdOpTimeout)
	defer cancel()

	return removeMemberByName(opCtx, conn.client, memberName)
}

func (w *WorkloadClusterClientGenerator) ListEtcdMembers(ctx context.Context, kubeconfig []byte) ([]EtcdMember, error) {
	conn, err := connectToEtcd(ctx, kubeconfig, "")
	if err != nil {
		return nil, err
	}
	defer conn.close()

	opCtx, cancel := context.WithTimeout(ctx, etcdOpTimeout)
	defer cancel()

	return listMembers(opCtx, conn.client)
}

func (w *WorkloadClusterClientGenerator) RemoveEtcdMemberByID(ctx context.Context, kubeconfig []byte, memberID uint64) error {
	conn, err := connectToEtcd(ctx, kubeconfig, "")
	if err != nil {
		return err
	}
	defer conn.close()

	opCtx, cancel := context.WithTimeout(ctx, etcdOpTimeout)
	defer cancel()

	return removeMemberByID(opCtx, conn.client, memberID)
}

func (w *WorkloadClusterClientGenerator) ForwardEtcdLeadership(ctx context.Context, kubeconfig []byte, fromMemberName, toMemberName string) error {
	conn, err := connectToEtcd(ctx, kubeconfig, "")
	if err != nil {
		return err
	}
	defer conn.close()

	opCtx, cancel := context.WithTimeout(ctx, etcdOpTimeout)
	defer cancel()

	return forwardLeadership(opCtx, conn.client, conn.endpoint, fromMemberName, toMemberName)
}

func listMembers(ctx context.Context, client etcdMemberManager) ([]EtcdMember, error) {
	resp, err := client.MemberList(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list etcd members: %w", err)
	}

	members := make([]EtcdMember, 0, len(resp.Members))
	for _, m := range resp.Members {
		members = append(members, EtcdMember{ID: m.ID, Name: m.Name})
	}
	return members, nil
}

func removeMemberByID(ctx context.Context, client etcdMemberManager, memberID uint64) error {
	memberListResp, err := client.MemberList(ctx)
	if err != nil {
		return fmt.Errorf("failed to list etcd members: %w", err)
	}

	if len(memberListResp.Members) == 1 {
		return fmt.Errorf("refusing to remove the last etcd member (id: %d)", memberID)
	}

	alarmResp, err := client.AlarmList(ctx)
	if err != nil {
		return fmt.Errorf("failed to check etcd alarms: %w", err)
	}
	for _, alarm := range alarmResp.Alarms {
		if alarm.Alarm == etcdserverpb.AlarmType_CORRUPT {
			return fmt.Errorf("etcd cluster has CORRUPT alarm, unsafe to remove member (id: %d)", memberID)
		}
	}

	_, err = client.MemberRemove(ctx, memberID)
	if err != nil {
		return fmt.Errorf("failed to remove etcd member (id: %d): %w", memberID, err)
	}

	return nil
}

func removeMemberByName(ctx context.Context, client etcdMemberManager, memberName string) error {
	memberListResp, err := client.MemberList(ctx)
	if err != nil {
		return fmt.Errorf("failed to list etcd members: %w", err)
	}

	var targetMemberID uint64
	found := false
	for _, member := range memberListResp.Members {
		if member.Name == memberName {
			targetMemberID = member.ID
			found = true
			break
		}
	}

	if !found {
		return nil
	}

	if len(memberListResp.Members) == 1 {
		return fmt.Errorf("refusing to remove the last etcd member %q", memberName)
	}

	alarmResp, err := client.AlarmList(ctx)
	if err != nil {
		return fmt.Errorf("failed to check etcd alarms: %w", err)
	}
	for _, alarm := range alarmResp.Alarms {
		if alarm.Alarm == etcdserverpb.AlarmType_CORRUPT {
			return fmt.Errorf("etcd cluster has CORRUPT alarm, unsafe to remove member %q", memberName)
		}
	}

	_, err = client.MemberRemove(ctx, targetMemberID)
	if err != nil {
		return fmt.Errorf("failed to remove etcd member %q: %w", memberName, err)
	}

	return nil
}

func forwardLeadership(ctx context.Context, client etcdMemberManager, endpoint, fromMemberName, toMemberName string) error {
	memberListResp, err := client.MemberList(ctx)
	if err != nil {
		return fmt.Errorf("failed to list etcd members: %w", err)
	}

	var fromMemberID, toMemberID uint64
	var currentLeaderID uint64
	foundFrom := false
	foundTo := false

	for _, member := range memberListResp.Members {
		if member.Name == fromMemberName {
			fromMemberID = member.ID
			foundFrom = true
		}
		if member.Name == toMemberName {
			toMemberID = member.ID
			foundTo = true
		}
		if foundFrom && foundTo {
			break
		}
	}

	if !foundFrom {
		// Source member not in etcd cluster - already removed or never joined.
		// Nothing to forward, return success.
		return nil
	}
	if !foundTo {
		return fmt.Errorf("target member %q not found in etcd cluster", toMemberName)
	}

	statusResp, err := client.Status(ctx, endpoint)
	if err != nil {
		return fmt.Errorf("failed to get etcd status: %w", err)
	}
	currentLeaderID = statusResp.Leader

	if currentLeaderID != fromMemberID {
		// No-op: fromMember is not the current leader
		return nil
	}

	_, err = client.MoveLeader(ctx, toMemberID)
	if err != nil {
		return fmt.Errorf("failed to move etcd leadership from %q to %q: %w", fromMemberName, toMemberName, err)
	}

	return nil
}

func buildEtcdTLSConfig(ctx context.Context, clientset kubernetes.Interface) (*tls.Config, error) {
	secret, err := clientset.CoreV1().Secrets(etcdTLSSecretNamespace).Get(ctx, etcdClientSecretName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get etcd client secret: %w", err)
	}

	certData := secret.Data["tls.crt"]
	keyData := secret.Data["tls.key"]
	clientCert, err := tls.X509KeyPair(certData, keyData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse etcd client cert: %w", err)
	}

	cm, err := clientset.CoreV1().ConfigMaps(etcdTLSSecretNamespace).Get(ctx, etcdCAConfigMapName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get etcd CA bundle: %w", err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM([]byte(cm.Data["ca-bundle.crt"])) {
		return nil, fmt.Errorf("failed to parse etcd CA bundle")
	}

	return &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caPool,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

func findRunningEtcdPod(ctx context.Context, pods podLister, excludeNodeName string) (string, error) {
	podList, err := pods.List(ctx, metav1.ListOptions{
		LabelSelector: "app=etcd",
	})
	if err != nil {
		return "", fmt.Errorf("failed to list etcd pods: %w", err)
	}

	var fallback string
	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.Status.Phase != corev1.PodRunning || pod.DeletionTimestamp != nil {
			continue
		}
		if !isPodReady(pod) {
			continue
		}
		if pod.Spec.NodeName == excludeNodeName {
			fallback = pod.Name
			continue
		}
		return pod.Name, nil
	}

	if fallback != "" {
		return fallback, nil
	}

	return "", fmt.Errorf("no running etcd pod found in namespace %s", etcdPodNamespace)
}

func isPodReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}

func startPortForward(ctx context.Context, restConfig *rest.Config, clientset kubernetes.Interface, podName string) (int, chan struct{}, error) {
	localPort, err := getFreePort()
	if err != nil {
		return 0, nil, fmt.Errorf("failed to get free port: %w", err)
	}

	transport, upgrader, err := spdy.RoundTripperFor(restConfig)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to create round tripper: %w", err)
	}

	url := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(etcdPodNamespace).
		Name(podName).
		SubResource("portforward").
		URL()

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, url)

	stopChan := make(chan struct{})
	readyChan := make(chan struct{})

	fw, err := portforward.New(dialer, []string{fmt.Sprintf("%d:%d", localPort, etcdClientPort)}, stopChan, readyChan, io.Discard, io.Discard)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to create port forwarder: %w", err)
	}

	errChan := make(chan error, 1)
	go func() {
		errChan <- fw.ForwardPorts()
	}()

	readyTimeout := time.NewTimer(etcdDialTimeout)
	defer readyTimeout.Stop()

	select {
	case err := <-errChan:
		return 0, nil, fmt.Errorf("port-forward failed: %w", err)
	case <-readyChan:
		return localPort, stopChan, nil
	case <-readyTimeout.C:
		close(stopChan)
		return 0, nil, fmt.Errorf("port-forward did not become ready within %s", etcdDialTimeout)
	case <-ctx.Done():
		close(stopChan)
		return 0, nil, fmt.Errorf("port-forward cancelled: %w", ctx.Err())
	}
}

func getFreePort() (int, error) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}
