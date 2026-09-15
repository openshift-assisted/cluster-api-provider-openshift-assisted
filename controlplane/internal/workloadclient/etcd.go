package workloadclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	logutil "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util/log"
	"go.etcd.io/etcd/api/v3/etcdserverpb"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	ctrl "sigs.k8s.io/controller-runtime"
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
	client    etcdMemberManager
	stopChn   chan struct{}
	endpoint  string
	podName   string
	nodeName  string
	closeFunc func()
	closeOnce sync.Once
}

func (c *etcdConnection) close() {
	c.closeOnce.Do(func() {
		if c.closeFunc != nil {
			c.closeFunc()
			return
		}
		if client, ok := c.client.(interface{ Close() error }); ok {
			_ = client.Close()
		}
		if c.stopChn != nil {
			close(c.stopChn)
		}
	})
}

type etcdConnectionFactory func(ctx context.Context, kubeconfig []byte, excludeNodeName, targetNodeName string) (*etcdConnection, error)

func defaultEtcdConnectionFactory(ctx context.Context, kubeconfig []byte, excludeNodeName, targetNodeName string) (*etcdConnection, error) {
	if targetNodeName != "" {
		return connectToEtcdOnNode(ctx, kubeconfig, targetNodeName)
	}
	return connectToEtcd(ctx, kubeconfig, excludeNodeName)
}

func connectToEtcd(ctx context.Context, kubeconfig []byte, excludeNodeName string) (*etcdConnection, error) {
	return connectToEtcdWithSelector(ctx, kubeconfig, func(selectionCtx context.Context, pods podLister) (*corev1.Pod, error) {
		return selectRunningEtcdPod(selectionCtx, pods, excludeNodeName, "")
	})
}

func connectToEtcdOnNode(ctx context.Context, kubeconfig []byte, nodeName string) (*etcdConnection, error) {
	return connectToEtcdWithSelector(ctx, kubeconfig, func(selectionCtx context.Context, pods podLister) (*corev1.Pod, error) {
		return selectRunningEtcdPod(selectionCtx, pods, "", nodeName)
	})
}

func connectToEtcdWithSelector(ctx context.Context, kubeconfig []byte, selectPod func(context.Context, podLister) (*corev1.Pod, error)) (*etcdConnection, error) {
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

	pod, err := selectPod(setupCtx, clientset.CoreV1().Pods(etcdPodNamespace))
	if err != nil {
		return nil, fmt.Errorf("failed to find running etcd pod: %w", err)
	}

	log := ctrl.LoggerFrom(ctx)
	log.V(logutil.DebugLevel).Info("selected etcd pod", "pod", pod.Name, "node", pod.Spec.NodeName)

	localPort, stopChan, err := startPortForward(setupCtx, restConfig, clientset, pod.Name)
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

	return &etcdConnection{
		client:   etcdClient,
		stopChn:  stopChan,
		endpoint: endpoint,
		podName:  pod.Name,
		nodeName: pod.Spec.NodeName,
	}, nil
}

func (w *WorkloadClusterClientGenerator) connectToEtcd(ctx context.Context, kubeconfig []byte, excludeNodeName, targetNodeName string) (*etcdConnection, error) {
	etcdConnectionFactory := w.etcdConnectionFactory
	if etcdConnectionFactory == nil {
		etcdConnectionFactory = defaultEtcdConnectionFactory
	}
	return etcdConnectionFactory(ctx, kubeconfig, excludeNodeName, targetNodeName)
}

func (w *WorkloadClusterClientGenerator) RemoveEtcdMember(ctx context.Context, kubeconfig []byte, memberName string) error {
	conn, err := w.connectToEtcd(ctx, kubeconfig, memberName, "")
	if err != nil {
		return err
	}
	defer conn.close()

	opCtx, cancel := context.WithTimeout(ctx, etcdOpTimeout)
	defer cancel()

	return removeMemberByName(opCtx, conn.client, memberName)
}

func (w *WorkloadClusterClientGenerator) ListEtcdMembers(ctx context.Context, kubeconfig []byte) ([]EtcdMember, error) {
	conn, err := w.connectToEtcd(ctx, kubeconfig, "", "")
	if err != nil {
		return nil, err
	}
	defer conn.close()

	opCtx, cancel := context.WithTimeout(ctx, etcdOpTimeout)
	defer cancel()

	return listMembers(opCtx, conn.client)
}

func (w *WorkloadClusterClientGenerator) RemoveEtcdMemberByID(ctx context.Context, kubeconfig []byte, memberID uint64) error {
	conn, err := w.connectToEtcd(ctx, kubeconfig, "", "")
	if err != nil {
		return err
	}
	defer conn.close()

	opCtx, cancel := context.WithTimeout(ctx, etcdOpTimeout)
	defer cancel()

	return removeMemberByID(opCtx, conn.client, memberID)
}

// ProtectEtcdLeadership transfers leadership away from the source member before removal when needed, and otherwise no-ops.
func (w *WorkloadClusterClientGenerator) ProtectEtcdLeadership(ctx context.Context, kubeconfig []byte, fromMemberName, toMemberName string) error {
	conn, err := w.connectToEtcd(ctx, kubeconfig, "", "")
	if err != nil {
		return err
	}
	defer conn.close()

	opCtx, cancel := context.WithTimeout(ctx, etcdOpTimeout)
	defer cancel()

	return protectEtcdLeadership(opCtx, conn, func(ctx context.Context, nodeName string) (*etcdConnection, error) {
		return w.connectToEtcd(ctx, kubeconfig, "", nodeName)
	}, fromMemberName, toMemberName)
}

func listMembers(ctx context.Context, client etcdMemberManager) ([]EtcdMember, error) {
	resp, err := client.MemberList(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list etcd members: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("failed to list etcd members: empty response")
	}

	members := make([]EtcdMember, 0, len(resp.Members))
	for _, m := range resp.Members {
		if m == nil {
			continue
		}
		members = append(members, EtcdMember{ID: m.ID, Name: m.Name})
	}
	return members, nil
}

func removeMemberByID(ctx context.Context, client etcdMemberManager, memberID uint64) error {
	memberListResp, err := client.MemberList(ctx)
	if err != nil {
		return fmt.Errorf("failed to list etcd members: %w", err)
	}
	if memberListResp == nil {
		return fmt.Errorf("failed to list etcd members: empty response")
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
	if memberListResp == nil {
		return fmt.Errorf("failed to list etcd members: empty response")
	}

	var targetMemberID uint64
	found := false
	for _, member := range memberListResp.Members {
		if member == nil {
			continue
		}
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

func protectEtcdLeadership(ctx context.Context, discoveryConn *etcdConnection, connectSource func(context.Context, string) (*etcdConnection, error), fromMemberName, toMemberName string) error {
	memberListResp, err := discoveryConn.client.MemberList(ctx)
	if err != nil {
		return fmt.Errorf("failed to list etcd members: %w", err)
	}
	if memberListResp == nil {
		return fmt.Errorf("failed to list etcd members: empty response")
	}

	fromMember := findEtcdMember(memberListResp.Members, fromMemberName)
	if fromMember == nil {
		// Source member not in etcd cluster - already removed or never joined.
		// Nothing to forward, return success.
		return nil
	}

	statusResp, err := discoveryConn.client.Status(ctx, discoveryConn.endpoint)
	if err != nil {
		return fmt.Errorf("failed to get etcd status: %w", err)
	}
	if statusResp == nil {
		return fmt.Errorf("failed to get etcd status: empty response")
	}
	log := ctrl.LoggerFrom(ctx)
	log.V(logutil.DebugLevel).Info("received etcd status", "pod", discoveryConn.podName, "node", discoveryConn.nodeName,
		"memberID", statusMemberID(statusResp), "leaderID", statusResp.Leader,
		"source", fromMemberName, "target", toMemberName)

	if statusResp.Leader == 0 {
		return fmt.Errorf("etcd has no elected leader while protecting leadership from %q to %q", fromMemberName, toMemberName)
	}
	if statusResp.Leader != fromMember.ID {
		// No-op: fromMember is not the current leader
		return nil
	}

	// MoveLeader must be sent to the current leader. The discovery connection can
	// be connected to any healthy member, so reconnect to the source member's pod.
	discoveryConn.close()
	sourceConn, err := connectSource(ctx, fromMemberName)
	if err != nil {
		return fmt.Errorf("failed to connect to etcd source member %q: %w", fromMemberName, err)
	}
	defer sourceConn.close()

	refreshedMembers, err := sourceConn.client.MemberList(ctx)
	if err != nil {
		return fmt.Errorf("failed to refresh etcd members through source member %q: %w", fromMemberName, err)
	}
	if refreshedMembers == nil {
		return fmt.Errorf("failed to refresh etcd members through source member %q: empty response", fromMemberName)
	}
	refreshedSource := findEtcdMember(refreshedMembers.Members, fromMemberName)
	if refreshedSource == nil {
		return nil
	}

	statusResp, err = sourceConn.client.Status(ctx, sourceConn.endpoint)
	if err != nil {
		return fmt.Errorf("failed to get etcd source member %q status: %w", fromMemberName, err)
	}
	if statusResp == nil {
		return fmt.Errorf("failed to get etcd source member %q status: empty response", fromMemberName)
	}
	log.V(logutil.DebugLevel).Info("received etcd source status", "pod", sourceConn.podName, "node", sourceConn.nodeName,
		"memberID", statusMemberID(statusResp), "leaderID", statusResp.Leader,
		"source", fromMemberName, "target", toMemberName)
	if refreshedSource.ID == 0 || statusResp.Header == nil || statusResp.Header.MemberId == 0 || statusResp.Header.MemberId != refreshedSource.ID {
		return fmt.Errorf("etcd status member ID %d does not match source member %q ID %d", statusMemberID(statusResp), fromMemberName, refreshedSource.ID)
	}
	if statusResp.Leader == 0 {
		return fmt.Errorf("etcd has no elected leader while protecting leadership from %q to %q", fromMemberName, toMemberName)
	}
	if statusResp.Leader != refreshedSource.ID {
		return nil
	}

	targetMember := findEtcdMember(refreshedMembers.Members, toMemberName)
	if targetMember == nil {
		return fmt.Errorf("target member %q not found in etcd cluster", toMemberName)
	}
	if targetMember.ID == 0 {
		return fmt.Errorf("target member %q has invalid ID 0", toMemberName)
	}
	if targetMember.ID == refreshedSource.ID {
		return fmt.Errorf("target member %q is the source member %q", toMemberName, fromMemberName)
	}
	if targetMember.IsLearner {
		return fmt.Errorf("target member %q is a learner and cannot receive leadership", toMemberName)
	}

	log.V(logutil.InfoLevel).Info("transferring etcd leadership", "pod", sourceConn.podName, "node", sourceConn.nodeName,
		"memberID", refreshedSource.ID, "leaderID", statusResp.Leader,
		"source", fromMemberName, "target", toMemberName)
	_, err = sourceConn.client.MoveLeader(ctx, targetMember.ID)
	if err != nil {
		if !errors.Is(err, rpctypes.ErrNotLeader) {
			return fmt.Errorf("failed to move etcd leadership from %q to %q: %w", fromMemberName, toMemberName, err)
		}

		statusResp, statusErr := sourceConn.client.Status(ctx, sourceConn.endpoint)
		if statusErr != nil {
			return fmt.Errorf("failed to refresh etcd status after moving leadership from %q to %q: %w", fromMemberName, toMemberName, statusErr)
		}
		if statusResp == nil {
			return fmt.Errorf("failed to refresh etcd status after moving leadership from %q to %q: empty response", fromMemberName, toMemberName)
		}
		log.V(logutil.DebugLevel).Info("received etcd status after failed leadership transfer", "pod", sourceConn.podName, "node", sourceConn.nodeName,
			"memberID", statusMemberID(statusResp), "leaderID", statusResp.Leader,
			"source", fromMemberName, "target", toMemberName)
		if statusResp.Leader != 0 && statusResp.Leader != refreshedSource.ID {
			return nil
		}

		return fmt.Errorf("failed to move etcd leadership from %q to %q: %w", fromMemberName, toMemberName, err)
	}

	return nil
}

func findEtcdMember(members []*etcdserverpb.Member, name string) *etcdserverpb.Member {
	for _, member := range members {
		if member == nil {
			continue
		}
		if member.Name == name {
			return member
		}
	}
	return nil
}

func statusMemberID(status *clientv3.StatusResponse) uint64 {
	if status == nil || status.Header == nil {
		return 0
	}
	return status.Header.MemberId
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
	pod, err := selectRunningEtcdPod(ctx, pods, excludeNodeName, "")
	if err != nil {
		return "", err
	}
	return pod.Name, nil
}

func findRunningEtcdPodOnNode(ctx context.Context, pods podLister, nodeName string) (string, error) {
	pod, err := selectRunningEtcdPod(ctx, pods, "", nodeName)
	if err != nil {
		return "", err
	}
	return pod.Name, nil
}

func selectRunningEtcdPod(ctx context.Context, pods podLister, excludeNodeName, targetNodeName string) (*corev1.Pod, error) {
	podList, err := pods.List(ctx, metav1.ListOptions{
		LabelSelector: "app=etcd",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list etcd pods: %w", err)
	}

	var fallback *corev1.Pod
	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.Status.Phase != corev1.PodRunning || pod.DeletionTimestamp != nil {
			continue
		}
		if !isPodReady(pod) {
			continue
		}
		if targetNodeName != "" {
			if pod.Spec.NodeName == targetNodeName {
				return pod, nil
			}
			continue
		}
		if pod.Spec.NodeName == excludeNodeName {
			fallback = pod
			continue
		}
		return pod, nil
	}

	if fallback != nil {
		return fallback, nil
	}

	if targetNodeName != "" {
		return nil, fmt.Errorf("no running etcd pod found on node %s in namespace %s", targetNodeName, etcdPodNamespace)
	}
	return nil, fmt.Errorf("no running etcd pod found in namespace %s", etcdPodNamespace)
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
