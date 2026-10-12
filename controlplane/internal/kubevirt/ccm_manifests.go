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
	CCMManifestsConfigMapName = "kubevirt-ccm-manifests"

	ccmNamespace        = "openshift-cloud-controller-manager"
	ccmCredSecretName   = "kubevirt-infra-credentials"
	ccmDeploymentName   = "kubevirt-cloud-controller-manager"
	ccmServiceAccount   = "kubevirt-cloud-controller-manager"
	ccmClusterRole      = "system:cloud-controller-manager"
)

// GenerateCCMManifests produces the manifest entries needed to deploy the KubeVirt
// Cloud Controller Manager on the tenant cluster during installation.
//
// The ccmImage parameter is resolved from the OCP/OKD release payload at
// manifest-generation time (via ResolveImageFromPayload). The oseCliImage is
// used by a lightweight bash operator that runs on the tenant cluster and
// re-resolves CCM images from ClusterVersion on OCP upgrades, keeping the
// CCM deployment current without requiring management cluster connectivity.
func GenerateCCMManifests(oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane, infraNamespace, ccmImage, oseCliImage string) []ManifestEntry {
	if oacp.Spec.Config.CloudControllerManager == nil || !oacp.Spec.Config.CloudControllerManager.Enabled {
		return nil
	}

	if ccmImage == "" {
		return nil
	}

	var manifests []ManifestEntry

	manifests = append(manifests, ManifestEntry{
		Filename: "01-ccm-namespace.yaml",
		Content: fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
  labels:
    openshift.io/cluster-monitoring: "true"
`, ccmNamespace),
	})

	manifests = append(manifests, ManifestEntry{
		Filename: "02-ccm-cloud-config.yaml",
		Content: fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: cloud-config
  namespace: %s
data:
  cloud-config: |
    kubeconfig: /etc/kubernetes/infra-kubeconfig/kubeconfig
    namespace: %s
    loadBalancer:
      enabled: true
      creationPollInterval: 5
      creationPollTimeout: 60
      selectorless: true
    instancesV2:
      enabled: true
      zoneAndRegionEnabled: false
`, ccmNamespace, infraNamespace),
	})

	manifests = append(manifests, ManifestEntry{
		Filename: "03-ccm-rbac.yaml",
		Content: fmt.Sprintf(`apiVersion: v1
kind: ServiceAccount
metadata:
  name: %s
  namespace: %s
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: %s
rules:
  - apiGroups: [""]
    resources: ["nodes"]
    verbs: ["get", "list", "watch", "patch", "update", "delete"]
  - apiGroups: [""]
    resources: ["nodes/status"]
    verbs: ["patch"]
  - apiGroups: [""]
    resources: ["services"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["services/status"]
    verbs: ["patch", "update"]
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["create", "patch", "update"]
  - apiGroups: ["coordination.k8s.io"]
    resources: ["leases"]
    verbs: ["get", "list", "watch", "create", "update", "patch"]
  - apiGroups: [""]
    resources: ["serviceaccounts"]
    verbs: ["create", "get"]
  - apiGroups: [""]
    resources: ["serviceaccounts/token"]
    verbs: ["create"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: %s
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: %s
subjects:
  - kind: ServiceAccount
    name: %s
    namespace: %s
`, ccmServiceAccount, ccmNamespace, ccmClusterRole, ccmClusterRole, ccmClusterRole, ccmServiceAccount, ccmNamespace),
	})

	manifests = append(manifests, ManifestEntry{
		Filename: "04-ccm-deployment.yaml",
		Content: fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: %s
  labels:
    app: %s
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
      containers:
        - name: cloud-controller-manager
          image: %s
          command:
            - /bin/kubevirt-cloud-controller-manager
          args:
            - --cloud-provider=kubevirt
            - --cloud-config=/etc/cloud/cloud-config
            - --use-service-account-credentials=true
            - --leader-elect=true
            - --leader-elect-resource-namespace=%s
            - --controllers=cloud-node,cloud-node-lifecycle,service-lb-controller
            - --authentication-skip-lookup
          resources:
            requests:
              cpu: 75m
              memory: 60Mi
          volumeMounts:
            - name: cloud-config
              mountPath: /etc/cloud
              readOnly: true
            - name: infra-kubeconfig
              mountPath: /etc/kubernetes/infra-kubeconfig
              readOnly: true
      volumes:
        - name: cloud-config
          configMap:
            name: cloud-config
        - name: infra-kubeconfig
          secret:
            secretName: %s
      tolerations:
        - key: node-role.kubernetes.io/master
          operator: Exists
          effect: NoSchedule
        - key: node.cloudprovider.kubernetes.io/uninitialized
          operator: Exists
          effect: NoSchedule
      nodeSelector:
        node-role.kubernetes.io/master: ""
`, ccmDeploymentName, ccmNamespace, ccmDeploymentName, ccmDeploymentName, ccmDeploymentName,
			ccmServiceAccount, ccmImage, ccmNamespace, ccmCredSecretName),
	})

	if oseCliImage != "" {
		manifests = append(manifests, generateCCMOperatorManifests(oseCliImage)...)
	}

	return manifests
}

// ccmOperatorScript watches ClusterVersion on the tenant cluster and patches the
// CCM Deployment with the correct digest-pinned image from the current release
// payload. This keeps the CCM current across OCP upgrades without requiring
// connectivity to the management cluster.
const ccmOperatorScript = `#!/bin/bash
set -euo pipefail

NAMESPACE="openshift-cloud-controller-manager"
PULL_SECRET_DIR="/tmp/ccm-pull-secret"
PULL_SECRET_PATH="$PULL_SECRET_DIR/.dockerconfigjson"
RECONCILE_INTERVAL="${RECONCILE_INTERVAL:-60}"

MAPPINGS=(
  "cloud-controller-manager:kubevirt-cloud-controller-manager:deployment:kubevirt-cloud-controller-manager"
)

log() { echo "[$(date -u '+%Y-%m-%d %H:%M:%S UTC')] $*"; }

wait_for_clusterversion() {
  log "Waiting for ClusterVersion..."
  until oc get clusterversion version -o jsonpath='{.status.desired.image}' 2>/dev/null | grep -q .; do sleep 10; done
  log "ClusterVersion available."
}

wait_for_workloads() {
  log "Waiting for CCM deployment..."
  until oc get deployment/kubevirt-cloud-controller-manager -n "$NAMESPACE" &>/dev/null; do sleep 5; done
  log "CCM deployment found."
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

log "=== KubeVirt CCM Operator starting ==="
wait_for_clusterversion
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
  sleep "$RECONCILE_INTERVAL"
done
`

func generateCCMOperatorManifests(oseCliImage string) []ManifestEntry {
	return []ManifestEntry{
		{
			Filename: "05-ccm-operator-script.yaml",
			Content: fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: kubevirt-ccm-operator-script
  namespace: %s
data:
  operator.sh: |
%s`, ccmNamespace, indentMultiline(ccmOperatorScript)),
		},
		{
			Filename: "06-ccm-operator-rbac.yaml",
			Content: fmt.Sprintf(`apiVersion: v1
kind: ServiceAccount
metadata:
  name: kubevirt-ccm-operator
  namespace: %s
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kubevirt-ccm-operator
rules:
  - apiGroups: ["config.openshift.io"]
    resources: ["clusterversions"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kubevirt-ccm-operator
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kubevirt-ccm-operator
subjects:
  - kind: ServiceAccount
    name: kubevirt-ccm-operator
    namespace: %s
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: kubevirt-ccm-operator-workloads
  namespace: %s
rules:
  - apiGroups: ["apps"]
    resources: ["deployments"]
    resourceNames: ["%s"]
    verbs: ["get", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: kubevirt-ccm-operator-workloads
  namespace: %s
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: kubevirt-ccm-operator-workloads
subjects:
  - kind: ServiceAccount
    name: kubevirt-ccm-operator
    namespace: %s
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: kubevirt-ccm-operator-pull-secret
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
  name: kubevirt-ccm-operator-pull-secret
  namespace: openshift-config
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: kubevirt-ccm-operator-pull-secret
subjects:
  - kind: ServiceAccount
    name: kubevirt-ccm-operator
    namespace: %s
`, ccmNamespace, ccmNamespace, ccmNamespace, ccmDeploymentName, ccmNamespace, ccmNamespace, ccmNamespace),
		},
		{
			Filename: "07-ccm-operator-deployment.yaml",
			Content: fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: kubevirt-ccm-operator
  namespace: %s
  labels:
    app: kubevirt-ccm-operator
spec:
  replicas: 1
  selector:
    matchLabels:
      app: kubevirt-ccm-operator
  template:
    metadata:
      labels:
        app: kubevirt-ccm-operator
    spec:
      serviceAccountName: kubevirt-ccm-operator
      containers:
        - name: operator
          image: %s
          command: ["/bin/bash", "/scripts/operator.sh"]
          volumeMounts:
            - name: scripts
              mountPath: /scripts
              readOnly: true
          resources:
            requests:
              cpu: 10m
              memory: 50Mi
      volumes:
        - name: scripts
          configMap:
            name: kubevirt-ccm-operator-script
      tolerations:
        - key: node-role.kubernetes.io/master
          operator: Exists
          effect: NoSchedule
      nodeSelector:
        node-role.kubernetes.io/master: ""
`, ccmNamespace, oseCliImage),
		},
	}
}
