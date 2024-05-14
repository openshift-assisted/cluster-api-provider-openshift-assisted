/*
Copyright 2024.

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
	"fmt"

	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	metal3 "github.com/metal3-io/cluster-api-provider-metal3/api/v1beta1"
	"github.com/pkg/errors"

	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aimodels "github.com/openshift/assisted-service/models"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const ProviderIDLabelKey = "metal3.io/uuid"

// AgentClusterInstallReconciler reconciles a AgentClusterInstall object
type AgentClusterInstallReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *AgentClusterInstallReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hiveext.AgentClusterInstall{}).
		Complete(r)
}

func (r *AgentClusterInstallReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// handle deletion: do nothing
	defer func() {
		log.Info("agentclusterinstall reconcile ended")
	}()

	log.Info("agentclusterinstall reconcile started")
	aci := &hiveext.AgentClusterInstall{}
	if err := r.Client.Get(ctx, req.NamespacedName, aci); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("no ACI found", "name", req.NamespacedName)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if aci == nil {
		log.Info("agentclusterinstall is nil", "name", req.Name, "namespace", req.Namespace)
		return ctrl.Result{}, nil
	}
	log.Info("reconciling AgentClusterInstall", "name", aci.Name, "namespace", aci.Namespace)

	clusterName, ok := aci.GetLabels()[clusterv1.ClusterNameLabel]
	if !ok {
		log.Info("agentclusterinstall, does not belong to any cluster", "name", aci.Name, "namespace", aci.Namespace)
		return ctrl.Result{}, nil
	}
	log.Info("found cluster linked to agentclusterinstall", "name", aci.Name, "clustername", clusterName)

	if !isInstallCompleted(aci) {
		log.Info("install has not completed yet", "name", aci.Name, "clustername", clusterName)
		return ctrl.Result{}, nil
	}
	if !hasKubeconfigSecretRef(aci) {
		log.Info("kubeconfig secret reference not found", "name", aci.Name, "clustername", clusterName)
		return ctrl.Result{}, nil
	}

	// Set metal3 node label on spoke node so that capm3 can set the provider ID
	log.Info("agentcluster install finished installing, setting bmh ID label for spoke nodes")
	if err := r.labelWorkloadClusterNodes(ctx, aci); err != nil {
		log.Error(err, "failed setting spokes node label", "agentclusterinstall name", aci.Name)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func hasKubeconfigSecretRef(aci *hiveext.AgentClusterInstall) bool {
	return aci != nil && aci.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name != ""
}

func isInstallCompleted(aci *hiveext.AgentClusterInstall) bool {
	return aci.Status.DebugInfo.State == aimodels.ClusterStatusAddingHosts
}

func getSpokeClient(kubeconfig []byte) (client.Client, error) {
	clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfig)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get clientconfig from kubeconfig data")
	}

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get restconfig for kube client")
	}

	schemes := getKubeClientSchemes()
	targetClient, err := client.New(restConfig, client.Options{Scheme: schemes})
	if err != nil {
		return nil, err
	}
	return targetClient, nil
}

func getKubeClientSchemes() *runtime.Scheme {
	var schemes = runtime.NewScheme()
	utilruntime.Must(scheme.AddToScheme(schemes))
	utilruntime.Must(corev1.AddToScheme(schemes))
	return schemes
}

func (r *AgentClusterInstallReconciler) labelWorkloadClusterNodes(ctx context.Context, aci *hiveext.AgentClusterInstall) error {
	log := ctrl.LoggerFrom(ctx)
	// Get all Metal3 Machines associated with this cluster
	metal3Machines := &metal3.Metal3MachineList{}
	if err := r.Client.List(ctx, metal3Machines); err != nil {
		log.Error(err, "couldn't get metal3machines associated with agentclusterinstall", "aci name", aci.Name)
		return err
	}
	secretName := aci.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name
	kubeconfigSecret := &corev1.Secret{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: secretName, Namespace: aci.Namespace}, kubeconfigSecret); err != nil {
		log.Error(err, "failed getting kubeconfig secret for agentclusterinstall", "agent cluster install name", aci.Name, "agent cluster install namespace", aci.Namespace, "secret name", secretName)
		return err
	}
	kubeconfig, ok := kubeconfigSecret.Data["kubeconfig"]
	if !ok {
		return errors.New("kubeconfig not found in secret")
	}
	// Create spoke client from kubeconfig
	targetClient, err := getSpokeClient(kubeconfig)
	if err != nil {
		log.Error(err, "couldn't create spoke cluster client")
		return err
	}
	nodes := corev1.NodeList{}
	err = targetClient.List(ctx, &nodes)
	if err != nil {
		log.Error(err, "couldn't list nodes")
		return err
	}
	log.Info("found nodes in workload cluster", "number", len(nodes.Items))
	for _, node := range nodes.Items {
		machine, err := getMachineByMatchingIP(*metal3Machines, node)
		if err != nil {
			log.Info("could not match any machine to node", "name", node.Name)
			continue
		}
		bmh, err := r.getBMH(ctx, *machine)
		if err != nil {
			log.Error(err, "couldn't get bmh", "machine name", machine.Name)
			return err
		}
		bmhUID := string(bmh.ObjectMeta.GetUID())
		providerId := fmt.Sprintf("metal3.io://%s", bmhUID)
		op, err := ctrl.CreateOrUpdate(ctx, r.Client, machine, func() error {
			machine.Spec.ProviderID = &providerId
			return nil
		})
		if err != nil {
			log.Error(err, "error trying to update machine", "op", op, "machine", machine.Name)
			return err
		}
		log.Info("set providerID successfully", "op", op, "machine", machine.Name)
		if err := r.setNodeMetadata(ctx, targetClient, node, machine, bmhUID); err != nil {
			log.Info("could not set node metadata", "name", node.Name)
		} else {
			log.Info("set node metadata", "name", node.Name)
		}
	}
	return nil
}

// return metal3 machine from list when matching node's internal IP address
func getMachineByMatchingIP(machines metal3.Metal3MachineList, node corev1.Node) (*metal3.Metal3Machine, error) {
	for _, machine := range machines.Items {
		for _, machineAddr := range machine.Status.Addresses {
			if corev1.NodeAddressType(machineAddr.Type) == corev1.NodeInternalIP {
				for _, nodeAddr := range node.Status.Addresses {
					if nodeAddr.Type == corev1.NodeInternalIP {
						if nodeAddr.Address == machineAddr.Address {
							return &machine, nil
						}
					}
				}
			}
		}
	}
	return nil, errors.New("could not match any machine to node")
}

func (r *AgentClusterInstallReconciler) setNodeMetadata(ctx context.Context, targetClient client.Client, node corev1.Node, machine *metal3.Metal3Machine, bmhUid string) error {
	log := ctrl.LoggerFrom(ctx)
	providerID, ok := node.Labels[ProviderIDLabelKey]
	if ok {
		log.Info("providerID already set", "providerID", providerID)
		return nil
	}
	node.Labels[ProviderIDLabelKey] = bmhUid
	return targetClient.Update(ctx, &node)
}

func (r *AgentClusterInstallReconciler) getBMH(ctx context.Context, metal3Machine metal3.Metal3Machine) (*bmh_v1alpha1.BareMetalHost, error) {
	log := ctrl.LoggerFrom(ctx)

	annotations := metal3Machine.ObjectMeta.GetAnnotations()
	if annotations == nil {
		return nil, errors.New("metal3machine has no annotations")
	}
	hostKey, ok := annotations["metal3.io/BareMetalHost"]
	if !ok {
		return nil, errors.New("metal3machine has no BMH annotation")
	}
	hostNamespace, hostName, err := cache.SplitMetaNamespaceKey(hostKey)
	if err != nil {
		log.Error(err, "error parsing annotation value", "annotation key", hostKey)
		return nil, err
	}

	bmh := bmh_v1alpha1.BareMetalHost{}
	key := client.ObjectKey{
		Name:      hostName,
		Namespace: hostNamespace,
	}
	if err := r.Client.Get(ctx, key, &bmh); err != nil {
		log.Error(err, "couldn't get associated bmh", "annotation key", hostKey)
		return nil, err
	}

	return &bmh, nil
}
