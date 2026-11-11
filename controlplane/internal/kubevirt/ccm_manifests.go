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
	"net"
	"strings"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
)

func infraAPIHostCIDR(infraAPIIP string) string {
	if ip := net.ParseIP(infraAPIIP); ip != nil && ip.To4() == nil {
		return infraAPIIP + "/128"
	}
	return infraAPIIP + "/32"
}

func networkPolicyIPBlockDestinations(cidrs []string) string {
	destinations := make([]string, 0, len(cidrs))
	for _, cidr := range cidrs {
		destinations = append(destinations, fmt.Sprintf("        - ipBlock:\n            cidr: %s", cidr))
	}
	return strings.Join(destinations, "\n")
}

func networkPolicyTCPPorts(ports []int32) string {
	lines := make([]string, 0, len(ports))
	for _, port := range ports {
		lines = append(lines, fmt.Sprintf("        - protocol: TCP\n          port: %d", port))
	}
	return strings.Join(lines, "\n")
}

const (
	CCMManifestsConfigMapName = "kubevirt-ccm-manifests"

	ccmNamespace      = "openshift-cloud-controller-manager"
	ccmCredSecretName = "kubevirt-infra-credentials"
	ccmDeploymentName = "kubevirt-cloud-controller-manager"
	ccmServiceAccount = "kubevirt-cloud-controller-manager"
	ccmClusterRole    = "system:cloud-controller-manager"
	ccmInfraRoleName  = "kubevirt-cloud-controller-manager"
)

// GenerateCCMManifests produces the manifest entries needed to deploy the KubeVirt
// Cloud Controller Manager on the tenant cluster during installation.
//
// The ccmImage parameter is resolved from the OCP/OKD release payload at
// manifest-generation time (via ResolveImageFromPayload). The oseCliImage is
// used by a lightweight bash operator that runs on the tenant cluster and
// re-resolves CCM images from ClusterVersion on OCP upgrades, keeping the
// CCM deployment current without requiring management cluster connectivity.
// The generated Deployment also includes a small sidecar that defaults
// classless KubeVirt LoadBalancer Services to Local traffic policy so OVN
// programs their public VIPs on the node ingress path. Users can preserve
// Cluster traffic policy by setting the
// capoa.openshift.io/lb-external-traffic-policy annotation to Cluster.
func GenerateCCMManifests(
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	infraNamespace string,
	clusterName string,
	ccmImage string,
	oseCliImage string,
	egressDestinations CCMEgressDestinations,
) []ManifestEntry {
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
      enableEPSController: true
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
  - apiGroups: ["discovery.k8s.io"]
    resources: ["endpointslices"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["networking.k8s.io"]
    resources: ["networkpolicies"]
    resourceNames: ["allow-apiserver-and-dns-egress"]
    verbs: ["get", "patch"]
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
		Filename: "04-ccm-egress-network-policy.yaml",
		Content: fmt.Sprintf(`apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-apiserver-and-dns-egress
  namespace: %s
spec:
  podSelector:
    matchLabels:
      app: %s
  policyTypes:
    - Egress
  egress:
    - to:
%s
      ports:
%s
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: openshift-dns
          podSelector:
            matchLabels:
              dns.operator.openshift.io/daemonset-dns: default
      ports:
        - protocol: UDP
          port: 53
        - protocol: TCP
          port: 53
        - protocol: UDP
          port: 5353
        - protocol: TCP
          port: 5353
`, ccmNamespace, ccmDeploymentName,
			networkPolicyIPBlockDestinations(egressDestinations.all()),
			networkPolicyTCPPorts(egressDestinations.apiPorts())),
	})

	manifests = append(manifests, ManifestEntry{
		Filename: "05-ccm-deployment.yaml",
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
            - --cluster-name=%s
            - --controllers=cloud-node-controller,cloud-node-lifecycle-controller,service-lb-controller,kubevirt_eps_controller
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
        - name: kubevirt-lb-policy-controller
          image: %s
          command: ["/bin/bash", "-c"]
          args:
            - |
              set -euo pipefail

              NAMESPACE="openshift-cloud-controller-manager"
              INFRA_API_CIDRS_FILE="/tmp/kubevirt-ccm-infra-api-cidrs"
              printf '%%s\n' ${INFRA_API_CIDRS//,/ } > "$INFRA_API_CIDRS_FILE"

              log() {
                echo "[$(date -u '+%%Y-%%m-%%dT%%H:%%M:%%SZ')] $*"
              }

              run_with_egress_lock() {
                exec 9>/tmp/kubevirt-ccm-egress.lock
                flock 9
                local status=0
                "$@" || status=$?
                flock -u 9
                exec 9>&-
                return "$status"
              }

              watch_tenant_services() {
                local namespace name type load_balancer_class policy opt_out
                while true; do
                  if oc get service -A \
                    --field-selector spec.type=LoadBalancer \
                    --watch --no-headers \
                    -o 'custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name,TYPE:.spec.type,CLASS:.spec.loadBalancerClass,POLICY:.spec.externalTrafficPolicy,OPT_OUT:.metadata.annotations.capoa\.openshift\.io/lb-external-traffic-policy' |
                  while read -r namespace name type load_balancer_class policy opt_out; do
                    if [ "$type" = "LoadBalancer" ] && { [ -z "$load_balancer_class" ] || [ "$load_balancer_class" = "<none>" ]; } && [ "$policy" != "Local" ]; then
                      if [ "$opt_out" != "Cluster" ]; then
                        log "setting tenant Service ${namespace}/${name} externalTrafficPolicy from ${policy:-unset} to Local"
                        if ! oc --request-timeout=30s patch service -n "$namespace" "$name" \
                          -p '{"spec":{"externalTrafficPolicy":"Local"}}' >/dev/null; then
                          log "failed to set tenant Service ${namespace}/${name} externalTrafficPolicy; retrying watch"
                          break
                        fi
                      fi
                    fi
                  done; then
                    log "tenant Service watch stopped; restarting in 5 seconds"
                  else
                    log "tenant Service watch failed; retrying in 5 seconds"
                  fi
                  sleep 5
                done
              }

              watch_infra_services() {
                local name type load_balancer_class policy opt_out
                while true; do
                  if oc --kubeconfig /etc/kubernetes/infra-kubeconfig/kubeconfig \
                    -n "$INFRA_NAMESPACE" get service \
                    -l "cluster.x-k8s.io/tenant-service-name,cluster.x-k8s.io/cluster-name=$TENANT_CLUSTER_NAME" \
                    --field-selector spec.type=LoadBalancer \
                    --watch --no-headers \
                    -o 'custom-columns=NAME:.metadata.name,TYPE:.spec.type,CLASS:.spec.loadBalancerClass,POLICY:.spec.externalTrafficPolicy,OPT_OUT:.metadata.annotations.capoa\.openshift\.io/lb-external-traffic-policy' |
                  while read -r name type load_balancer_class policy opt_out; do
                    if [ "$type" = "LoadBalancer" ] && { [ -z "$load_balancer_class" ] || [ "$load_balancer_class" = "<none>" ]; } && [ "$policy" != "Local" ]; then
                      if [ "$opt_out" != "Cluster" ]; then
                        log "setting infra Service ${INFRA_NAMESPACE}/${name} externalTrafficPolicy from ${policy:-unset} to Local"
                        if ! oc --request-timeout=30s --kubeconfig /etc/kubernetes/infra-kubeconfig/kubeconfig \
                          -n "$INFRA_NAMESPACE" patch service "$name" \
                          -p '{"spec":{"externalTrafficPolicy":"Local"}}' >/dev/null; then
                          log "failed to set infra Service ${INFRA_NAMESPACE}/${name} externalTrafficPolicy; retrying watch"
                          break
                        fi
                      fi
                    fi
                  done; then
                    log "infra Service watch stopped; restarting in 5 seconds"
                  else
                    log "infra Service watch failed; retrying in 5 seconds"
                  fi
                  sleep 5
                done
              }

              reconcile_tenant_api_egress() {
                local endpoint_output ready addresses address cidr first=true
                local -A seen=()
                local destinations_json="["

                load_infra_api_cidrs

                if ! endpoint_output=$(oc --request-timeout=30s --kubeconfig /etc/kubernetes/infra-kubeconfig/kubeconfig \
                  -n "$INFRA_NAMESPACE" get endpointslices \
                  -l "kubernetes.io/service-name=$TENANT_API_SERVICE" \
                  -o jsonpath='{range .items[*].endpoints[*]}{.conditions.ready}{"|"}{.addresses[*]}{"\n"}{end}'); then
                  log "failed to list tenant API EndpointSlices; retrying"
                  return 1
                fi

                for cidr in ${infra_api_cidrs[@]} ${BOOTSTRAP_API_CIDRS//,/ }; do
                  [[ -n "$cidr" ]] || continue
                  if [[ "$first" == "true" ]]; then
                    first=false
                  else
                    destinations_json+=","
                  fi
                  destinations_json+="{\"ipBlock\":{\"cidr\":\"$cidr\"}}"
                  seen["$cidr"]=1
                done

                while IFS='|' read -r ready addresses; do
                  [ "$ready" = "false" ] && continue
                  for address in $addresses; do
                    [[ "$address" =~ ^[0-9A-Fa-f:.]+$ ]] || continue
                    if [[ "$address" == *:* ]]; then
                      cidr="$address/128"
                    else
                      cidr="$address/32"
                    fi
                    [[ -n "${seen[$cidr]+x}" ]] && continue
                    seen["$cidr"]=1
                    if [[ "$first" == "true" ]]; then
                      first=false
                    else
                      destinations_json+=","
                    fi
                    destinations_json+="{\"ipBlock\":{\"cidr\":\"$cidr\"}}"
                  done
                done <<< "$endpoint_output"

                destinations_json+="]"
                if ! oc --request-timeout=30s patch networkpolicy \
                  -n "$NAMESPACE" allow-apiserver-and-dns-egress \
                  --type=json \
                  -p "[{\"op\":\"replace\",\"path\":\"/spec/egress/0/to\",\"value\":$destinations_json}]"; then
                  log "failed to update tenant API egress destinations; retrying"
                  return 1
                fi
              }

              watch_tenant_api_endpoints() {
                local name
                while true; do
                  if oc --kubeconfig /etc/kubernetes/infra-kubeconfig/kubeconfig \
                    -n "$INFRA_NAMESPACE" get endpointslices \
                    -l "kubernetes.io/service-name=$TENANT_API_SERVICE" \
                    --watch --no-headers \
                    -o 'custom-columns=NAME:.metadata.name' |
                  while read -r name; do
                    if ! run_with_egress_lock reconcile_tenant_api_egress; then
                      break
                    fi
                  done; then
                    log "tenant API EndpointSlice watch stopped; restarting in 5 seconds"
                  else
                    log "tenant API EndpointSlice watch failed; retrying in 5 seconds"
                  fi
                  sleep 5
                done
              }

              resolve_infra_api_cidrs() {
                local output address cidr existing
                infra_api_cidrs_changed=false
                load_infra_api_cidrs
                if ! output=$(getent ahosts "$INFRA_API_HOST"); then
                  log "failed to resolve infra API server $INFRA_API_HOST; retaining current destinations"
                  return 1
                fi

                while read -r address _; do
                  [[ "$address" =~ ^[0-9A-Fa-f:.]+$ ]] || continue
                  if [[ "$address" == *:* ]]; then
                    cidr="$address/128"
                  else
                    cidr="$address/32"
                  fi

                  for existing in "${infra_api_cidrs[@]}"; do
                    [[ "$existing" == "$cidr" ]] && continue 2
                  done
                  infra_api_cidrs+=("$cidr")
                  infra_api_cidrs_changed=true
                done <<< "$output"
                return 0
              }

              load_infra_api_cidrs() {
                local cidr
                infra_api_cidrs=()
                if [[ -s "$INFRA_API_CIDRS_FILE" ]]; then
                  while read -r cidr; do
                    [[ "$cidr" =~ ^[0-9A-Fa-f:.]+/[0-9]+$ ]] || continue
                    infra_api_cidrs+=("$cidr")
                  done < "$INFRA_API_CIDRS_FILE"
                  return 0
                fi

                read -r -a infra_api_cidrs <<< "${INFRA_API_CIDRS//,/ }"
              }

              save_infra_api_cidrs() {
                local cidr
                : > "$INFRA_API_CIDRS_FILE"
                for cidr in "${infra_api_cidrs[@]}"; do
                  [[ -n "$cidr" ]] || continue
                  printf '%%s\n' "$cidr" >> "$INFRA_API_CIDRS_FILE"
                done
              }

              patch_infra_api_egress() {
                local current_output cidr first=true
                local -A seen=()
                local destinations_json="["

                if ! current_output=$(oc --request-timeout=30s get networkpolicy \
                  -n "$NAMESPACE" allow-apiserver-and-dns-egress \
                  -o jsonpath='{range .spec.egress[0].to[*]}{.ipBlock.cidr}{"\n"}{end}'); then
                  log "failed to read CCM egress destinations; retrying"
                  return 1
                fi

                while read -r cidr; do
                  [[ "$cidr" =~ ^[0-9A-Fa-f:.]+/[0-9]+$ ]] || continue
                  [[ -n "${seen[$cidr]+x}" ]] && continue
                  seen["$cidr"]=1
                  if [[ "$first" == "true" ]]; then
                    first=false
                  else
                    destinations_json+=","
                  fi
                  destinations_json+="{\"ipBlock\":{\"cidr\":\"$cidr\"}}"
                done <<< "$current_output"

                for cidr in "${infra_api_cidrs[@]}"; do
                  [[ -n "$cidr" ]] || continue
                  [[ -n "${seen[$cidr]+x}" ]] && continue
                  seen["$cidr"]=1
                  if [[ "$first" == "true" ]]; then
                    first=false
                  else
                    destinations_json+=","
                  fi
                  destinations_json+="{\"ipBlock\":{\"cidr\":\"$cidr\"}}"
                done

                destinations_json+="]"
                if ! oc --request-timeout=30s patch networkpolicy \
                  -n "$NAMESPACE" allow-apiserver-and-dns-egress \
                  --type=json \
                  -p "[{\"op\":\"replace\",\"path\":\"/spec/egress/0/to\",\"value\":$destinations_json}]"; then
                  log "failed to update infra API egress destinations; retrying"
                  return 1
                fi
              }

              refresh_infra_api_egress() {
                if ! resolve_infra_api_cidrs; then
                  return 0
                fi
                if [[ "$infra_api_cidrs_changed" != "true" ]]; then
                  return 0
                fi
                patch_infra_api_egress || return 1
                save_infra_api_cidrs || return 1
                reconcile_tenant_api_egress
              }

              watch_infra_api_dns() {
                while true; do
                  if ! run_with_egress_lock refresh_infra_api_egress; then
                    log "failed to update infra API egress destinations; retrying"
                    sleep 5
                  else
                    sleep 60
                  fi
                done
              }

              watch_tenant_services &
              tenant_watch_pid=$!
              watch_infra_services &
              infra_watch_pid=$!
              watch_tenant_api_endpoints &
              tenant_api_watch_pid=$!
              infra_api_dns_watch_pid=""
              if [[ -n "$INFRA_API_HOST" ]]; then
                watch_infra_api_dns &
                infra_api_dns_watch_pid=$!
              fi
              while true; do
                if ! wait -n; then
                  log "a CCM sidecar watch process exited unexpectedly; restarting it"
                fi
                if ! kill -0 "$tenant_watch_pid" 2>/dev/null; then
                  watch_tenant_services &
                  tenant_watch_pid=$!
                fi
                if ! kill -0 "$infra_watch_pid" 2>/dev/null; then
                  watch_infra_services &
                  infra_watch_pid=$!
                fi
                if ! kill -0 "$tenant_api_watch_pid" 2>/dev/null; then
                  watch_tenant_api_endpoints &
                  tenant_api_watch_pid=$!
                fi
                if [[ -n "$infra_api_dns_watch_pid" ]] && ! kill -0 "$infra_api_dns_watch_pid" 2>/dev/null; then
                  watch_infra_api_dns &
                  infra_api_dns_watch_pid=$!
                fi
              done
          env:
            - name: INFRA_NAMESPACE
              value: %s
            - name: TENANT_CLUSTER_NAME
              value: %s
            - name: INFRA_API_HOST
              value: %s
            - name: INFRA_API_CIDRS
              value: %s
            - name: BOOTSTRAP_API_CIDRS
              value: %s
            - name: TENANT_API_SERVICE
              value: %s
          volumeMounts:
            - name: infra-kubeconfig
              mountPath: /etc/kubernetes/infra-kubeconfig
              readOnly: true
          resources:
            requests:
              cpu: 10m
              memory: 32Mi
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
			ccmServiceAccount, ccmImage, ccmNamespace, clusterName, GetJobImage(oseCliImage), infraNamespace, clusterName,
			egressDestinations.InfraAPIHost,
			strings.Join(egressDestinations.InfraAPI, ","), strings.Join(egressDestinations.TenantAPI, ","),
			clusterName+APIServiceNameSuffix, ccmCredSecretName),
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
			Filename: "06-ccm-operator-script.yaml",
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
			Filename: "07-ccm-operator-rbac.yaml",
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
			Filename: "08-ccm-operator-deployment.yaml",
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
