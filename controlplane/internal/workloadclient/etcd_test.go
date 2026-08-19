package workloadclient

import (
	"context"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.etcd.io/etcd/api/v3/etcdserverpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEtcd(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Etcd Suite")
}

type mockEtcdClient struct {
	members         []*etcdserverpb.Member
	alarms          []*etcdserverpb.AlarmMember
	memberListErr   error
	alarmListErr    error
	memberRemoveErr error
	removedID       uint64
	leaderID        uint64
	moveLeaderErr   error
	movedToID       uint64
	statusErr       error
}

func (m *mockEtcdClient) MemberList(_ context.Context, _ ...clientv3.OpOption) (*clientv3.MemberListResponse, error) {
	if m.memberListErr != nil {
		return nil, m.memberListErr
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
	if m.moveLeaderErr != nil {
		return nil, m.moveLeaderErr
	}
	return &clientv3.MoveLeaderResponse{}, nil
}

func (m *mockEtcdClient) Status(_ context.Context, _ string) (*clientv3.StatusResponse, error) {
	if m.statusErr != nil {
		return nil, m.statusErr
	}
	resp := &clientv3.StatusResponse{
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
