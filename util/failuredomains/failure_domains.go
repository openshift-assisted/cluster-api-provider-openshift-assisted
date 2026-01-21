package failuredomains

import (
	"context"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/failuredomains"
)

// NextFailureDomainForScaleUp returns the failure domain with the fewest number of machines
func NextFailureDomainForScaleUp(
	ctx context.Context,
	cluster *clusterv1.Cluster,
	machines collections.Machines,
) (string, error) {
	allFailureDomains := FailureDomains(cluster)
	ctlplnFailureDomains := filterControlPlaneFailureDomains(allFailureDomains)
	if len(ctlplnFailureDomains) == 0 {
		return "", nil
	}
	upToDateMachines := collections.Machines{}
	for _, machine := range machines {
		if conditions.IsTrue(machine, clusterv1.MachineUpToDateCondition) {
			upToDateMachines.Insert(machine)
		}
	}
	return failuredomains.PickFewest(ctx, ctlplnFailureDomains, machines, upToDateMachines), nil
}

// NextFailureDomainForScaleDown returns the failure domain with the most number of machines
func NextFailureDomainForScaleDown(
	ctx context.Context,
	cluster *clusterv1.Cluster,
	machines collections.Machines,
) (string, error) {
	allFailureDomains := FailureDomains(cluster)
	ctlplnFailureDomains := filterControlPlaneFailureDomains(allFailureDomains)
	if len(ctlplnFailureDomains) == 0 {
		return "", nil
	}
	eligibleMachines := machines.Filter(collections.HasAnnotationKey(clusterv1.DeleteMachineAnnotation))
	if eligibleMachines.Len() == 0 {
		eligibleMachines = machines
	}
	return failuredomains.PickMost(ctx, ctlplnFailureDomains, machines, eligibleMachines), nil
}

// FailureDomains returns a slice of failure domain objects synced from the infrastructure provider into Cluster.Status.
func FailureDomains(cluster *clusterv1.Cluster) []clusterv1.FailureDomain {
	if cluster == nil || cluster.Status.FailureDomains == nil {
		return []clusterv1.FailureDomain{}
	}
	return cluster.Status.FailureDomains
}

// filter failure domains that are control plane eligible
func filterControlPlaneFailureDomains(failureDomains []clusterv1.FailureDomain) []clusterv1.FailureDomain {
	filteredFailureDomains := []clusterv1.FailureDomain{}
	for _, fd := range failureDomains {
		if fd.ControlPlane != nil && *(fd.ControlPlane) {
			filteredFailureDomains = append(filteredFailureDomains, fd)
		}
	}
	return filteredFailureDomains
}
