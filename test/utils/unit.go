package utils

import (
	"strings"
	"time"

	"github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	metal3 "github.com/metal3-io/cluster-api-provider-metal3/api/v1beta1"
	bootstrapv1alpha2 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/bootstrap/api/v1alpha2"
	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	"github.com/openshift/assisted-service/api/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ExtractAPIGroupFromVersion extracts the API group from an apiVersion string.
// For example, "infrastructure.cluster.x-k8s.io/v1beta1" returns "infrastructure.cluster.x-k8s.io".
// Returns an empty string if the apiVersion doesn't contain a group (e.g., "v1" for core API).
func ExtractAPIGroupFromVersion(apiVersion string) string {
	if apiVersion == "" {
		return ""
	}
	parts := strings.Split(apiVersion, "/")
	if len(parts) == 2 {
		return parts[0]
	}
	return ""
}

func NewAgentClusterInstall(name string, namespace string, ownerCluster string) *hiveext.AgentClusterInstall {
	aci := &hiveext.AgentClusterInstall{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: ownerCluster,
			},
		},
		Spec: hiveext.AgentClusterInstallSpec{},
	}
	return aci
}

func NewClusterDeployment(namespace, name string, oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane) *hivev1.ClusterDeployment {
	cd := &hivev1.ClusterDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{},
		},
		Spec: hivev1.ClusterDeploymentSpec{
			ClusterName: name,
			BaseDomain:  "example.com",
		},
	}

	cd.Kind = "ClusterDeployment"
	cd.APIVersion = hivev1.SchemeGroupVersion.String()
	return cd
}

func NewClusterDeploymentWithOwnerCluster(namespace, name, ownerCluster string, oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane) *hivev1.ClusterDeployment {
	cd := NewClusterDeployment(namespace, name, oacp)
	cd.Labels[clusterv1.ClusterNameLabel] = ownerCluster
	return cd
}

func NewCluster(clusterName, namespace string) *clusterv1.Cluster {
	// Create cluster and have it own this agent control plane
	notPaused := false
	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: clusterv1.ClusterSpec{
			Paused: &notPaused,
			ControlPlaneEndpoint: clusterv1.APIEndpoint{
				Host: "example.com",
				Port: 8080,
			},
		},
		Status: clusterv1.ClusterStatus{
			Initialization: clusterv1.ClusterInitializationStatus{
				InfrastructureProvisioned: ptr.To(true),
			},
		},
	}
	return cluster
}

func NewMachineWithInfraRef(
	machineName, namespace, clusterName string,
	acp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	infraRef client.Object,
) *clusterv1.Machine {
	infraRefGVK := infraRef.GetObjectKind().GroupVersionKind()
	machine := NewMachineWithOwner(namespace, machineName, clusterName, acp)
	machine.Spec.InfrastructureRef = clusterv1.ContractVersionedObjectReference{
		APIGroup: ExtractAPIGroupFromVersion(infraRefGVK.GroupVersion().String()),
		Kind:     infraRefGVK.Kind,
		Name:     infraRef.GetName(),
	}
	return machine
}

func NewMachine(namespace, name, clusterName string) *clusterv1.Machine {
	machine := &clusterv1.Machine{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Machine",
			APIVersion: clusterv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: clusterName,
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: clusterName,
		},
	}
	return machine
}
func NewMachineWithOwner(namespace, name, clusterName string, obj client.Object) *clusterv1.Machine {
	gvk := obj.GetObjectKind().GroupVersionKind()
	machine := NewMachine(namespace, name, clusterName)
	machine.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: gvk.GroupVersion().String(),
			Kind:       gvk.Kind,
			Name:       obj.GetName(),
			UID:        obj.GetUID(),
		},
	}
	return machine
}

func NewM3MachineTemplate(namespace, name string) *metal3.Metal3MachineTemplate {
	return &metal3.Metal3MachineTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}
}

func NewOpenshiftAssistedControlPlane(namespace, name string) *controlplanev1alpha3.OpenshiftAssistedControlPlane {
	return &controlplanev1alpha3.OpenshiftAssistedControlPlane{
		TypeMeta: metav1.TypeMeta{
			Kind:       "OpenshiftAssistedControlPlane",
			APIVersion: controlplanev1alpha3.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       uuid.NewUUID(),
		},
		Spec: controlplanev1alpha3.OpenshiftAssistedControlPlaneSpec{
			DistributionVersion: "4.18.0",
		},
	}
}

func NewOpenshiftAssistedControlPlaneWithCapabilities(
	namespace, name string,
	replicas int32,
	baselineCapability string,
	additionalCapabilities []string,
) *controlplanev1alpha3.OpenshiftAssistedControlPlane {
	oacp := NewOpenshiftAssistedControlPlane(namespace, name)
	oacp.Spec.Config.Capabilities.BaselineCapability = baselineCapability
	oacp.Spec.Config.Capabilities.AdditionalEnabledCapabilities = additionalCapabilities
	oacp.Spec.Replicas = replicas
	return oacp
}

func NewOpenshiftAssistedControlPlaneWithMachineTemplate(
	namespace, name string,
	m3Template *metal3.Metal3MachineTemplate,
) *controlplanev1alpha3.OpenshiftAssistedControlPlane {
	acp := NewOpenshiftAssistedControlPlane(namespace, name)
	acp.Spec.MachineTemplate.InfrastructureRef = clusterv1.ContractVersionedObjectReference{
		Kind:     m3Template.Kind,
		Name:     m3Template.Name,
		APIGroup: ExtractAPIGroupFromVersion(m3Template.APIVersion),
	}
	return acp
}

func NewMetal3Machine(namespace, name string) *metal3.Metal3Machine {
	return &metal3.Metal3Machine{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Metal3Machine",
			APIVersion: metal3.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}
}

func NewInfraEnv(namespace, name string) *v1beta1.InfraEnv {
	return &v1beta1.InfraEnv{
		TypeMeta: metav1.TypeMeta{
			Kind:       "InfraEnv",
			APIVersion: v1beta1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Status: v1beta1.InfraEnvStatus{
			CreatedTime: &metav1.Time{Time: time.Now().Add(-2 * time.Minute)},
		},
	}
}

func NewAgent(namespace, name string) *v1beta1.Agent {
	return &v1beta1.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func NewAgentWithInfraEnvLabel(namespace, name, infraEnvName string) *v1beta1.Agent {
	agent := NewAgent(namespace, name)
	agent.Labels = map[string]string{"infraenvs.agent-install.openshift.io": infraEnvName}
	return agent
}

func NewAgentWithClusterDeploymentReference(namespace, name string, cd hivev1.ClusterDeployment) *v1beta1.Agent {
	agent := NewAgent(namespace, name)
	agent.Spec.ClusterDeploymentName = &v1beta1.ClusterReference{
		Name:      cd.Name,
		Namespace: cd.Namespace,
	}
	return agent
}

func NewBareMetalHost(namespace, name string) *v1alpha1.BareMetalHost {
	return &v1alpha1.BareMetalHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func NewOpenshiftAssistedConfigWithInfraEnv(
	namespace, name, clusterName string,
	infraEnv *v1beta1.InfraEnv,
) *bootstrapv1alpha2.OpenshiftAssistedConfig {
	// This should really set the ref to infraenv
	return &bootstrapv1alpha2.OpenshiftAssistedConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "OpenshiftAssistedConfig",
			APIVersion: bootstrapv1alpha2.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			UID: uuid.NewUUID(),
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:         clusterName,
				clusterv1.MachineControlPlaneLabel: "control-plane",
			},
			Name:      name,
			Namespace: namespace,
		},
	}
}

func NewOpenshiftAssistedConfig(namespace, name, clusterName string) *bootstrapv1alpha2.OpenshiftAssistedConfig {
	return NewOpenshiftAssistedConfigWithInfraEnv(namespace, name, clusterName, nil)
}
