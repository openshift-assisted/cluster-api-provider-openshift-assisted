package utils

import (
	"github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	metal3 "github.com/metal3-io/cluster-api-provider-metal3/api/v1beta1"
	controlplanev1alpha1 "github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1alpha1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	"github.com/openshift/assisted-service/api/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewAgentClusterInstall(name string, namespace string, ownerCluster string) *hiveext.AgentClusterInstall {
	cd := &hiveext.AgentClusterInstall{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: ownerCluster,
			},
		},
		Spec: hiveext.AgentClusterInstallSpec{},
	}
	return cd
}
func NewClusterDeployment(namespace, name string) *hivev1.ClusterDeployment {
	cd := &hivev1.ClusterDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
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

func NewClusterDeploymentWithOwnerCluster(namespace, name, ownerCluster string) *hivev1.ClusterDeployment {
	cd := NewClusterDeployment(namespace, name)
	cd.Labels = map[string]string{
		clusterv1.ClusterNameLabel: ownerCluster,
	}
	return cd
}

func NewCluster(clusterName, namespace string) *clusterv1.Cluster {
	// Create cluster and have it own this agent control plane
	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: clusterv1.ClusterSpec{
			Paused: false,
			ControlPlaneEndpoint: clusterv1.APIEndpoint{
				Host: "example.com",
				Port: 8080,
			},
		},
		Status: clusterv1.ClusterStatus{
			InfrastructureReady: true,
		},
	}
	return cluster
}

func NewMachineWithInfraRef(machineName, namespace, clusterName string, acp *controlplanev1alpha1.AgentControlPlane, infraRef client.Object) *clusterv1.Machine {
	infraRefGVK := infraRef.GetObjectKind().GroupVersionKind()
	machine := NewMachineWithOwner(namespace, machineName, clusterName, acp)
	machine.Spec.InfrastructureRef = corev1.ObjectReference{
		APIVersion: infraRefGVK.GroupVersion().String(),
		Kind:       infraRefGVK.Kind,
		Namespace:  infraRef.GetNamespace(),
		Name:       infraRef.GetName(),
		UID:        infraRef.GetUID(),
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
func NewM3MachineTemplateWithImage(namespace, name, url, diskFormat string) *metal3.Metal3MachineTemplate {
	m3Template := NewM3MachineTemplate(namespace, name)
	m3Template.Spec.Template.Spec.Image.URL = url
	m3Template.Spec.Template.Spec.Image.DiskFormat = &diskFormat
	return m3Template
}

func NewAgentControlPlane(namespace, name string) *controlplanev1alpha1.AgentControlPlane {
	return &controlplanev1alpha1.AgentControlPlane{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AgentControlPlane",
			APIVersion: controlplanev1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func NewAgentControlPlaneWithMachineTemplate(namespace, name string, m3Template *metal3.Metal3MachineTemplate) *controlplanev1alpha1.AgentControlPlane {
	acp := NewAgentControlPlane(namespace, name)
	acp.Spec.MachineTemplate.InfrastructureRef = corev1.ObjectReference{
		Kind:       m3Template.Kind,
		Namespace:  m3Template.Namespace,
		Name:       m3Template.Name,
		UID:        m3Template.UID,
		APIVersion: m3Template.APIVersion,
	}
	return acp
}

func NewMetal3Machine(namespace, name string) *metal3.Metal3Machine {
	return &metal3.Metal3Machine{
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
