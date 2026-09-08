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

package kubevirt

import (
	"fmt"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
)

const (
	CSIManifestsConfigMapName = "kubevirt-csi-manifests"

	csiNamespace       = "openshift-cluster-csi-drivers"
	csiCredSecretName  = "kubevirt-infra-credentials"
	csiControllerSA    = "kubevirt-csi-controller-sa"
	csiNodeSA          = "kubevirt-csi-node-sa"
	csiControllerName  = "kubevirt-csi-controller"
	csiNodeName        = "kubevirt-csi-node"
	csiDriverName      = "csi.kubevirt.io"
	csiConfigMapName   = "driver-config"
	csiStorageClass    = "kubevirt-csi"

	// Release payload component names for image resolution.
	PayloadCSIDriver            = "kubevirt-csi-driver"
	PayloadCSIProvisioner       = "csi-external-provisioner"
	PayloadCSIAttacher          = "csi-external-attacher"
	PayloadCSISnapshotter       = "csi-external-snapshotter"
	PayloadCSIResizer           = "csi-external-resizer"
	PayloadCSILivenessProbe     = "csi-livenessprobe"
	PayloadCSINodeRegistrar     = "csi-node-driver-registrar"
)

// CSIImages holds the resolved images for the CSI driver stack.
// All fields must be resolved from the OCP/OKD release payload via
// ResolveImageFromPayload before generating manifests.
type CSIImages struct {
	Driver        string
	Provisioner   string
	Attacher      string
	Snapshotter   string
	Resizer       string
	LivenessProbe string
	NodeRegistrar string
}

// GenerateCSIManifests produces the manifest entries needed to deploy the
// kubevirt-csi-driver stack on the tenant cluster during installation.
//
// Container images are resolved from the OCP/OKD release payload at
// manifest-generation time. A lightweight bash operator runs on the tenant
// cluster to re-resolve images from ClusterVersion on OCP upgrades.
func GenerateCSIManifests(oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane, infraNamespace string, images CSIImages, oseCliImage string) []ManifestEntry {
	if oacp.Spec.Config.CSIDriver == nil || !oacp.Spec.Config.CSIDriver.Enabled {
		return nil
	}

	infraSC := oacp.Spec.Config.CSIDriver.InfraStorageClass
	if images.Driver == "" || images.Provisioner == "" || images.Attacher == "" ||
		images.Snapshotter == "" || images.Resizer == "" ||
		images.LivenessProbe == "" || images.NodeRegistrar == "" {
		return nil
	}

	var manifests []ManifestEntry

	manifests = append(manifests, ManifestEntry{
		Filename: "01-csi-namespace.yaml",
		Content: fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
  labels:
    openshift.io/cluster-monitoring: "true"
`, csiNamespace),
	})

	manifests = append(manifests, ManifestEntry{
		Filename: "02-csi-driver.yaml",
		Content: fmt.Sprintf(`apiVersion: storage.k8s.io/v1
kind: CSIDriver
metadata:
  name: %s
spec:
  attachRequired: true
  podInfoOnMount: true
  fsGroupPolicy: ReadWriteOnceWithFSType
`, csiDriverName),
	})

	manifests = append(manifests, ManifestEntry{
		Filename: "03-csi-driver-config.yaml",
		Content: fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: %s
  namespace: %s
data:
  infraClusterNamespace: "%s"
  infraClusterLabels: "csi-driver/cluster=tenant"
`, csiConfigMapName, csiNamespace, infraNamespace),
	})

	manifests = append(manifests, ManifestEntry{
		Filename: "04-csi-serviceaccounts.yaml",
		Content: fmt.Sprintf(`apiVersion: v1
kind: ServiceAccount
metadata:
  name: %s
  namespace: %s
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: %s
  namespace: %s
`, csiControllerSA, csiNamespace, csiNodeSA, csiNamespace),
	})

	manifests = append(manifests, ManifestEntry{
		Filename: "05-csi-rbac-controller.yaml",
		Content: fmt.Sprintf(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kubevirt-csi-controller-role
rules:
  - apiGroups: [""]
    resources: ["nodes", "persistentvolumeclaims", "persistentvolumes", "pods", "events"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["storage.k8s.io"]
    resources: ["storageclasses", "volumeattachments", "volumeattachments/status", "csinodes"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["snapshot.storage.k8s.io"]
    resources: ["volumesnapshots", "volumesnapshotcontents", "volumesnapshotclasses"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kubevirt-csi-controller-binding
subjects:
  - kind: ServiceAccount
    name: %s
    namespace: %s
roleRef:
  kind: ClusterRole
  name: kubevirt-csi-controller-role
  apiGroup: rbac.authorization.k8s.io
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: external-snapshotter-runner
rules:
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["list", "watch", "create", "update", "patch"]
  - apiGroups: ["snapshot.storage.k8s.io"]
    resources: ["volumesnapshots"]
    verbs: ["get", "list", "watch", "update", "patch"]
  - apiGroups: ["snapshot.storage.k8s.io"]
    resources: ["volumesnapshots/status"]
    verbs: ["update", "patch"]
  - apiGroups: ["snapshot.storage.k8s.io"]
    resources: ["volumesnapshotcontents"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["snapshot.storage.k8s.io"]
    resources: ["volumesnapshotcontents/status"]
    verbs: ["update", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: csi-snapshotter-role-binding
subjects:
  - kind: ServiceAccount
    name: %s
    namespace: %s
roleRef:
  kind: ClusterRole
  name: external-snapshotter-runner
  apiGroup: rbac.authorization.k8s.io
`, csiControllerSA, csiNamespace, csiControllerSA, csiNamespace),
	})

	manifests = append(manifests, ManifestEntry{
		Filename: "06-csi-rbac-node.yaml",
		Content: fmt.Sprintf(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kubevirt-csi-node-role
rules:
  - apiGroups: [""]
    resources: ["nodes", "events"]
    verbs: ["get", "list", "watch", "update", "patch", "create"]
  - apiGroups: ["storage.k8s.io"]
    resources: ["csinodes"]
    verbs: ["get", "list", "watch", "update", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kubevirt-csi-node-binding
subjects:
  - kind: ServiceAccount
    name: %s
    namespace: %s
roleRef:
  kind: ClusterRole
  name: kubevirt-csi-node-role
  apiGroup: rbac.authorization.k8s.io
`, csiNodeSA, csiNamespace),
	})

	manifests = append(manifests, ManifestEntry{
		Filename: "07-csi-scc-rolebindings.yaml",
		Content: fmt.Sprintf(`apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: kubevirt-csi-controller-privileged
  namespace: %s
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: system:openshift:scc:privileged
subjects:
  - kind: ServiceAccount
    name: %s
    namespace: %s
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: kubevirt-csi-node-privileged
  namespace: %s
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: system:openshift:scc:privileged
subjects:
  - kind: ServiceAccount
    name: %s
    namespace: %s
`, csiNamespace, csiControllerSA, csiNamespace, csiNamespace, csiNodeSA, csiNamespace),
	})

	manifests = append(manifests, ManifestEntry{
		Filename: "08-csi-controller-deployment.yaml",
		Content: fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: %s
spec:
  replicas: 1
  selector:
    matchLabels:
      app: %s
  template:
    metadata:
      labels:
        app: %s
    spec:
      serviceAccountName: %s
      priorityClassName: system-cluster-critical
      nodeSelector:
        node-role.kubernetes.io/control-plane: ""
      tolerations:
        - key: CriticalAddonsOnly
          operator: Exists
        - key: node-role.kubernetes.io/master
          operator: Exists
          effect: "NoSchedule"
        - key: node-role.kubernetes.io/control-plane
          operator: Exists
          effect: "NoSchedule"
      containers:
        - name: kubevirt-csi-driver
          image: %s
          args:
            - "--endpoint=$(CSI_ENDPOINT)"
            - "--infra-cluster-namespace=$(INFRACLUSTER_NAMESPACE)"
            - "--infra-cluster-kubeconfig=/var/run/secrets/infracluster/kubeconfig"
            - "--infra-cluster-labels=$(INFRACLUSTER_LABELS)"
            - "--run-node-service=false"
            - "--run-controller-service=true"
            - "--v=5"
          ports:
            - name: healthz
              containerPort: 10301
              protocol: TCP
          env:
            - name: CSI_ENDPOINT
              value: unix:///var/lib/csi/sockets/pluginproxy/csi.sock
            - name: KUBE_NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
            - name: INFRACLUSTER_NAMESPACE
              valueFrom:
                configMapKeyRef:
                  name: %s
                  key: infraClusterNamespace
            - name: INFRACLUSTER_LABELS
              valueFrom:
                configMapKeyRef:
                  name: %s
                  key: infraClusterLabels
            - name: INFRA_STORAGE_CLASS_ENFORCEMENT
              valueFrom:
                configMapKeyRef:
                  name: %s
                  key: infraStorageClassEnforcement
                  optional: true
          volumeMounts:
            - name: socket-dir
              mountPath: /var/lib/csi/sockets/pluginproxy/
            - name: infracluster
              mountPath: "/var/run/secrets/infracluster"
          resources:
            requests:
              memory: 50Mi
              cpu: 10m
        - name: csi-provisioner
          image: %s
          args:
            - "--csi-address=$(ADDRESS)"
            - "--default-fstype=ext4"
            - "--v=5"
            - "--timeout=3m"
            - "--retry-interval-max=1m"
          env:
            - name: ADDRESS
              value: /var/lib/csi/sockets/pluginproxy/csi.sock
          volumeMounts:
            - name: socket-dir
              mountPath: /var/lib/csi/sockets/pluginproxy/
          resources:
            requests:
              memory: 50Mi
              cpu: 10m
        - name: csi-attacher
          image: %s
          args:
            - "--csi-address=$(ADDRESS)"
            - "--v=5"
            - "--timeout=3m"
            - "--retry-interval-max=1m"
          env:
            - name: ADDRESS
              value: /var/lib/csi/sockets/pluginproxy/csi.sock
          volumeMounts:
            - name: socket-dir
              mountPath: /var/lib/csi/sockets/pluginproxy/
          resources:
            requests:
              memory: 50Mi
              cpu: 10m
        - name: csi-liveness-probe
          image: %s
          args:
            - "--csi-address=/csi/csi.sock"
            - "--probe-timeout=3s"
            - "--health-port=10301"
          volumeMounts:
            - name: socket-dir
              mountPath: /csi
          resources:
            requests:
              memory: 50Mi
              cpu: 10m
        - name: csi-snapshotter
          image: %s
          args:
            - "--v=3"
            - "--csi-address=/csi/csi.sock"
            - "--timeout=3m"
          volumeMounts:
            - mountPath: /csi
              name: socket-dir
          resources:
            requests:
              memory: 20Mi
              cpu: 10m
        - name: csi-resizer
          image: %s
          args:
            - "-csi-address=/csi/csi.sock"
            - "-v=5"
            - "-timeout=3m"
            - "-handle-volume-inuse-error=false"
          volumeMounts:
            - name: socket-dir
              mountPath: /csi
          resources:
            requests:
              cpu: 10m
              memory: 20Mi
          securityContext:
            capabilities:
              drop:
                - ALL
      volumes:
        - name: socket-dir
          emptyDir: {}
        - name: infracluster
          secret:
            secretName: %s
`, csiControllerName, csiNamespace, csiControllerName, csiControllerName, csiControllerSA,
			images.Driver, csiConfigMapName, csiConfigMapName, csiConfigMapName,
			images.Provisioner, images.Attacher, images.LivenessProbe,
			images.Snapshotter, images.Resizer, csiCredSecretName),
	})

	manifests = append(manifests, ManifestEntry{
		Filename: "09-csi-node-daemonset.yaml",
		Content: fmt.Sprintf(`apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: %s
  namespace: %s
spec:
  selector:
    matchLabels:
      app: kubevirt-csi-driver
  updateStrategy:
    type: RollingUpdate
  template:
    metadata:
      labels:
        app: kubevirt-csi-driver
    spec:
      serviceAccountName: %s
      priorityClassName: system-node-critical
      tolerations:
        - operator: Exists
      containers:
        - name: csi-driver
          securityContext:
            privileged: true
            allowPrivilegeEscalation: true
          image: %s
          args:
            - "--endpoint=unix:/csi/csi.sock"
            - "--node-name=$(KUBE_NODE_NAME)"
            - "--run-node-service=true"
            - "--run-controller-service=false"
            - "--v=5"
          env:
            - name: KUBE_NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
          volumeMounts:
            - name: kubelet-dir
              mountPath: /var/lib/kubelet
              mountPropagation: "Bidirectional"
            - name: plugin-dir
              mountPath: /csi
            - name: device-dir
              mountPath: /dev
            - name: udev
              mountPath: /run/udev
          ports:
            - name: healthz
              containerPort: 10300
              protocol: TCP
          livenessProbe:
            httpGet:
              path: /healthz
              port: healthz
            initialDelaySeconds: 10
            timeoutSeconds: 3
            periodSeconds: 10
            failureThreshold: 5
          resources:
            requests:
              memory: 50Mi
              cpu: 10m
        - name: csi-node-driver-registrar
          image: %s
          args:
            - "--csi-address=$(ADDRESS)"
            - "--kubelet-registration-path=$(DRIVER_REG_SOCK_PATH)"
            - "--v=5"
          lifecycle:
            preStop:
              exec:
                command: ["/bin/sh", "-c", "rm -rf /registration/csi.kubevirt.io-reg.sock /csi/csi.sock"]
          env:
            - name: ADDRESS
              value: /csi/csi.sock
            - name: DRIVER_REG_SOCK_PATH
              value: /var/lib/kubelet/plugins/csi.kubevirt.io/csi.sock
          volumeMounts:
            - name: plugin-dir
              mountPath: /csi
            - name: registration-dir
              mountPath: /registration
          resources:
            requests:
              memory: 20Mi
              cpu: 5m
        - name: csi-liveness-probe
          image: %s
          args:
            - "--csi-address=/csi/csi.sock"
            - "--probe-timeout=3s"
            - "--health-port=10300"
          volumeMounts:
            - name: plugin-dir
              mountPath: /csi
          resources:
            requests:
              memory: 20Mi
              cpu: 5m
      volumes:
        - name: kubelet-dir
          hostPath:
            path: /var/lib/kubelet
            type: Directory
        - name: plugin-dir
          hostPath:
            path: /var/lib/kubelet/plugins/csi.kubevirt.io/
            type: DirectoryOrCreate
        - name: registration-dir
          hostPath:
            path: /var/lib/kubelet/plugins_registry/
            type: Directory
        - name: device-dir
          hostPath:
            path: /dev
            type: Directory
        - name: udev
          hostPath:
            path: /run/udev
`, csiNodeName, csiNamespace, csiNodeSA, images.Driver,
			images.NodeRegistrar, images.LivenessProbe),
	})

	if infraSC != "" {
		manifests = append(manifests, ManifestEntry{
			Filename: "10-csi-storageclass.yaml",
			Content: fmt.Sprintf(`apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: %s
  annotations:
    storageclass.kubernetes.io/is-default-class: "true"
provisioner: %s
parameters:
  infraStorageClassName: %s
  bus: scsi
reclaimPolicy: Delete
allowVolumeExpansion: true
volumeBindingMode: Immediate
`, csiStorageClass, csiDriverName, infraSC),
		})
	}

	if oseCliImage != "" {
		manifests = append(manifests, generateCSIOperatorManifests(oseCliImage)...)
	}

	return manifests
}

// csiOperatorScript watches ClusterVersion on the tenant cluster and patches
// CSI controller Deployment and node DaemonSet with digest-pinned images from
// the current release payload. Also annotates nodes with the infra cluster
// namespace for the CSI driver to discover its backing VMs.
const csiOperatorScript = `#!/bin/bash
set -euo pipefail

NAMESPACE="openshift-cluster-csi-drivers"
PULL_SECRET_DIR="/tmp/csi-pull-secret"
PULL_SECRET_PATH="$PULL_SECRET_DIR/.dockerconfigjson"
RECONCILE_INTERVAL="${RECONCILE_INTERVAL:-60}"
INFRA_NAMESPACE=$(cat /config/infraClusterNamespace 2>/dev/null || echo "")

MAPPINGS=(
  "kubevirt-csi-driver:kubevirt-csi-driver:deployment:kubevirt-csi-controller"
  "csi-driver:kubevirt-csi-driver:daemonset:kubevirt-csi-node"
  "csi-provisioner:csi-external-provisioner:deployment:kubevirt-csi-controller"
  "csi-attacher:csi-external-attacher:deployment:kubevirt-csi-controller"
  "csi-snapshotter:csi-external-snapshotter:deployment:kubevirt-csi-controller"
  "csi-resizer:csi-external-resizer:deployment:kubevirt-csi-controller"
  "csi-liveness-probe:csi-livenessprobe:deployment:kubevirt-csi-controller"
  "csi-liveness-probe:csi-livenessprobe:daemonset:kubevirt-csi-node"
  "csi-node-driver-registrar:csi-node-driver-registrar:daemonset:kubevirt-csi-node"
)

log() { echo "[$(date -u '+%Y-%m-%d %H:%M:%S UTC')] $*"; }

wait_for_clusterversion() {
  log "Waiting for ClusterVersion..."
  until oc get clusterversion version -o jsonpath='{.status.desired.image}' 2>/dev/null | grep -q .; do sleep 10; done
  log "ClusterVersion available."
}

annotate_nodes() {
  [ -z "$INFRA_NAMESPACE" ] && return
  for node in $(oc get nodes -o jsonpath='{.items[*].metadata.name}' 2>/dev/null); do
    current=$(oc get node "$node" -o jsonpath='{.metadata.annotations.cluster\.x-k8s\.io/cluster-namespace}' 2>/dev/null || true)
    if [ "$current" != "$INFRA_NAMESPACE" ]; then
      log "Annotating node $node with cluster.x-k8s.io/cluster-namespace=$INFRA_NAMESPACE"
      oc annotate node "$node" "cluster.x-k8s.io/cluster-namespace=$INFRA_NAMESPACE" --overwrite 2>/dev/null || true
    fi
  done
}

wait_for_workloads() {
  log "Waiting for CSI workloads..."
  until oc get deployment/kubevirt-csi-controller -n "$NAMESPACE" &>/dev/null && \
        oc get daemonset/kubevirt-csi-node -n "$NAMESPACE" &>/dev/null; do sleep 5; done
  log "CSI workloads found."
}

extract_pull_secret() {
  mkdir -p "$PULL_SECRET_DIR"
  oc extract secret/pull-secret -n openshift-config --to="$PULL_SECRET_DIR" --confirm >/dev/null 2>&1 && [ -s "$PULL_SECRET_PATH" ]
}

reconcile_images() {
  local payload="$1"
  local all_ok=true
  for mapping in "${MAPPINGS[@]}"; do
    IFS=':' read -r container payload_name res_type res_name <<< "$mapping"
    desired=$(oc adm release info "$payload" --image-for="$payload_name" --registry-config="$PULL_SECRET_PATH" 2>/dev/null || true)
    if [ -z "$desired" ]; then
      log "WARN: could not resolve $payload_name from payload"
      all_ok=false
      continue
    fi
    current=$(oc get "$res_type/$res_name" -n "$NAMESPACE" -o jsonpath="{.spec.template.spec.containers[?(@.name=='$container')].image}" 2>/dev/null || true)
    [ "$current" = "$desired" ] && continue
    log "UPDATE: $res_type/$res_name container=$container -> $desired"
    if ! oc set image "$res_type/$res_name" "$container=$desired" -n "$NAMESPACE" 2>/dev/null; then
      log "WARN: failed to update $res_type/$res_name container=$container"
      all_ok=false
    fi
  done
  $all_ok
}

log "=== KubeVirt CSI Operator starting ==="
wait_for_clusterversion
annotate_nodes
wait_for_workloads

LAST_PAYLOAD=""
while true; do
  CURRENT_PAYLOAD=$(oc get clusterversion version -o jsonpath='{.status.desired.image}' 2>/dev/null || true)
  if [ -n "$CURRENT_PAYLOAD" ] && [ "$CURRENT_PAYLOAD" != "$LAST_PAYLOAD" ]; then
    log "Release payload: $CURRENT_PAYLOAD"
    if extract_pull_secret; then
      if reconcile_images "$CURRENT_PAYLOAD"; then
        LAST_PAYLOAD="$CURRENT_PAYLOAD"
      else
        log "WARN: partial reconciliation failure, will retry next cycle"
      fi
    fi
  fi
  annotate_nodes
  sleep "$RECONCILE_INTERVAL"
done
`

func generateCSIOperatorManifests(oseCliImage string) []ManifestEntry {
	return []ManifestEntry{
		{
			Filename: "11-csi-operator-rbac.yaml",
			Content: fmt.Sprintf(`apiVersion: v1
kind: ServiceAccount
metadata:
  name: kubevirt-csi-operator-sa
  namespace: %s
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kubevirt-csi-operator-role
rules:
  - apiGroups: ["config.openshift.io"]
    resources: ["clusterversions"]
    verbs: ["get", "list"]
  - apiGroups: [""]
    resources: ["nodes"]
    verbs: ["get", "list", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kubevirt-csi-operator-binding
subjects:
  - kind: ServiceAccount
    name: kubevirt-csi-operator-sa
    namespace: %s
roleRef:
  kind: ClusterRole
  name: kubevirt-csi-operator-role
  apiGroup: rbac.authorization.k8s.io
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: kubevirt-csi-operator-workloads
  namespace: %s
rules:
  - apiGroups: ["apps"]
    resources: ["deployments"]
    resourceNames: ["%s"]
    verbs: ["get", "patch"]
  - apiGroups: ["apps"]
    resources: ["daemonsets"]
    resourceNames: ["%s"]
    verbs: ["get", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: kubevirt-csi-operator-workloads
  namespace: %s
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: kubevirt-csi-operator-workloads
subjects:
  - kind: ServiceAccount
    name: kubevirt-csi-operator-sa
    namespace: %s
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: kubevirt-csi-operator-pull-secret
  namespace: openshift-config
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    resourceNames: ["pull-secret"]
    verbs: ["get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: kubevirt-csi-operator-pull-secret
  namespace: openshift-config
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: kubevirt-csi-operator-pull-secret
subjects:
  - kind: ServiceAccount
    name: kubevirt-csi-operator-sa
    namespace: %s
`, csiNamespace, csiNamespace, csiNamespace, csiControllerName, csiNodeName, csiNamespace, csiNamespace, csiNamespace),
		},
		{
			Filename: "12-csi-operator-script.yaml",
			Content: fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: kubevirt-csi-operator-script
  namespace: %s
data:
  operator.sh: |
%s`, csiNamespace, indentMultiline(csiOperatorScript)),
		},
		{
			Filename: "13-csi-operator-deployment.yaml",
			Content: fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: kubevirt-csi-operator
  namespace: %s
  labels:
    app: kubevirt-csi-operator
spec:
  replicas: 1
  selector:
    matchLabels:
      app: kubevirt-csi-operator
  template:
    metadata:
      labels:
        app: kubevirt-csi-operator
    spec:
      serviceAccountName: kubevirt-csi-operator-sa
      containers:
        - name: operator
          image: %s
          command: ["/bin/bash", "/scripts/operator.sh"]
          env:
            - name: RECONCILE_INTERVAL
              value: "60"
          volumeMounts:
            - name: script
              mountPath: /scripts
              readOnly: true
            - name: config
              mountPath: /config
              readOnly: true
          resources:
            requests:
              cpu: 10m
              memory: 50Mi
      tolerations:
        - key: node-role.kubernetes.io/master
          operator: Exists
          effect: NoSchedule
        - key: node-role.kubernetes.io/control-plane
          operator: Exists
          effect: NoSchedule
      nodeSelector:
        node-role.kubernetes.io/control-plane: ""
      volumes:
        - name: script
          configMap:
            name: kubevirt-csi-operator-script
            defaultMode: 0755
        - name: config
          configMap:
            name: %s
`, csiNamespace, oseCliImage, csiConfigMapName),
		},
	}
}
