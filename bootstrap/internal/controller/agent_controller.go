package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	v1beta12 "github.com/metal3-io/cluster-api-provider-metal3/api/v1beta1"
	"github.com/metal3-io/cluster-api-provider-metal3/baremetal"
	"github.com/openshift-assisted/cluster-api-agent/bootstrap/api/v1beta1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/models"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
	"time"
)

// AgentReconciler reconciles an Agent object
type AgentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *AgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aiv1beta1.Agent{}).
		Complete(r)
}

func (r *AgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	agent := &aiv1beta1.Agent{}
	err := r.Get(ctx, req.NamespacedName, agent)
	if err != nil {
		log.Error(err, "unable to fetch Agent")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// if we find an agent, we must ensure it is controlled by our provider
	clusterDeploymentKey := client.ObjectKey{
		Namespace: agent.Spec.ClusterDeploymentName.Namespace,
		Name:      agent.Spec.ClusterDeploymentName.Name,
	}
	clusterDeployment := &hivev1.ClusterDeployment{}
	if err := r.Client.Get(ctx, clusterDeploymentKey, clusterDeployment); err != nil {
		log.Error(err, "unable to fetch Agent")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	clusterName, ok := clusterDeployment.Labels[clusterv1.ClusterNameLabel]
	if !ok {
		log.Error(err, "clusterdeployment does not belong to a CAPI cluster")
		return ctrl.Result{}, nil
	}
	agentBootstrapConfigList := v1beta1.AgentBootstrapConfigList{}
	if err := r.Client.List(ctx, &agentBootstrapConfigList, client.MatchingLabels{clusterv1.ClusterNameLabel: clusterName}); err != nil {
		log.Error(err, "agentboostrapconfig not found for cluster", "cluster", clusterName)
		return ctrl.Result{}, err
	}
	if agent.Status.Inventory.Interfaces == nil {
		log.Info("agent doesn't have interfaces yet", "agent name", agent.Name)
		return ctrl.Result{RequeueAfter: 20 * time.Second}, nil
	}

	bmh, err := r.getBMHFromAgent(ctx, agent)
	if err != nil {
		log.Error(err, "can't get bmhs for agent", "cluster", clusterName)
		return ctrl.Result{}, err
	}
	if bmh == nil {
		return ctrl.Result{RequeueAfter: 20 * time.Second}, nil
	}
	bmhUID := string(bmh.GetUID())
	agent.Spec.NodeLabels = map[string]string{"metal3.io/uuid": bmhUID}
	machine, err := r.getMachineFromBMH(ctx, bmh)
	if err != nil {
		log.Error(err, "can't get bmhs for agent", "cluster", bmh)
		return ctrl.Result{}, err
	}
	role := models.HostRoleWorker
	if _, ok := machine.Labels[clusterv1.MachineControlPlaneLabel]; ok {
		role = models.HostRoleMaster
	}
	log.Info("setting role to agent", "role", role)
	// TODO: skip if installing

	//	kubeletConfig := getKubeletConfig()
	/*{
		"path": "/etc/systemd/system/kubelet.service",
		"mode": 420,
		"overwrite": true,
		"contents": {
		"source": "data:text/plain;charset=utf-8;base64,%s"
	}
	},*/
	agent.Spec.Role = role
	content := fmt.Sprintf(`[Service]
Environment="KUBELET_PROVIDERID=metal3.io/uuid=%s"
Environment="CUSTOM_KUBELET_LABELS=metal3.io/uuid=%s"
`,
		string(bmh.UID),
		string(bmh.UID),
	)

	encodedContent := base64.StdEncoding.EncodeToString([]byte(content))
	//encodedKubeletConfig := base64.StdEncoding.EncodeToString([]byte(kubeletConfig))
	ignition := fmt.Sprintf(`{
		"ignition": { "version": "3.1.0" },
		"storage": {
			"files": [
              {
                "path": "/etc/systemd/system/kubelet.service.d/30-capi-provider-env.conf",
		        "mode": 420,
		        "contents": {
					"source": "data:text/plain;charset=utf-8;base64,%s"
				}
		      },
			  {
                "path": "/run/cluster-api/bootstrap-success.complete",
		        "mode": 420,
		        "contents": {
					"source": "data:text/plain;charset=utf-8;base64,c3VjY2Vzcw=="
				}
		      }
	        ]
		}
	}
`,
		//encodedKubeletConfig,
		encodedContent,
	)
	log.Info("setting ignition override to agent", "ignition", ignition)

	agent.Spec.IgnitionConfigOverrides = ignition
	agent.Spec.Approved = true
	if err := r.Client.Update(ctx, agent); err != nil {
		log.Error(err, "couldn't update agent", "name", agent.Name, "namespace", agent.Namespace)
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func getKubeletConfig() string {
	return `[Unit]
Description=Kubernetes Kubelet
Requires=crio.service kubelet-dependencies.target
After=kubelet-dependencies.target
After=ostree-finalize-staged.service

[Service]
Type=notify
ExecStartPre=/bin/mkdir --parents /etc/kubernetes/manifests
ExecStartPre=/bin/rm -f /var/lib/kubelet/cpu_manager_state
ExecStartPre=/bin/rm -f /var/lib/kubelet/memory_manager_state
EnvironmentFile=/etc/os-release
EnvironmentFile=-/etc/kubernetes/kubelet-workaround
EnvironmentFile=-/etc/kubernetes/kubelet-env
EnvironmentFile=/etc/node-sizing.env

ExecStart=/usr/local/bin/kubenswrapper \
    /usr/bin/kubelet \
      --config=/etc/kubernetes/kubelet.conf \
      --bootstrap-kubeconfig=/etc/kubernetes/kubeconfig \
      --kubeconfig=/var/lib/kubelet/kubeconfig \
      --container-runtime-endpoint=/var/run/crio/crio.sock \
      --runtime-cgroups=/system.slice/crio.service \
      --node-labels=node-role.kubernetes.io/control-plane,node-role.kubernetes.io/master,node.openshift.io/os_id=${ID},${CUSTOM_KUBELET_LABELS} \
      --node-ip=${KUBELET_NODE_IP} \
      --address=${KUBELET_NODE_IP} \
      --minimum-container-ttl-duration=6m0s \
      --cloud-provider= \
      --provider-id=${KUBELET_PROVIDER_ID}\
      --volume-plugin-dir=/etc/kubernetes/kubelet-plugins/volume/exec \
       \
      --hostname-override=${KUBELET_NODE_NAME} \
      --register-with-taints=node-role.kubernetes.io/master=:NoSchedule \
      --pod-infra-container-image=quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:b47df6baa7da64933f574bbfb110ed81efb926e6a730dfd846efcf2001093741 \
      --system-reserved=cpu=${SYSTEM_RESERVED_CPU},memory=${SYSTEM_RESERVED_MEMORY},ephemeral-storage=${SYSTEM_RESERVED_ES} \
      --v=${KUBELET_LOG_LEVEL}

Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target`
}

func (r *AgentReconciler) getMachineFromBMH(ctx context.Context, bmh *bmh_v1alpha1.BareMetalHost) (*clusterv1.Machine, error) {
	m3machine, err := r.getMetal3MachineFromBMH(ctx, bmh)
	if err != nil {
		return nil, err
	}
	return r.getMachineFromMetal3Machine(ctx, m3machine)
}

func (r *AgentReconciler) getMachineFromMetal3Machine(ctx context.Context, m3machine *v1beta12.Metal3Machine) (*clusterv1.Machine, error) {
	log := ctrl.LoggerFrom(ctx)

	machine := clusterv1.Machine{}
	for _, ref := range m3machine.OwnerReferences {
		log.Info("comparing owner to machine", "refKind", ref.Kind, "refAPIVersion", ref.APIVersion, "machineKind", machine.Kind, "machineAPIversion", machine.APIVersion)
		// TODO: set it as constant
		if ref.Kind == "Machine" && ref.APIVersion == "cluster.x-k8s.io/v1beta1" {
			if err := r.Client.Get(ctx, types.NamespacedName{
				Namespace: m3machine.Namespace,
				Name:      ref.Name,
			},
				&machine); err != nil {
				return nil, err
			}
			return &machine, nil
		}
	}
	return nil, fmt.Errorf("no machine found for metal3machine %s/%s", m3machine.Namespace, m3machine.Name)
}

func (r *AgentReconciler) getMetal3MachineFromBMH(ctx context.Context, bmh *bmh_v1alpha1.BareMetalHost) (*v1beta12.Metal3Machine, error) {
	ml := v1beta12.Metal3MachineList{}
	if err := r.Client.List(ctx, &ml); err != nil {
		return nil, err
	}
	for _, m := range ml.Items {
		annotation, ok := m.Annotations[baremetal.HostAnnotation]
		if !ok {
			continue
		}
		parts := strings.Split(annotation, "/")
		if len(parts) < 2 {
			continue
		}
		if bmh.Namespace == parts[0] && bmh.Name == parts[1] {
			return &m, nil
		}
	}

	return nil, fmt.Errorf("found %d metal3machines, none matching BMH %s/%s", len(ml.Items), bmh.Namespace, bmh.Name)
}

func (r *AgentReconciler) getBMHFromAgent(ctx context.Context, agent *aiv1beta1.Agent) (*bmh_v1alpha1.BareMetalHost, error) {
	bmhs := &bmh_v1alpha1.BareMetalHostList{}
	if err := r.Client.List(ctx, bmhs); err != nil {
		return nil, err
	}
	for _, bmh := range bmhs.Items {
		for _, agentInterface := range agent.Status.Inventory.Interfaces {
			if agentInterface.MacAddress != "" && strings.EqualFold(bmh.Spec.BootMACAddress, agentInterface.MacAddress) {
				return &bmh, nil
			}
		}
	}

	return nil, fmt.Errorf("found %d BMHs, and none matched any MacAddress from the agent's %d interfaces", len(bmhs.Items), len(agent.Status.Inventory.Interfaces))
}
