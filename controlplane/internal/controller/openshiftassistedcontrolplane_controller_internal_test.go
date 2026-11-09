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

package controller

import (
	"context"
	"testing"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	testutils "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/test/utils"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReconcileReplicasUsesAPIReaderForKubeVirtMachineCount(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(controlplanev1alpha3.AddToScheme(scheme))

	oacp := testutils.NewOpenshiftAssistedControlPlane("test-namespace", "test-control-plane")
	oacp.Spec.Replicas = 1
	cluster := testutils.NewCluster("test-cluster", "test-namespace")
	cluster.Spec.InfrastructureRef.Kind = "KubevirtCluster"
	machine := testutils.NewMachineWithOwner("test-namespace", "existing-machine", "test-cluster", oacp)

	cachedClient := fakeclient.NewClientBuilder().WithScheme(scheme).Build()
	apiReader := fakeclient.NewClientBuilder().WithScheme(scheme).WithObjects(machine).Build()
	reconciler := &OpenshiftAssistedControlPlaneReconciler{
		Client:    cachedClient,
		APIReader: apiReader,
		Scheme:    scheme,
	}

	if err := reconciler.reconcileReplicas(ctx, oacp, cluster); err != nil {
		t.Fatalf("reconcileReplicas() error = %v", err)
	}

	machineList := &clusterv1.MachineList{}
	if err := cachedClient.List(ctx, machineList, client.InNamespace("test-namespace")); err != nil {
		t.Fatalf("failed to list cached machines: %v", err)
	}
	if len(machineList.Items) != 0 {
		t.Fatalf("expected no duplicate machine creation, got %d machines", len(machineList.Items))
	}

	if err := apiReader.List(ctx, machineList, client.InNamespace("test-namespace")); err != nil {
		t.Fatalf("failed to list API-reader machines: %v", err)
	}
	if len(machineList.Items) != 1 {
		t.Fatalf("expected one machine, got %d machines", len(machineList.Items))
	}
}

func TestReconcileReplicasUsesCachedReaderForMetal3MachineCount(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(controlplanev1alpha3.AddToScheme(scheme))

	oacp := testutils.NewOpenshiftAssistedControlPlane("test-namespace", "test-control-plane")
	oacp.Spec.Replicas = 1
	cluster := testutils.NewCluster("test-cluster", "test-namespace")
	cluster.Spec.InfrastructureRef.Kind = "Metal3Cluster"
	machine := testutils.NewMachineWithOwner("test-namespace", "existing-machine", "test-cluster", oacp)

	cachedClient := fakeclient.NewClientBuilder().WithScheme(scheme).WithObjects(machine).Build()
	apiReader := fakeclient.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &OpenshiftAssistedControlPlaneReconciler{
		Client:    cachedClient,
		APIReader: apiReader,
		Scheme:    scheme,
	}

	if err := reconciler.reconcileReplicas(ctx, oacp, cluster); err != nil {
		t.Fatalf("reconcileReplicas() error = %v", err)
	}

	machineList := &clusterv1.MachineList{}
	if err := cachedClient.List(ctx, machineList, client.InNamespace("test-namespace")); err != nil {
		t.Fatalf("failed to list cached machines: %v", err)
	}
	if len(machineList.Items) != 1 {
		t.Fatalf("expected the cached metal3 machine to be used, got %d machines", len(machineList.Items))
	}
}
