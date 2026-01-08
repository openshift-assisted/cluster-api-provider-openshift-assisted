package failuredomains

import (
	"context"
	"fmt"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/conditions"
)

var _ = Describe("Failure Domains", func() {
	var (
		ctx context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("NextFailureDomainForScaleUp", func() {
		Context("with no failure domains", func() {
			It("returns nil", func() {
				cluster := &clusterv1.Cluster{
					Status: clusterv1.ClusterStatus{
						FailureDomains: []clusterv1.FailureDomain{},
					},
				}
				machines := collections.Machines{}
				domain, err := NextFailureDomainForScaleUp(ctx, cluster, machines)
				Expect(err).ToNot(HaveOccurred())
				Expect(domain).To(Equal(""))
			})
		})

		Context("with single failure domain", func() {
			It("returns the single control plane domain", func() {
				cluster := &clusterv1.Cluster{
					Status: clusterv1.ClusterStatus{
						FailureDomains: []clusterv1.FailureDomain{
							{
								Name:         "zone-1",
								ControlPlane: ptr.To(true),
							},
						},
					},
				}
				machines := collections.Machines{}

				domain, err := NextFailureDomainForScaleUp(ctx, cluster, machines)
				Expect(err).ToNot(HaveOccurred())
				Expect(domain).To(Equal("zone-1"))
			})
		})

		Context("with multiple failure domains", func() {
			It("returns the domain with fewest machines", func() {
				cluster := &clusterv1.Cluster{
					Status: clusterv1.ClusterStatus{
						FailureDomains: []clusterv1.FailureDomain{
							{
								Name:         "zone-1",
								ControlPlane: ptr.To(true),
							},
							{
								Name:         "zone-2",
								ControlPlane: ptr.To(true),
							},
						},
					},
				}
				machines := createTestMachines(
					machinesParams{"zone-1", 3},
					machinesParams{"zone-2", 1},
				)
				for _, machine := range machines {
					conditions.Set(machine, metav1.Condition{
						Type:   clusterv1.MachineUpToDateCondition,
						Status: metav1.ConditionTrue,
						Reason: clusterv1.MachineUpToDateReason,
					})
				}
				domain, err := NextFailureDomainForScaleUp(ctx, cluster, machines)
				Expect(err).ToNot(HaveOccurred())
				Expect(domain).To(Equal("zone-2"))
			})
		})
	})

	Describe("NextFailureDomainForScaleDown", func() {
		Context("with no eligible machines", func() {
			It("returns nil when no machines exist", func() {
				cluster := &clusterv1.Cluster{
					Status: clusterv1.ClusterStatus{
						FailureDomains: []clusterv1.FailureDomain{},
					},
				}
				machines := collections.Machines{}

				domain, err := NextFailureDomainForScaleDown(ctx, cluster, machines)
				Expect(err).ToNot(HaveOccurred())
				Expect(domain).To(Equal(""))
			})
		})

		Context("with machines in multiple domains", func() {
			It("returns the domain with most machines", func() {
				cluster := &clusterv1.Cluster{
					Status: clusterv1.ClusterStatus{
						FailureDomains: []clusterv1.FailureDomain{
							{
								Name:         "zone-1",
								ControlPlane: ptr.To(true),
							},
							{
								Name:         "zone-2",
								ControlPlane: ptr.To(true),
							},
						},
					},
				}
				machines := createTestMachines(
					machinesParams{"zone-1", 3},
					machinesParams{"zone-2", 1},
				)

				domain, err := NextFailureDomainForScaleDown(ctx, cluster, machines)
				Expect(err).ToNot(HaveOccurred())
				Expect(domain).To(Equal("zone-1"))
			})

			It("considers only machines with delete annotation when present", func() {
				cluster := &clusterv1.Cluster{
					Status: clusterv1.ClusterStatus{
						FailureDomains: []clusterv1.FailureDomain{
							{
								Name:         "zone-1",
								ControlPlane: ptr.To(true),
							},
							{
								Name:         "zone-2",
								ControlPlane: ptr.To(true),
							},
						},
					},
				}
				machines := createTestMachinesWithDeleteAnnotation()

				domain, err := NextFailureDomainForScaleDown(ctx, cluster, machines)
				Expect(err).ToNot(HaveOccurred())
				Expect(domain).To(Equal("zone-2"))
			})
		})
	})

	Describe("FailureDomains", func() {
		Context("with nil inputs", func() {
			It("returns empty failure domains for nil cluster", func() {
				domains := FailureDomains(nil)
				Expect(domains).To(Equal([]clusterv1.FailureDomain{}))
			})

			It("returns empty failure domains for cluster with nil failure domains", func() {
				cluster := &clusterv1.Cluster{
					Status: clusterv1.ClusterStatus{
						FailureDomains: nil,
					},
				}
				domains := FailureDomains(cluster)
				Expect(domains).To(Equal([]clusterv1.FailureDomain{}))
			})
		})

		Context("with valid cluster", func() {
			It("returns the cluster's failure domains", func() {
				expectedDomains := []clusterv1.FailureDomain{
					{
						Name:         "zone-1",
						ControlPlane: ptr.To(true),
					},
				}
				cluster := &clusterv1.Cluster{
					Status: clusterv1.ClusterStatus{
						FailureDomains: expectedDomains,
					},
				}

				domains := FailureDomains(cluster)
				Expect(domains).To(Equal(expectedDomains))
			})
		})
	})
})

// Helper functions
type machinesParams struct {
	failureDomain string
	machines      int
}

func createTestMachines(params ...machinesParams) collections.Machines {
	var rawMachines []*clusterv1.Machine
	for _, testCase := range params {
		fd := testCase.failureDomain
		for i := 0; i < testCase.machines; i++ {
			machine := clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					// this must be unique,
					Name: "machine-" + fd + "-" + strconv.Itoa(i),
					Labels: map[string]string{
						clusterv1.MachineControlPlaneLabel: "",
					},
				},
				Spec: clusterv1.MachineSpec{
					FailureDomain: fd,
				},
			}
			fmt.Printf("adding machine %v\n", machine)
			rawMachines = append(rawMachines, &machine)
		}
	}
	Expect(len(rawMachines)).To(Equal(4))
	machines := collections.FromMachines(rawMachines...)
	return machines
}

func createTestMachinesWithDeleteAnnotation() collections.Machines {
	machines := collections.Machines{}

	// Create 2 machines in zone-1, one with delete annotation
	zone1 := "zone-1"
	machine1 := &clusterv1.Machine{
		Spec: clusterv1.MachineSpec{
			FailureDomain: zone1,
		},
	}
	machine2 := &clusterv1.Machine{
		Spec: clusterv1.MachineSpec{
			FailureDomain: zone1,
		},
	}

	// Create 1 machine in zone-2 with delete annotation
	zone2 := "zone-2"
	machine3 := &clusterv1.Machine{
		Spec: clusterv1.MachineSpec{
			FailureDomain: zone2,
		},
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				clusterv1.DeleteMachineAnnotation: "true",
			},
		},
	}

	machines.Insert(machine1, machine2, machine3)
	return machines
}
