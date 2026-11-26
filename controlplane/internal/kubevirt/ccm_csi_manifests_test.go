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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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

var _ = Describe("CCM Manifests", func() {
	const (
		testCurrentContext = "current"
		testInfraNS        = "infra-test-ns"
		testCCMImage       = "registry.example.com/kubevirt-ccm@sha256:abc123"
		testOseCliImage    = "registry.example.com/ose-cli@sha256:cli123"
	)

	It("should return nil when CCM is not enabled", func() {
		oacp := newOACP(false, false, "")
		result := GenerateCCMManifests(oacp, testInfraNS, testCCMImage, testOseCliImage, nil)
		Expect(result).To(BeNil())
	})

	It("should return nil when CCM spec is nil", func() {
		oacp := newOACP(false, false, "")
		oacp.Spec.Config.CloudControllerManager = nil
		result := GenerateCCMManifests(oacp, testInfraNS, testCCMImage, testOseCliImage, nil)
		Expect(result).To(BeNil())
	})

	It("should return nil when CCM image is empty", func() {
		oacp := newOACP(true, false, "")
		result := GenerateCCMManifests(oacp, testInfraNS, "", testOseCliImage, nil)
		Expect(result).To(BeNil())
	})

	It("should generate all required manifests when CCM is enabled", func() {
		oacp := newOACP(true, false, "")
		result := GenerateCCMManifests(oacp, testInfraNS, testCCMImage, testOseCliImage, nil)

		Expect(result).To(HaveLen(8))
		Expect(result[0].Filename).To(Equal("01-ccm-namespace.yaml"))
		Expect(result[1].Filename).To(Equal("02-ccm-cloud-config.yaml"))
		Expect(result[2].Filename).To(Equal("03-ccm-bootstrap-egress.yaml"))
		Expect(result[3].Filename).To(Equal("04-ccm-rbac.yaml"))
		Expect(result[4].Filename).To(Equal("05-ccm-deployment.yaml"))
		Expect(result[5].Filename).To(Equal("06-ccm-operator-script.yaml"))
		Expect(result[6].Filename).To(Equal("07-ccm-operator-rbac.yaml"))
		Expect(result[7].Filename).To(Equal("08-ccm-operator-deployment.yaml"))
	})

	It("should omit operator manifests when oseCliImage is empty", func() {
		oacp := newOACP(true, false, "")
		result := GenerateCCMManifests(oacp, testInfraNS, testCCMImage, "", nil)

		Expect(result).To(HaveLen(5))
		Expect(result[4].Filename).To(Equal("05-ccm-deployment.yaml"))
	})

	It("should set the infra namespace in cloud-config", func() {
		oacp := newOACP(true, false, "")
		result := GenerateCCMManifests(oacp, testInfraNS, testCCMImage, testOseCliImage, nil)

		Expect(result[1].Content).To(ContainSubstring("namespace: " + testInfraNS))
		Expect(result[1].Content).To(ContainSubstring("selectorless: false"))
		Expect(result[1].Content).NotTo(ContainSubstring("enableEPSController"))
		Expect(result[2].Content).To(ContainSubstring("port: 6443"))
	})

	It("should use the resolved CCM image directly in the deployment", func() {
		oacp := newOACP(true, false, "")
		result := GenerateCCMManifests(oacp, testInfraNS, testCCMImage, testOseCliImage, nil)

		Expect(result[4].Content).To(ContainSubstring("image: " + testCCMImage))
		Expect(result[4].Content).To(ContainSubstring("--cluster-name=test-cluster"))
		Expect(result[4].Content).To(ContainSubstring(
			"--controllers=cloud-node-controller,cloud-node-lifecycle-controller,service-lb-controller",
		))
	})

	It("should not require CCM EndpointSlice access", func() {
		oacp := newOACP(true, false, "")
		result := GenerateCCMManifests(oacp, testInfraNS, testCCMImage, testOseCliImage, nil)

		Expect(result[3].Content).NotTo(ContainSubstring("apiGroups: [\"discovery.k8s.io\"]"))
		Expect(result[3].Content).NotTo(ContainSubstring("resources: [\"endpointslices\"]"))
	})

	It("should reference the infra credentials secret in the deployment", func() {
		oacp := newOACP(true, false, "")
		result := GenerateCCMManifests(oacp, testInfraNS, testCCMImage, testOseCliImage, nil)

		Expect(result[4].Content).To(ContainSubstring("secretName: " + ccmCredSecretName))
	})

	It("should use ose-cli image in the operator deployment", func() {
		oacp := newOACP(true, false, "")
		result := GenerateCCMManifests(oacp, testInfraNS, testCCMImage, testOseCliImage, nil)

		Expect(result[7].Content).To(ContainSubstring("image: " + testOseCliImage))
		Expect(result[7].Content).To(ContainSubstring("value: " + testInfraNS))
		Expect(result[7].Content).To(ContainSubstring("value: test-cluster"))
	})

	It("should watch and normalize mirrored LoadBalancer Services", func() {
		oacp := newOACP(true, false, "")
		result := GenerateCCMManifests(oacp, testInfraNS, testCCMImage, testOseCliImage, nil)

		Expect(result[5].Content).To(ContainSubstring("--watch"))
		Expect(result[5].Content).To(ContainSubstring(`\"externalTrafficPolicy\":\"Local\"`))
		Expect(result[5].Content).To(ContainSubstring(`{.spec.selector}`))
		Expect(result[5].Content).To(ContainSubstring(`mirrored_service_is_normalized`))
		Expect(result[5].Content).To(ContainSubstring(`\"cluster.x-k8s.io/cluster-name\":\"$INFRA_CLUSTER_NAME\"`))
		Expect(result[5].Content).To(ContainSubstring(`\"cluster.x-k8s.io/role\":null`))
		Expect(result[5].Content).To(ContainSubstring(`\"kubevirt.io\":\"virt-launcher\"`))
		Expect(result[5].Content).To(ContainSubstring(`\"kubevirt.io\":\"virt-launcher\"}}}"`))

		match := regexp.MustCompile(`--type=merge -p ("(?:\\.|[^"\\])*")`).FindStringSubmatch(result[5].Content)
		Expect(match).To(HaveLen(2))
		payload, err := strconv.Unquote(match[1])
		Expect(err).NotTo(HaveOccurred())
		var patch map[string]any
		Expect(json.Unmarshal([]byte(payload), &patch)).To(Succeed())
		Expect(patch).To(HaveKey("spec"))
	})

	It("should distinguish an absent role selector from a present empty value", func() {
		runNormalization := func(policy string, selector string) bool {
			script := fmt.Sprintf(`#!/bin/bash
set -euo pipefail
INFRA_CLUSTER_NAME=test-cluster
%s
if mirrored_service_is_normalized %q %q; then
  exit 0
fi
exit 1
`, mirroredServiceNormalizationFunction, policy, selector)
			err := exec.Command("bash", "-c", script).Run()
			if err == nil {
				return true
			}
			var exitErr *exec.ExitError
			Expect(errors.As(err, &exitErr)).To(BeTrue())
			Expect(exitErr.ExitCode()).To(Equal(1))
			return false
		}

		normalizedSelector := `{"cluster.x-k8s.io/cluster-name":"test-cluster","kubevirt.io":"virt-launcher"}`
		Expect(runNormalization("Local", normalizedSelector)).To(BeTrue())
		Expect(runNormalization("Local", `{"cluster.x-k8s.io/cluster-name":"test-cluster","cluster.x-k8s.io/role":"worker","kubevirt.io":"virt-launcher"}`)).To(BeFalse())
		Expect(runNormalization("Local", `{"cluster.x-k8s.io/cluster-name":"test-cluster","cluster.x-k8s.io/role":"","kubevirt.io":"virt-launcher"}`)).To(BeFalse())
	})

	It("should allow the infrastructure proxy endpoint", func() {
		oacp := newOACP(true, false, "")
		proxyEndpoints := []proxyEndpoint{{CIDR: "192.0.2.20/32", Port: 3128}}
		result := GenerateCCMManifests(oacp, testInfraNS, testCCMImage, testOseCliImage, proxyEndpoints)

		Expect(result[2].Content).To(ContainSubstring("cidr: 192.0.2.20/32"))
		Expect(result[2].Content).To(ContainSubstring("port: 3128"))
	})

	It("should allow egress for the CCM and its operator", func() {
		oacp := newOACP(true, false, "")
		result := GenerateCCMManifests(oacp, testInfraNS, testCCMImage, testOseCliImage, nil)

		Expect(result[2].Content).To(ContainSubstring("operator: In"))
		Expect(result[2].Content).To(ContainSubstring(ccmDeploymentName))
		Expect(result[2].Content).To(ContainSubstring("kubevirt-ccm-operator"))
	})

	It("should resolve proxy endpoints from the infra kubeconfig", func() {
		config := clientcmdapi.NewConfig()
		config.CurrentContext = testCurrentContext
		config.Contexts[testCurrentContext] = &clientcmdapi.Context{Cluster: "selected-infra"}
		config.Contexts["unused"] = &clientcmdapi.Context{Cluster: "unused-infra"}
		config.Clusters["selected-infra"] = &clientcmdapi.Cluster{ProxyURL: "http://192.0.2.20:3128"}
		config.Clusters["unused-infra"] = &clientcmdapi.Cluster{ProxyURL: "http://192.0.2.30:3128"}
		kubeconfig, err := clientcmd.Write(*config)
		Expect(err).NotTo(HaveOccurred())

		oacp := newOACP(true, false, "")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "infra-cluster-credentials", Namespace: oacp.Namespace},
			Data:       map[string][]byte{"kubeconfig": kubeconfig},
		}
		fakeClient := fake.NewClientBuilder().WithObjects(secret).Build()

		endpoints, err := infraProxyEndpoints(context.Background(), fakeClient, oacp)
		Expect(err).NotTo(HaveOccurred())
		Expect(endpoints).To(Equal([]proxyEndpoint{{CIDR: "192.0.2.20/32", Port: 3128}}))
	})

	It("should default SOCKS5 proxy ports", func() {
		config := clientcmdapi.NewConfig()
		config.CurrentContext = testCurrentContext
		config.Contexts[testCurrentContext] = &clientcmdapi.Context{Cluster: "infra"}
		config.Clusters["infra"] = &clientcmdapi.Cluster{ProxyURL: "socks5://192.0.2.20"}
		kubeconfig, err := clientcmd.Write(*config)
		Expect(err).NotTo(HaveOccurred())

		oacp := newOACP(true, false, "")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "infra-cluster-credentials", Namespace: oacp.Namespace},
			Data:       map[string][]byte{"kubeconfig": kubeconfig},
		}
		fakeClient := fake.NewClientBuilder().WithObjects(secret).Build()

		endpoints, err := infraProxyEndpoints(context.Background(), fakeClient, oacp)
		Expect(err).NotTo(HaveOccurred())
		Expect(endpoints).To(Equal([]proxyEndpoint{{CIDR: "192.0.2.20/32", Port: 1080}}))
	})

	It("should replace stale manifest keys", func() {
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(controlplanev1alpha3.AddToScheme(scheme)).To(Succeed())
		oacp := newOACP(true, false, "")
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "manifests", Namespace: oacp.Namespace},
			Data:       map[string]string{"old.yaml": "old"},
		}
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(configMap).Build()

		err := ensureManifestsConfigMap(context.Background(), fakeClient, oacp, "manifests", []ManifestEntry{
			{Filename: "new.yaml", Content: "new"},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &corev1.ConfigMap{}
		Expect(fakeClient.Get(context.Background(), client.ObjectKeyFromObject(configMap), updated)).To(Succeed())
		Expect(updated.Data).To(Equal(map[string]string{"new.yaml": "new"}))
	})

	It("should rewrite the infra kubeconfig for the internal API", func() {
		config := clientcmdapi.NewConfig()
		config.CurrentContext = testCurrentContext
		config.Contexts[testCurrentContext] = &clientcmdapi.Context{Cluster: "infra"}
		config.Clusters["infra"] = &clientcmdapi.Cluster{Server: "https://api.example.com:6443"}
		config.Clusters["unused"] = &clientcmdapi.Cluster{}
		kubeconfig, err := clientcmd.Write(*config)
		Expect(err).NotTo(HaveOccurred())

		rewritten, err := rewriteKubeconfigForInternalAPI(string(kubeconfig), "https://192.0.2.10:443")
		Expect(err).NotTo(HaveOccurred())

		parsed, err := clientcmd.Load([]byte(rewritten))
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Clusters["infra"].Server).To(Equal("https://192.0.2.10:443"))
		Expect(parsed.Clusters["infra"].TLSServerName).To(Equal("api.example.com"))
	})

	It("should resolve the infra API server from the Kubernetes Service", func() {
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		apiService := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "kubernetes", Namespace: "default"},
			Spec: corev1.ServiceSpec{
				ClusterIP: "192.0.2.10",
				Ports:     []corev1.ServicePort{{Name: "https", Port: 443}},
			},
		}
		infraClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(apiService).Build()

		server, err := getInfraAPIServer(context.Background(), infraClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(server).To(Equal("https://192.0.2.10:443"))
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
