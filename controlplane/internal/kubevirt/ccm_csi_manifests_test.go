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
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/yaml"
)

func newOACP(ccmEnabled bool, csiEnabled bool, infraSC string) *controlplanev1alpha3.OpenshiftAssistedControlPlane {
	oacp := &controlplanev1alpha3.OpenshiftAssistedControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "test-ns",
		},
	}
	if ccmEnabled {
		oacp.Spec.Config.CloudControllerManager = &controlplanev1alpha3.CCMSpec{Enabled: true}
	}
	if csiEnabled {
		oacp.Spec.Config.CSIDriver = &controlplanev1alpha3.CSIDriverSpec{
			Enabled:           true,
			InfraStorageClass: infraSC,
		}
	}
	return oacp
}

func generateTestCCMManifests(
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	ccmImage string,
	oseCliImage string,
	tenantAPIEndpoints ...string,
) []ManifestEntry {
	return GenerateCCMManifests(
		oacp,
		"infra-test-ns",
		"test-cluster",
		ccmImage,
		oseCliImage,
		CCMEgressDestinations{
			InfraAPI:  []string{"172.30.0.1/32"},
			TenantAPI: tenantAPIEndpoints,
		},
	)
}

var _ = Describe("CCM Manifests", func() {
	const (
		testInfraNS     = "infra-test-ns"
		testInfraAPIIP  = "172.30.0.1"
		testCCMImage    = "registry.example.com/kubevirt-ccm@sha256:abc123"
		testOseCliImage = "registry.example.com/ose-cli@sha256:cli123"
	)

	It("should convert infra API addresses to host CIDRs", func() {
		Expect(infraAPIHostCIDR("192.0.2.10")).To(Equal("192.0.2.10/32"))
		Expect(infraAPIHostCIDR("fd00:1234:5678::1")).To(Equal("fd00:1234:5678::1/128"))
	})

	var (
		tcpProtocol = corev1.ProtocolTCP
		udpProtocol = corev1.ProtocolUDP
		port53      = intstr.FromInt32(53)
		port5353    = intstr.FromInt32(5353)
		port443     = intstr.FromInt32(443)
		port6443    = intstr.FromInt32(6443)
		port8443    = intstr.FromInt32(8443)
	)

	It("should return nil when CCM is not enabled", func() {
		oacp := newOACP(false, false, "")
		result := generateTestCCMManifests(oacp, testCCMImage, testOseCliImage)
		Expect(result).To(BeNil())
	})

	It("should return nil when CCM spec is nil", func() {
		oacp := newOACP(false, false, "")
		oacp.Spec.Config.CloudControllerManager = nil
		result := generateTestCCMManifests(oacp, testCCMImage, testOseCliImage)
		Expect(result).To(BeNil())
	})

	It("should return nil when CCM image is empty", func() {
		oacp := newOACP(true, false, "")
		result := generateTestCCMManifests(oacp, "", testOseCliImage)
		Expect(result).To(BeNil())
	})

	It("should generate all required manifests when CCM is enabled", func() {
		oacp := newOACP(true, false, "")
		result := generateTestCCMManifests(oacp, testCCMImage, testOseCliImage)

		Expect(result).To(HaveLen(8))
		Expect(result[0].Filename).To(Equal("01-ccm-namespace.yaml"))
		Expect(result[1].Filename).To(Equal("02-ccm-cloud-config.yaml"))
		Expect(result[2].Filename).To(Equal("03-ccm-rbac.yaml"))
		Expect(result[3].Filename).To(Equal("04-ccm-egress-network-policy.yaml"))
		Expect(result[4].Filename).To(Equal("05-ccm-deployment.yaml"))
		Expect(result[5].Filename).To(Equal("06-ccm-operator-script.yaml"))
		Expect(result[6].Filename).To(Equal("07-ccm-operator-rbac.yaml"))
		Expect(result[7].Filename).To(Equal("08-ccm-operator-deployment.yaml"))
	})

	It("should omit operator manifests when oseCliImage is empty", func() {
		oacp := newOACP(true, false, "")
		result := generateTestCCMManifests(oacp, testCCMImage, "")

		Expect(result).To(HaveLen(5))
		Expect(result[4].Filename).To(Equal("05-ccm-deployment.yaml"))
	})

	It("should set the infra namespace in cloud-config", func() {
		oacp := newOACP(true, false, "")
		result := generateTestCCMManifests(oacp, testCCMImage, testOseCliImage)

		Expect(result[1].Content).To(ContainSubstring("namespace: " + testInfraNS))
	})

	It("should use the resolved CCM image directly in the deployment", func() {
		oacp := newOACP(true, false, "")
		result := generateTestCCMManifests(oacp, testCCMImage, testOseCliImage)

		Expect(result[4].Content).To(ContainSubstring("image: " + testCCMImage))
	})

	It("should reference the infra credentials secret in the deployment", func() {
		oacp := newOACP(true, false, "")
		result := generateTestCCMManifests(oacp, testCCMImage, testOseCliImage)

		Expect(result[4].Content).To(ContainSubstring("secretName: " + ccmCredSecretName))
	})

	It("should use canonical controller names", func() {
		oacp := newOACP(true, false, "")
		result := generateTestCCMManifests(oacp, testCCMImage, testOseCliImage)

		Expect(result[4].Content).To(ContainSubstring(
			"--controllers=cloud-node-controller,cloud-node-lifecycle-controller,service-lb-controller"))
		Expect(result[4].Content).NotTo(ContainSubstring("--controllers=cloud-node,cloud-node-lifecycle"))
	})

	It("should preserve a bootstrap tenant API destination when no endpoint is ready", func() {
		destinations := withTenantAPIBootstrapDestination(
			CCMEgressDestinations{InfraAPI: []string{testInfraAPIIP + "/32"}},
			&ServiceIPs{APIClusterIP: "10.132.0.10"},
		)

		Expect(destinations.TenantAPI).To(Equal([]string{"10.132.0.10/32"}))
	})

	It("should enable the KubeVirt EndpointSlice controller", func() {
		oacp := newOACP(true, false, "")
		result := generateTestCCMManifests(oacp, testCCMImage, testOseCliImage)

		Expect(result[1].Content).To(ContainSubstring("enableEPSController: true"))
		Expect(result[2].Content).To(ContainSubstring(`apiGroups: ["discovery.k8s.io"]`))
		Expect(result[2].Content).To(ContainSubstring(`resources: ["endpointslices"]`))
		Expect(result[2].Content).To(ContainSubstring(`verbs: ["get", "list", "watch"]`))
		Expect(result[2].Content).To(ContainSubstring(`resources: ["networkpolicies"]`))
	})

	It("should default LoadBalancer Services to Local traffic policy", func() {
		oacp := newOACP(true, false, "")
		result := generateTestCCMManifests(oacp, testCCMImage, testOseCliImage)

		var deployment appsv1.Deployment
		Expect(yaml.Unmarshal([]byte(result[4].Content), &deployment)).To(Succeed())
		Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(2))
		Expect(deployment.Spec.Template.Spec.Containers[1].Name).To(Equal("kubevirt-lb-policy-controller"))
		Expect(deployment.Spec.Template.Spec.Containers[1].Image).To(Equal(testOseCliImage))
		Expect(deployment.Spec.Template.Spec.Containers[1].Command).To(ContainElement("/bin/bash"))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args).To(HaveLen(1))
		Expect(deployment.Spec.Template.Spec.Containers[1].VolumeMounts).To(ContainElement(corev1.VolumeMount{
			Name:      "infra-kubeconfig",
			MountPath: "/etc/kubernetes/infra-kubeconfig",
			ReadOnly:  true,
		}))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring(`externalTrafficPolicy":"Local`))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring("cluster.x-k8s.io/tenant-service-name"))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring(`-l "cluster.x-k8s.io/tenant-service-name,cluster.x-k8s.io/cluster-name=$TENANT_CLUSTER_NAME"`))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring("CLASS:.spec.loadBalancerClass"))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring(`OPT_OUT:.metadata.annotations.capoa\.openshift\.io/lb-external-traffic-policy`))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring(`-o 'custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name,TYPE:.spec.type,CLASS:.spec.loadBalancerClass,POLICY:.spec.externalTrafficPolicy,OPT_OUT:.metadata.annotations.capoa\.openshift\.io/lb-external-traffic-policy'`))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring(`-o 'custom-columns=NAME:.metadata.name,TYPE:.spec.type,CLASS:.spec.loadBalancerClass,POLICY:.spec.externalTrafficPolicy,OPT_OUT:.metadata.annotations.capoa\.openshift\.io/lb-external-traffic-policy'`))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring(`[ "$load_balancer_class" = "<none>" ]`))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring(`[ "$opt_out" != "Cluster" ]`))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring("watch_tenant_services"))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring("watch_infra_services"))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring("watch_tenant_api_endpoints"))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring(`NAMESPACE="openshift-cloud-controller-manager"`))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring("--watch --no-headers"))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring("--field-selector spec.type=LoadBalancer"))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring(`-l "kubernetes.io/service-name=$TENANT_API_SERVICE"`))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring("reconcile_tenant_api_egress"))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring("patch networkpolicy"))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring(`{"|"}`))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring(`IFS='|'`))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring("oc --request-timeout=30s patch service"))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring("oc --request-timeout=30s --kubeconfig /etc/kubernetes/infra-kubeconfig/kubeconfig"))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring("tenant_watch_pid"))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring("a CCM sidecar watch process exited unexpectedly; restarting it"))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring("setting tenant Service"))
		Expect(deployment.Spec.Template.Spec.Containers[1].Args[0]).To(ContainSubstring("setting infra Service"))
	})

	It("should generate a syntactically valid sidecar script", func() {
		oacp := newOACP(true, false, "")
		result := generateTestCCMManifests(oacp, testCCMImage, testOseCliImage)

		var deployment appsv1.Deployment
		Expect(yaml.Unmarshal([]byte(result[4].Content), &deployment)).To(Succeed())
		script := deployment.Spec.Template.Spec.Containers[1].Args[0]
		command := exec.Command("bash", "-n")
		command.Stdin = strings.NewReader(script)
		Expect(command.Run()).To(Succeed())
	})

	It("should scope the infra Service watch to the tenant Cluster", func() {
		oacp := newOACP(true, false, "")
		result := generateTestCCMManifests(oacp, testCCMImage, testOseCliImage)

		Expect(result[4].Content).To(ContainSubstring("--cluster-name=test-cluster"))
		Expect(result[4].Content).To(ContainSubstring("- name: TENANT_CLUSTER_NAME\n              value: test-cluster"))
	})

	It("should refresh hostname-based infra API egress from the tenant resolver", func() {
		oacp := newOACP(true, false, "")
		result := GenerateCCMManifests(
			oacp,
			"infra-test-ns",
			"test-cluster",
			testCCMImage,
			testOseCliImage,
			CCMEgressDestinations{
				InfraAPI:     []string{"192.0.2.10/32"},
				InfraAPIHost: "api.infra.example.com",
			},
		)

		Expect(result[4].Content).To(ContainSubstring("- name: INFRA_API_HOST\n              value: api.infra.example.com"))
		Expect(result[4].Content).To(ContainSubstring("getent ahosts \"$INFRA_API_HOST\""))
		Expect(result[4].Content).To(ContainSubstring("INFRA_API_CIDRS_FILE=\"/tmp/kubevirt-ccm-infra-api-cidrs\""))
		Expect(result[4].Content).To(ContainSubstring("load_infra_api_cidrs"))
		Expect(result[4].Content).To(ContainSubstring("watch_infra_api_dns"))
		Expect(result[4].Content).To(ContainSubstring("patch_infra_api_egress"))
		Expect(result[4].Content).To(ContainSubstring("patch_infra_api_egress || return 1\n                save_infra_api_cidrs || return 1"))
		Expect(result[4].Content).To(ContainSubstring("run_with_egress_lock reconcile_tenant_api_egress"))
	})

	It("should use the CAPI cluster name for the tenant API Service", func() {
		oacp := newOACP(true, false, "")
		result := GenerateCCMManifests(
			oacp,
			"infra-test-ns",
			"capi-cluster-name",
			testCCMImage,
			testOseCliImage,
			CCMEgressDestinations{InfraAPI: []string{testInfraAPIIP + "/32"}},
		)

		Expect(result[4].Content).To(ContainSubstring("name: TENANT_API_SERVICE"))
		Expect(result[4].Content).To(ContainSubstring("value: capi-cluster-name-api"))
	})

	It("should allow API and DNS egress before deploying CCM workloads", func() {
		oacp := newOACP(true, false, "")
		result := generateTestCCMManifests(oacp, testCCMImage, testOseCliImage)

		var networkPolicy networkingv1.NetworkPolicy
		Expect(yaml.Unmarshal([]byte(result[3].Content), &networkPolicy)).To(Succeed())
		Expect(networkPolicy.Spec.PolicyTypes).To(ConsistOf(networkingv1.PolicyTypeEgress))
		Expect(networkPolicy.Spec.Egress).To(HaveLen(2))

		Expect(networkPolicy.Spec.Egress[0].To).To(ContainElement(networkingv1.NetworkPolicyPeer{
			IPBlock: &networkingv1.IPBlock{CIDR: testInfraAPIIP + "/32"},
		}))
		Expect(networkPolicy.Spec.Egress[0].To).To(HaveLen(1))
		for _, destination := range networkPolicy.Spec.Egress[0].To {
			Expect(destination.NamespaceSelector).To(BeNil())
			Expect(destination.PodSelector).To(BeNil())
		}
		Expect(networkPolicy.Spec.Egress[0].Ports).To(ConsistOf(
			networkingv1.NetworkPolicyPort{Protocol: &tcpProtocol, Port: &port443},
			networkingv1.NetworkPolicyPort{Protocol: &tcpProtocol, Port: &port6443},
		))

		Expect(networkPolicy.Spec.Egress[1].To).To(ContainElement(networkingv1.NetworkPolicyPeer{
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"kubernetes.io/metadata.name": "openshift-dns"},
			},
			PodSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"dns.operator.openshift.io/daemonset-dns": "default"},
			},
		}))
		Expect(networkPolicy.Spec.Egress[1].Ports).To(ConsistOf(
			networkingv1.NetworkPolicyPort{Protocol: &udpProtocol, Port: &port53},
			networkingv1.NetworkPolicyPort{Protocol: &tcpProtocol, Port: &port53},
			networkingv1.NetworkPolicyPort{Protocol: &udpProtocol, Port: &port5353},
			networkingv1.NetworkPolicyPort{Protocol: &tcpProtocol, Port: &port5353},
		))
	})

	It("should allow the fallback infra API server port", func() {
		oacp := newOACP(true, false, "")
		result := GenerateCCMManifests(
			oacp,
			"infra-test-ns",
			"test-cluster",
			testCCMImage,
			testOseCliImage,
			CCMEgressDestinations{
				InfraAPI:      []string{testInfraAPIIP + "/32"},
				InfraAPIPorts: []int32{8443},
			},
		)

		var networkPolicy networkingv1.NetworkPolicy
		Expect(yaml.Unmarshal([]byte(result[3].Content), &networkPolicy)).To(Succeed())
		Expect(networkPolicy.Spec.Egress[0].Ports).To(ConsistOf(
			networkingv1.NetworkPolicyPort{Protocol: &tcpProtocol, Port: &port443},
			networkingv1.NetworkPolicyPort{Protocol: &tcpProtocol, Port: &port6443},
			networkingv1.NetworkPolicyPort{Protocol: &tcpProtocol, Port: &port8443},
		))
	})

	It("should allow tenant API egress to every observed API endpoint", func() {
		oacp := newOACP(true, false, "")
		result := generateTestCCMManifests(oacp, testCCMImage, testOseCliImage, "10.132.0.10/32", "fd00:132::10/128")

		Expect(result[4].Content).To(ContainSubstring("name: BOOTSTRAP_API_CIDRS"))
		Expect(result[4].Content).To(ContainSubstring("value: 10.132.0.10/32,fd00:132::10/128"))

		var networkPolicy networkingv1.NetworkPolicy
		Expect(yaml.Unmarshal([]byte(result[3].Content), &networkPolicy)).To(Succeed())
		Expect(networkPolicy.Spec.Egress[0].To).To(ConsistOf(
			networkingv1.NetworkPolicyPeer{IPBlock: &networkingv1.IPBlock{CIDR: "10.132.0.10/32"}},
			networkingv1.NetworkPolicyPeer{IPBlock: &networkingv1.IPBlock{CIDR: "fd00:132::10/128"}},
			networkingv1.NetworkPolicyPeer{IPBlock: &networkingv1.IPBlock{CIDR: testInfraAPIIP + "/32"}},
		))
	})

	It("should restrict the CCM egress NetworkPolicy to CCM pods only", func() {
		oacp := newOACP(true, false, "")
		result := generateTestCCMManifests(oacp, testCCMImage, testOseCliImage)

		Expect(result[3].Content).To(ContainSubstring(
			"podSelector:\n    matchLabels:\n      app: " + ccmDeploymentName))
		Expect(result[3].Content).NotTo(ContainSubstring("podSelector: {}"))
	})

	It("should use ose-cli image in the operator deployment", func() {
		oacp := newOACP(true, false, "")
		result := generateTestCCMManifests(oacp, testCCMImage, testOseCliImage)

		Expect(result[7].Content).To(ContainSubstring("image: " + testOseCliImage))
	})
})

var _ = Describe("CSI Manifests", func() {
	const testInfraNS = "infra-test-ns"
	const testOseCliImage = "registry.example.com/ose-cli@sha256:cli123"

	testImages := CSIImages{
		Driver:        "registry.example.com/kubevirt-csi-driver@sha256:aaa",
		Provisioner:   "registry.example.com/csi-provisioner@sha256:bbb",
		Attacher:      "registry.example.com/csi-attacher@sha256:ccc",
		Snapshotter:   "registry.example.com/csi-snapshotter@sha256:ddd",
		Resizer:       "registry.example.com/csi-resizer@sha256:eee",
		LivenessProbe: "registry.example.com/csi-liveness@sha256:fff",
		NodeRegistrar: "registry.example.com/csi-registrar@sha256:ggg",
	}

	It("should return nil when CSI is not enabled", func() {
		oacp := newOACP(false, false, "")
		result := GenerateCSIManifests(oacp, testInfraNS, testImages, testOseCliImage)
		Expect(result).To(BeNil())
	})

	It("should return nil when CSI spec is nil", func() {
		oacp := newOACP(false, false, "")
		oacp.Spec.Config.CSIDriver = nil
		result := GenerateCSIManifests(oacp, testInfraNS, testImages, testOseCliImage)
		Expect(result).To(BeNil())
	})

	It("should return nil when driver image is empty", func() {
		oacp := newOACP(false, true, "managed-csi")
		emptyImages := CSIImages{}
		result := GenerateCSIManifests(oacp, testInfraNS, emptyImages, testOseCliImage)
		Expect(result).To(BeNil())
	})

	It("should generate all required manifests when CSI is enabled", func() {
		oacp := newOACP(false, true, "managed-csi")
		result := GenerateCSIManifests(oacp, testInfraNS, testImages, testOseCliImage)

		Expect(result).To(HaveLen(13))
		Expect(result[0].Filename).To(Equal("01-csi-namespace.yaml"))
		Expect(result[1].Filename).To(Equal("02-csi-driver.yaml"))
		Expect(result[2].Filename).To(Equal("03-csi-driver-config.yaml"))
		Expect(result[3].Filename).To(Equal("04-csi-serviceaccounts.yaml"))
		Expect(result[4].Filename).To(Equal("05-csi-rbac-controller.yaml"))
		Expect(result[5].Filename).To(Equal("06-csi-rbac-node.yaml"))
		Expect(result[6].Filename).To(Equal("07-csi-scc-rolebindings.yaml"))
		Expect(result[7].Filename).To(Equal("08-csi-controller-deployment.yaml"))
		Expect(result[8].Filename).To(Equal("09-csi-node-daemonset.yaml"))
		Expect(result[9].Filename).To(Equal("10-csi-storageclass.yaml"))
		Expect(result[10].Filename).To(Equal("11-csi-operator-rbac.yaml"))
		Expect(result[11].Filename).To(Equal("12-csi-operator-script.yaml"))
		Expect(result[12].Filename).To(Equal("13-csi-operator-deployment.yaml"))
	})

	It("should omit StorageClass when infraStorageClass is empty", func() {
		oacp := newOACP(false, true, "")
		result := GenerateCSIManifests(oacp, testInfraNS, testImages, testOseCliImage)

		Expect(result).To(HaveLen(12))
		for _, m := range result {
			Expect(m.Filename).NotTo(Equal("10-csi-storageclass.yaml"))
		}
	})

	It("should omit operator manifests when oseCliImage is empty", func() {
		oacp := newOACP(false, true, "managed-csi")
		result := GenerateCSIManifests(oacp, testInfraNS, testImages, "")

		Expect(result).To(HaveLen(10))
		for _, m := range result {
			Expect(m.Filename).NotTo(ContainSubstring("operator"))
		}
	})

	It("should use resolved images in the controller deployment", func() {
		oacp := newOACP(false, true, "managed-csi")
		result := GenerateCSIManifests(oacp, testInfraNS, testImages, testOseCliImage)

		controllerDeployment := result[7].Content
		Expect(controllerDeployment).To(ContainSubstring("image: " + testImages.Driver))
		Expect(controllerDeployment).To(ContainSubstring("image: " + testImages.Provisioner))
		Expect(controllerDeployment).To(ContainSubstring("image: " + testImages.Attacher))
		Expect(controllerDeployment).To(ContainSubstring("image: " + testImages.LivenessProbe))
		Expect(controllerDeployment).To(ContainSubstring("image: " + testImages.Snapshotter))
		Expect(controllerDeployment).To(ContainSubstring("image: " + testImages.Resizer))
	})

	It("should use resolved images in the node DaemonSet", func() {
		oacp := newOACP(false, true, "managed-csi")
		result := GenerateCSIManifests(oacp, testInfraNS, testImages, testOseCliImage)

		nodeDaemonSet := result[8].Content
		Expect(nodeDaemonSet).To(ContainSubstring("image: " + testImages.Driver))
		Expect(nodeDaemonSet).To(ContainSubstring("image: " + testImages.NodeRegistrar))
		Expect(nodeDaemonSet).To(ContainSubstring("image: " + testImages.LivenessProbe))
	})

	It("should set the infra namespace in driver config", func() {
		oacp := newOACP(false, true, "managed-csi")
		result := GenerateCSIManifests(oacp, testInfraNS, testImages, testOseCliImage)

		Expect(result[2].Content).To(ContainSubstring("infraClusterNamespace: \"" + testInfraNS + "\""))
	})

	It("should reference the infra credentials secret in the controller deployment", func() {
		oacp := newOACP(false, true, "managed-csi")
		result := GenerateCSIManifests(oacp, testInfraNS, testImages, testOseCliImage)

		Expect(result[7].Content).To(ContainSubstring("secretName: " + csiCredSecretName))
	})

	It("should set the correct infra StorageClass in the StorageClass manifest", func() {
		oacp := newOACP(false, true, "my-storage-class")
		result := GenerateCSIManifests(oacp, testInfraNS, testImages, testOseCliImage)

		scManifest := result[9]
		Expect(scManifest.Filename).To(Equal("10-csi-storageclass.yaml"))
		Expect(scManifest.Content).To(ContainSubstring("infraStorageClassName: my-storage-class"))
	})

	It("should not contain bash scripts in static manifests", func() {
		oacp := newOACP(false, true, "managed-csi")
		result := GenerateCSIManifests(oacp, testInfraNS, testImages, testOseCliImage)

		for _, m := range result {
			if strings.Contains(m.Filename, "operator") {
				continue
			}
			Expect(strings.ToLower(m.Content)).NotTo(ContainSubstring("#!/bin/bash"))
		}
	})

	It("should use ose-cli image in the operator deployment", func() {
		oacp := newOACP(false, true, "managed-csi")
		result := GenerateCSIManifests(oacp, testInfraNS, testImages, testOseCliImage)

		Expect(result[12].Content).To(ContainSubstring("image: " + testOseCliImage))
	})
})
