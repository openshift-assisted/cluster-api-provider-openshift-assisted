package workloadclient

import (
	"context"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.etcd.io/etcd/api/v3/etcdserverpb"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEtcd(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Etcd Suite")
}

type mockEtcdClient struct {
	members              []*etcdserverpb.Member
	memberListResponses  []*clientv3.MemberListResponse
	alarms               []*etcdserverpb.AlarmMember
	memberListErr        error
	alarmListErr         error
	memberRemoveErr      error
	removedID            uint64
	leaderID             uint64
	memberID             uint64
	statusResponses      []*clientv3.StatusResponse
	moveLeaderErr        error
	movedToID            uint64
	moveLeaderCalls      []uint64
	statusErr            error
	statusCalls          int
	statusEndpoints      []string
	memberListCalls      int
	memberListCallErrors []error
}

func (m *mockEtcdClient) MemberList(_ context.Context, _ ...clientv3.OpOption) (*clientv3.MemberListResponse, error) {
	m.memberListCalls++
	if m.memberListErr != nil {
		return nil, m.memberListErr
	}
	if len(m.memberListCallErrors) >= m.memberListCalls && m.memberListCallErrors[m.memberListCalls-1] != nil {
		return nil, m.memberListCallErrors[m.memberListCalls-1]
	}
	if len(m.memberListResponses) > 0 {
		index := m.memberListCalls - 1
		if index >= len(m.memberListResponses) {
			index = len(m.memberListResponses) - 1
		}
		return m.memberListResponses[index], nil
	}
	resp := clientv3.MemberListResponse{}
	resp.Members = m.members
	return &resp, nil
}

func (m *mockEtcdClient) MemberRemove(_ context.Context, id uint64) (*clientv3.MemberRemoveResponse, error) {
	m.removedID = id
	if m.memberRemoveErr != nil {
		return nil, m.memberRemoveErr
	}
	return &clientv3.MemberRemoveResponse{}, nil
}

func (m *mockEtcdClient) AlarmList(_ context.Context) (*clientv3.AlarmResponse, error) {
	if m.alarmListErr != nil {
		return nil, m.alarmListErr
	}
	resp := clientv3.AlarmResponse{}
	resp.Alarms = m.alarms
	return &resp, nil
}

func (m *mockEtcdClient) MoveLeader(_ context.Context, transfereeID uint64) (*clientv3.MoveLeaderResponse, error) {
	m.movedToID = transfereeID
	m.moveLeaderCalls = append(m.moveLeaderCalls, transfereeID)
	if m.moveLeaderErr != nil {
		return nil, m.moveLeaderErr
	}
	return &clientv3.MoveLeaderResponse{}, nil
}

func (m *mockEtcdClient) Status(_ context.Context, endpoint string) (*clientv3.StatusResponse, error) {
	m.statusCalls++
	m.statusEndpoints = append(m.statusEndpoints, endpoint)
	if m.statusErr != nil {
		return nil, m.statusErr
	}
	if len(m.statusResponses) > 0 {
		index := m.statusCalls - 1
		if index >= len(m.statusResponses) {
			index = len(m.statusResponses) - 1
		}
		return m.statusResponses[index], nil
	}
	resp := &clientv3.StatusResponse{
		Header: &etcdserverpb.ResponseHeader{MemberId: m.memberID},
		Leader: m.leaderID,
	}
	return resp, nil
}

type mockPodLister struct {
	pods        *corev1.PodList
	err         error
	listOptions metav1.ListOptions
}

func (m *mockPodLister) List(_ context.Context, opts metav1.ListOptions) (*corev1.PodList, error) {
	m.listOptions = opts
	if m.err != nil {
		return nil, m.err
	}
	return m.pods, nil
}

func etcdPod(name, nodeName string, phase corev1.PodPhase, deleting bool) corev1.Pod {
	return etcdPodWithReadiness(name, nodeName, phase, deleting, true)
}

func etcdPodWithReadiness(name, nodeName string, phase corev1.PodPhase, deleting, ready bool) corev1.Pod {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: etcdPodNamespace,
			Labels:    map[string]string{"app": "etcd"},
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
		},
		Status: corev1.PodStatus{
			Phase: phase,
		},
	}
	if ready {
		pod.Status.Conditions = []corev1.PodCondition{
			{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			},
		}
	}
	if deleting {
		now := metav1.Now()
		pod.DeletionTimestamp = &now
		pod.Finalizers = []string{"test"}
	}
	return pod
}

var _ = Describe("findRunningEtcdPod", func() {
	ctx := context.Background()

	It("should return a pod not on the excluded node", func() {
		lister := &mockPodLister{pods: &corev1.PodList{Items: []corev1.Pod{
			etcdPod("etcd-0", "node-0", corev1.PodRunning, false),
			etcdPod("etcd-1", "node-1", corev1.PodRunning, false),
		}}}
		pod, err := findRunningEtcdPod(ctx, lister, "node-0")
		Expect(err).NotTo(HaveOccurred())
		Expect(pod).To(Equal("etcd-1"))
		Expect(lister.listOptions.LabelSelector).To(Equal("app=etcd"))
	})

	It("should fall back to the excluded node when no other running pods exist", func() {
		lister := &mockPodLister{pods: &corev1.PodList{Items: []corev1.Pod{
			etcdPod("etcd-0", "node-0", corev1.PodRunning, false),
		}}}
		pod, err := findRunningEtcdPod(ctx, lister, "node-0")
		Expect(err).NotTo(HaveOccurred())
		Expect(pod).To(Equal("etcd-0"))
	})

	It("should skip non-running pods", func() {
		lister := &mockPodLister{pods: &corev1.PodList{Items: []corev1.Pod{
			etcdPod("etcd-0", "node-0", corev1.PodRunning, false),
			etcdPod("etcd-1", "node-1", corev1.PodPending, false),
		}}}
		pod, err := findRunningEtcdPod(ctx, lister, "node-0")
		Expect(err).NotTo(HaveOccurred())
		Expect(pod).To(Equal("etcd-0"))
	})

	It("should skip pods with a deletion timestamp", func() {
		lister := &mockPodLister{pods: &corev1.PodList{Items: []corev1.Pod{
			etcdPod("etcd-0", "node-0", corev1.PodRunning, false),
			etcdPod("etcd-1", "node-1", corev1.PodRunning, true),
		}}}
		pod, err := findRunningEtcdPod(ctx, lister, "node-0")
		Expect(err).NotTo(HaveOccurred())
		Expect(pod).To(Equal("etcd-0"))
	})

	It("should return an error when no running pods exist", func() {
		lister := &mockPodLister{pods: &corev1.PodList{Items: []corev1.Pod{
			etcdPod("etcd-0", "node-0", corev1.PodPending, false),
		}}}
		_, err := findRunningEtcdPod(ctx, lister, "node-1")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no running etcd pod found"))
	})

	It("should return an error when no pods exist at all", func() {
		lister := &mockPodLister{pods: &corev1.PodList{}}
		_, err := findRunningEtcdPod(ctx, lister, "node-0")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no running etcd pod found"))
	})

	It("should skip non-ready pods", func() {
		lister := &mockPodLister{pods: &corev1.PodList{Items: []corev1.Pod{
			etcdPodWithReadiness("etcd-0", "node-0", corev1.PodRunning, false, false),
			etcdPodWithReadiness("etcd-1", "node-1", corev1.PodRunning, false, true),
		}}}
		pod, err := findRunningEtcdPod(ctx, lister, "node-0")
		Expect(err).NotTo(HaveOccurred())
		Expect(pod).To(Equal("etcd-1"))
	})

	It("should fall back to excluded node if it is the only ready pod", func() {
		lister := &mockPodLister{pods: &corev1.PodList{Items: []corev1.Pod{
			etcdPodWithReadiness("etcd-0", "node-0", corev1.PodRunning, false, true),
			etcdPodWithReadiness("etcd-1", "node-1", corev1.PodRunning, false, false),
			etcdPodWithReadiness("etcd-2", "node-2", corev1.PodRunning, false, false),
		}}}
		pod, err := findRunningEtcdPod(ctx, lister, "node-0")
		Expect(err).NotTo(HaveOccurred())
		Expect(pod).To(Equal("etcd-0"))
	})

	It("should return an error when no ready pods exist", func() {
		lister := &mockPodLister{pods: &corev1.PodList{Items: []corev1.Pod{
			etcdPodWithReadiness("etcd-0", "node-0", corev1.PodRunning, false, false),
			etcdPodWithReadiness("etcd-1", "node-1", corev1.PodRunning, false, false),
		}}}
		_, err := findRunningEtcdPod(ctx, lister, "node-2")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no running etcd pod found"))
	})

	It("should select a ready pod on the exact node without falling back", func() {
		lister := &mockPodLister{pods: &corev1.PodList{Items: []corev1.Pod{
			etcdPod("etcd-0", "node-0", corev1.PodRunning, false),
			etcdPod("etcd-1", "node-1", corev1.PodRunning, false),
		}}}
		pod, err := findRunningEtcdPodOnNode(ctx, lister, "node-1")
		Expect(err).NotTo(HaveOccurred())
		Expect(pod).To(Equal("etcd-1"))
	})

	It("should reject an unavailable exact node even when another pod is ready", func() {
		lister := &mockPodLister{pods: &corev1.PodList{Items: []corev1.Pod{
			etcdPod("etcd-0", "node-0", corev1.PodRunning, false),
		}}}
		_, err := findRunningEtcdPodOnNode(ctx, lister, "node-1")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no running etcd pod found on node node-1"))
	})

	It("should reject unsuitable pods on the exact node", func() {
		lister := &mockPodLister{pods: &corev1.PodList{Items: []corev1.Pod{
			etcdPodWithReadiness("etcd-0", "node-1", corev1.PodPending, false, true),
			etcdPodWithReadiness("etcd-1", "node-1", corev1.PodRunning, true, true),
			etcdPodWithReadiness("etcd-2", "node-1", corev1.PodRunning, false, false),
			etcdPod("etcd-3", "node-0", corev1.PodRunning, false),
		}}}
		_, err := findRunningEtcdPodOnNode(ctx, lister, "node-1")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no running etcd pod found on node node-1"))
	})
})

var _ = Describe("removeMemberByName", func() {
	ctx := context.Background()

	It("should remove the member successfully", func() {
		mock := &mockEtcdClient{
			members: []*etcdserverpb.Member{
				{ID: 1, Name: "node-0"},
				{ID: 2, Name: "node-1"},
				{ID: 3, Name: "node-2"},
			},
		}
		err := removeMemberByName(ctx, mock, "node-1")
		Expect(err).NotTo(HaveOccurred())
		Expect(mock.removedID).To(Equal(uint64(2)))
	})

	It("should return nil when member is not found", func() {
		mock := &mockEtcdClient{
			members: []*etcdserverpb.Member{
				{ID: 1, Name: "node-0"},
				{ID: 2, Name: "node-1"},
			},
		}
		err := removeMemberByName(ctx, mock, "node-99")
		Expect(err).NotTo(HaveOccurred())
		Expect(mock.removedID).To(Equal(uint64(0)))
	})

	It("should refuse to remove the last member", func() {
		mock := &mockEtcdClient{
			members: []*etcdserverpb.Member{
				{ID: 1, Name: "node-0"},
			},
		}
		err := removeMemberByName(ctx, mock, "node-0")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("refusing to remove the last etcd member"))
	})

	It("should refuse to remove a member when CORRUPT alarm is active", func() {
		mock := &mockEtcdClient{
			members: []*etcdserverpb.Member{
				{ID: 1, Name: "node-0"},
				{ID: 2, Name: "node-1"},
			},
			alarms: []*etcdserverpb.AlarmMember{
				{Alarm: etcdserverpb.AlarmType_CORRUPT},
			},
		}
		err := removeMemberByName(ctx, mock, "node-0")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("CORRUPT alarm"))
		Expect(mock.removedID).To(Equal(uint64(0)))
	})

	It("should allow removal when non-CORRUPT alarms are active", func() {
		mock := &mockEtcdClient{
			members: []*etcdserverpb.Member{
				{ID: 1, Name: "node-0"},
				{ID: 2, Name: "node-1"},
			},
			alarms: []*etcdserverpb.AlarmMember{
				{Alarm: etcdserverpb.AlarmType_NOSPACE},
			},
		}
		err := removeMemberByName(ctx, mock, "node-0")
		Expect(err).NotTo(HaveOccurred())
		Expect(mock.removedID).To(Equal(uint64(1)))
	})

	It("should propagate MemberList errors", func() {
		mock := &mockEtcdClient{
			memberListErr: fmt.Errorf("connection refused"),
		}
		err := removeMemberByName(ctx, mock, "node-0")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to list etcd members"))
	})

	It("should propagate AlarmList errors", func() {
		mock := &mockEtcdClient{
			members: []*etcdserverpb.Member{
				{ID: 1, Name: "node-0"},
				{ID: 2, Name: "node-1"},
			},
			alarmListErr: fmt.Errorf("connection refused"),
		}
		err := removeMemberByName(ctx, mock, "node-0")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to check etcd alarms"))
	})

	It("should propagate MemberRemove errors", func() {
		mock := &mockEtcdClient{
			members: []*etcdserverpb.Member{
				{ID: 1, Name: "node-0"},
				{ID: 2, Name: "node-1"},
			},
			memberRemoveErr: fmt.Errorf("not leader"),
		}
		err := removeMemberByName(ctx, mock, "node-0")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to remove etcd member"))
	})
})

func statusResponse(memberID, leaderID uint64) *clientv3.StatusResponse {
	return &clientv3.StatusResponse{
		Header: &etcdserverpb.ResponseHeader{MemberId: memberID},
		Leader: leaderID,
	}
}

func testEtcdConnection(client *mockEtcdClient, endpoint, podName, nodeName string, closed *bool) *etcdConnection {
	return &etcdConnection{
		client:   client,
		endpoint: endpoint,
		podName:  podName,
		nodeName: nodeName,
		closeFunc: func() {
			*closed = true
		},
	}
}

var _ = Describe("protectEtcdLeadership", func() {
	ctx := context.Background()

	It("should send MoveLeader through the source member connection", func() {
		members := []*etcdserverpb.Member{
			{ID: 1, Name: "node-a"},
			{ID: 2, Name: "node-b"},
			{ID: 3, Name: "node-c"},
		}
		discoveryClient := &mockEtcdClient{
			members:         members,
			statusResponses: []*clientv3.StatusResponse{statusResponse(2, 1)},
		}
		sourceClient := &mockEtcdClient{
			members:         members,
			statusResponses: []*clientv3.StatusResponse{statusResponse(1, 1)},
		}
		var discoveryClosed, sourceClosed bool
		discovery := testEtcdConnection(discoveryClient, "https://discovery", "etcd-b", "node-b", &discoveryClosed)
		source := testEtcdConnection(sourceClient, "https://source", "etcd-a", "node-a", &sourceClosed)
		var targetNode string
		generator := &WorkloadClusterClientGenerator{
			etcdConnectionFactory: func(_ context.Context, _ []byte, excludeNodeName, targetNodeName string) (*etcdConnection, error) {
				Expect(excludeNodeName).To(BeEmpty())
				if targetNodeName == "" {
					return discovery, nil
				}
				targetNode = targetNodeName
				return source, nil
			},
		}

		Expect(generator.ProtectEtcdLeadership(ctx, nil, "node-a", "node-c")).To(Succeed())
		Expect(targetNode).To(Equal("node-a"))
		Expect(discoveryClient.moveLeaderCalls).To(BeEmpty())
		Expect(sourceClient.moveLeaderCalls).To(Equal([]uint64{3}))
		Expect(discoveryClosed).To(BeTrue())
		Expect(sourceClosed).To(BeTrue())
	})

	It("should succeed when the source member is absent", func() {
		discoveryClient := &mockEtcdClient{members: []*etcdserverpb.Member{{ID: 2, Name: "node-b"}}}
		var closed bool
		discovery := testEtcdConnection(discoveryClient, "https://discovery", "etcd-b", "node-b", &closed)
		called := false
		generator := &WorkloadClusterClientGenerator{
			etcdConnectionFactory: func(_ context.Context, _ []byte, _, targetNodeName string) (*etcdConnection, error) {
				Expect(targetNodeName).To(BeEmpty())
				called = true
				return discovery, nil
			},
		}

		Expect(generator.ProtectEtcdLeadership(ctx, nil, "node-a", "node-c")).To(Succeed())
		Expect(called).To(BeTrue())
		Expect(discoveryClient.statusCalls).To(Equal(0))
		Expect(closed).To(BeTrue())
	})

	It("should succeed without opening a source connection when the source is a follower", func() {
		discoveryClient := &mockEtcdClient{
			members:         []*etcdserverpb.Member{{ID: 1, Name: "node-a"}, {ID: 2, Name: "node-b"}},
			statusResponses: []*clientv3.StatusResponse{statusResponse(2, 2)},
		}
		var closed bool
		discovery := testEtcdConnection(discoveryClient, "https://discovery", "etcd-b", "node-b", &closed)
		connections := 0
		generator := &WorkloadClusterClientGenerator{
			etcdConnectionFactory: func(_ context.Context, _ []byte, _, targetNodeName string) (*etcdConnection, error) {
				connections++
				Expect(targetNodeName).To(BeEmpty())
				return discovery, nil
			},
		}

		Expect(generator.ProtectEtcdLeadership(ctx, nil, "node-a", "node-b")).To(Succeed())
		Expect(connections).To(Equal(1))
		Expect(discoveryClient.moveLeaderCalls).To(BeEmpty())
		Expect(closed).To(BeTrue())
	})

	It("should return an error when there is no elected leader", func() {
		discoveryClient := &mockEtcdClient{
			members:         []*etcdserverpb.Member{{ID: 1, Name: "node-a"}},
			statusResponses: []*clientv3.StatusResponse{statusResponse(2, 0)},
		}
		var closed bool
		discovery := testEtcdConnection(discoveryClient, "https://discovery", "etcd-b", "node-b", &closed)
		generator := &WorkloadClusterClientGenerator{
			etcdConnectionFactory: func(_ context.Context, _ []byte, _, targetNodeName string) (*etcdConnection, error) {
				Expect(targetNodeName).To(BeEmpty())
				return discovery, nil
			},
		}

		err := generator.ProtectEtcdLeadership(ctx, nil, "node-a", "node-b")
		Expect(err).To(MatchError(ContainSubstring("no elected leader")))
		Expect(closed).To(BeTrue())
	})

	It("should return success when leadership changes before the source refresh", func() {
		members := []*etcdserverpb.Member{{ID: 1, Name: "node-a"}, {ID: 2, Name: "node-b"}}
		discoveryClient := &mockEtcdClient{
			members:         members,
			statusResponses: []*clientv3.StatusResponse{statusResponse(2, 1)},
		}
		sourceClient := &mockEtcdClient{
			members:         members,
			statusResponses: []*clientv3.StatusResponse{statusResponse(1, 2)},
		}
		var discoveryClosed, sourceClosed bool
		discovery := testEtcdConnection(discoveryClient, "https://discovery", "etcd-b", "node-b", &discoveryClosed)
		source := testEtcdConnection(sourceClient, "https://source", "etcd-a", "node-a", &sourceClosed)
		generator := &WorkloadClusterClientGenerator{
			etcdConnectionFactory: func(_ context.Context, _ []byte, _, targetNodeName string) (*etcdConnection, error) {
				if targetNodeName == "" {
					return discovery, nil
				}
				return source, nil
			},
		}

		Expect(generator.ProtectEtcdLeadership(ctx, nil, "node-a", "node-b")).To(Succeed())
		Expect(sourceClient.moveLeaderCalls).To(BeEmpty())
		Expect(discoveryClosed).To(BeTrue())
		Expect(sourceClosed).To(BeTrue())
	})

	It("should succeed when the source disappears during the source refresh", func() {
		members := []*etcdserverpb.Member{{ID: 1, Name: "node-a"}, {ID: 2, Name: "node-b"}}
		discoveryClient := &mockEtcdClient{
			members:         members,
			statusResponses: []*clientv3.StatusResponse{statusResponse(2, 1)},
		}
		sourceClient := &mockEtcdClient{
			memberListResponses: []*clientv3.MemberListResponse{{}},
			statusResponses:     []*clientv3.StatusResponse{statusResponse(1, 1)},
		}
		var discoveryClosed, sourceClosed bool
		discovery := testEtcdConnection(discoveryClient, "https://discovery", "etcd-b", "node-b", &discoveryClosed)
		source := testEtcdConnection(sourceClient, "https://source", "etcd-a", "node-a", &sourceClosed)
		generator := &WorkloadClusterClientGenerator{
			etcdConnectionFactory: func(_ context.Context, _ []byte, _, targetNodeName string) (*etcdConnection, error) {
				if targetNodeName == "" {
					return discovery, nil
				}
				return source, nil
			},
		}

		Expect(generator.ProtectEtcdLeadership(ctx, nil, "node-a", "node-b")).To(Succeed())
		Expect(sourceClient.statusCalls).To(Equal(0))
		Expect(sourceClient.moveLeaderCalls).To(BeEmpty())
		Expect(discoveryClosed).To(BeTrue())
		Expect(sourceClosed).To(BeTrue())
	})

	It("should reject a missing target before MoveLeader", func() {
		members := []*etcdserverpb.Member{{ID: 1, Name: "node-a"}}
		discoveryClient := &mockEtcdClient{
			members:         members,
			statusResponses: []*clientv3.StatusResponse{statusResponse(2, 1)},
		}
		sourceClient := &mockEtcdClient{
			members:         members,
			statusResponses: []*clientv3.StatusResponse{statusResponse(1, 1)},
		}
		var discoveryClosed, sourceClosed bool
		discovery := testEtcdConnection(discoveryClient, "https://discovery", "etcd-b", "node-b", &discoveryClosed)
		source := testEtcdConnection(sourceClient, "https://source", "etcd-a", "node-a", &sourceClosed)
		generator := &WorkloadClusterClientGenerator{
			etcdConnectionFactory: func(_ context.Context, _ []byte, _, targetNodeName string) (*etcdConnection, error) {
				if targetNodeName == "" {
					return discovery, nil
				}
				return source, nil
			},
		}

		err := generator.ProtectEtcdLeadership(ctx, nil, "node-a", "node-b")
		Expect(err).To(MatchError(ContainSubstring("target member \"node-b\" not found")))
		Expect(sourceClient.moveLeaderCalls).To(BeEmpty())
		Expect(discoveryClosed).To(BeTrue())
		Expect(sourceClosed).To(BeTrue())
	})

	It("should reject a learner target before MoveLeader", func() {
		members := []*etcdserverpb.Member{{ID: 1, Name: "node-a"}, {ID: 2, Name: "node-b", IsLearner: true}}
		discoveryClient := &mockEtcdClient{
			members:         members,
			statusResponses: []*clientv3.StatusResponse{statusResponse(2, 1)},
		}
		sourceClient := &mockEtcdClient{
			members:         members,
			statusResponses: []*clientv3.StatusResponse{statusResponse(1, 1)},
		}
		var discoveryClosed, sourceClosed bool
		discovery := testEtcdConnection(discoveryClient, "https://discovery", "etcd-b", "node-b", &discoveryClosed)
		source := testEtcdConnection(sourceClient, "https://source", "etcd-a", "node-a", &sourceClosed)
		generator := &WorkloadClusterClientGenerator{
			etcdConnectionFactory: func(_ context.Context, _ []byte, _, targetNodeName string) (*etcdConnection, error) {
				if targetNodeName == "" {
					return discovery, nil
				}
				return source, nil
			},
		}

		err := generator.ProtectEtcdLeadership(ctx, nil, "node-a", "node-b")
		Expect(err).To(MatchError(ContainSubstring("is a learner")))
		Expect(sourceClient.moveLeaderCalls).To(BeEmpty())
	})

	It("should reject a source status identity mismatch", func() {
		members := []*etcdserverpb.Member{{ID: 1, Name: "node-a"}, {ID: 2, Name: "node-b"}}
		discoveryClient := &mockEtcdClient{
			members:         members,
			statusResponses: []*clientv3.StatusResponse{statusResponse(2, 1)},
		}
		sourceClient := &mockEtcdClient{
			members:         members,
			statusResponses: []*clientv3.StatusResponse{statusResponse(2, 1)},
		}
		var discoveryClosed, sourceClosed bool
		discovery := testEtcdConnection(discoveryClient, "https://discovery", "etcd-b", "node-b", &discoveryClosed)
		source := testEtcdConnection(sourceClient, "https://source", "etcd-a", "node-a", &sourceClosed)
		generator := &WorkloadClusterClientGenerator{
			etcdConnectionFactory: func(_ context.Context, _ []byte, _, targetNodeName string) (*etcdConnection, error) {
				if targetNodeName == "" {
					return discovery, nil
				}
				return source, nil
			},
		}

		err := generator.ProtectEtcdLeadership(ctx, nil, "node-a", "node-b")
		Expect(err).To(MatchError(ContainSubstring("does not match source member")))
		Expect(sourceClient.moveLeaderCalls).To(BeEmpty())
	})

	It("should return an error when the source pod cannot be reached", func() {
		discoveryClient := &mockEtcdClient{
			members:         []*etcdserverpb.Member{{ID: 1, Name: "node-a"}, {ID: 2, Name: "node-b"}},
			statusResponses: []*clientv3.StatusResponse{statusResponse(2, 1)},
		}
		var discoveryClosed bool
		discovery := testEtcdConnection(discoveryClient, "https://discovery", "etcd-b", "node-b", &discoveryClosed)
		generator := &WorkloadClusterClientGenerator{
			etcdConnectionFactory: func(_ context.Context, _ []byte, _, targetNodeName string) (*etcdConnection, error) {
				if targetNodeName == "" {
					return discovery, nil
				}
				return nil, fmt.Errorf("no running etcd pod found on node %s", targetNodeName)
			},
		}

		err := generator.ProtectEtcdLeadership(ctx, nil, "node-a", "node-b")
		Expect(err).To(MatchError(ContainSubstring("no running etcd pod found on node node-a")))
		Expect(discoveryClosed).To(BeTrue())
	})

	It("should perform one status refresh after ErrNotLeader and accept a changed leader", func() {
		members := []*etcdserverpb.Member{{ID: 1, Name: "node-a"}, {ID: 2, Name: "node-b"}}
		discoveryClient := &mockEtcdClient{
			members:         members,
			statusResponses: []*clientv3.StatusResponse{statusResponse(2, 1)},
		}
		sourceClient := &mockEtcdClient{
			members: members,
			statusResponses: []*clientv3.StatusResponse{
				statusResponse(1, 1),
				statusResponse(1, 2),
			},
			moveLeaderErr: rpctypes.ErrNotLeader,
		}
		var discoveryClosed, sourceClosed bool
		discovery := testEtcdConnection(discoveryClient, "https://discovery", "etcd-b", "node-b", &discoveryClosed)
		source := testEtcdConnection(sourceClient, "https://source", "etcd-a", "node-a", &sourceClosed)
		generator := &WorkloadClusterClientGenerator{
			etcdConnectionFactory: func(_ context.Context, _ []byte, _, targetNodeName string) (*etcdConnection, error) {
				if targetNodeName == "" {
					return discovery, nil
				}
				return source, nil
			},
		}

		Expect(generator.ProtectEtcdLeadership(ctx, nil, "node-a", "node-b")).To(Succeed())
		Expect(sourceClient.statusCalls).To(Equal(2))
		Expect(sourceClient.moveLeaderCalls).To(Equal([]uint64{2}))
	})

	It("should retain ErrNotLeader when the source remains leader after refresh", func() {
		members := []*etcdserverpb.Member{{ID: 1, Name: "node-a"}, {ID: 2, Name: "node-b"}}
		discoveryClient := &mockEtcdClient{
			members:         members,
			statusResponses: []*clientv3.StatusResponse{statusResponse(2, 1)},
		}
		sourceClient := &mockEtcdClient{
			members: members,
			statusResponses: []*clientv3.StatusResponse{
				statusResponse(1, 1),
				statusResponse(1, 1),
			},
			moveLeaderErr: rpctypes.ErrNotLeader,
		}
		var discoveryClosed, sourceClosed bool
		discovery := testEtcdConnection(discoveryClient, "https://discovery", "etcd-b", "node-b", &discoveryClosed)
		source := testEtcdConnection(sourceClient, "https://source", "etcd-a", "node-a", &sourceClosed)
		generator := &WorkloadClusterClientGenerator{
			etcdConnectionFactory: func(_ context.Context, _ []byte, _, targetNodeName string) (*etcdConnection, error) {
				if targetNodeName == "" {
					return discovery, nil
				}
				return source, nil
			},
		}

		err := generator.ProtectEtcdLeadership(ctx, nil, "node-a", "node-b")
		Expect(err).To(MatchError(ContainSubstring("failed to move etcd leadership")))
		Expect(sourceClient.statusCalls).To(Equal(2))
	})

	It("should propagate non-ErrNotLeader MoveLeader errors", func() {
		members := []*etcdserverpb.Member{{ID: 1, Name: "node-a"}, {ID: 2, Name: "node-b"}}
		discoveryClient := &mockEtcdClient{
			members:         members,
			statusResponses: []*clientv3.StatusResponse{statusResponse(2, 1)},
		}
		sourceClient := &mockEtcdClient{
			members:         members,
			statusResponses: []*clientv3.StatusResponse{statusResponse(1, 1)},
			moveLeaderErr:   fmt.Errorf("connection refused"),
		}
		var discoveryClosed, sourceClosed bool
		discovery := testEtcdConnection(discoveryClient, "https://discovery", "etcd-b", "node-b", &discoveryClosed)
		source := testEtcdConnection(sourceClient, "https://source", "etcd-a", "node-a", &sourceClosed)
		generator := &WorkloadClusterClientGenerator{
			etcdConnectionFactory: func(_ context.Context, _ []byte, _, targetNodeName string) (*etcdConnection, error) {
				if targetNodeName == "" {
					return discovery, nil
				}
				return source, nil
			},
		}

		err := generator.ProtectEtcdLeadership(ctx, nil, "node-a", "node-b")
		Expect(err).To(MatchError(ContainSubstring("connection refused")))
		Expect(sourceClient.statusCalls).To(Equal(1))
	})
})
