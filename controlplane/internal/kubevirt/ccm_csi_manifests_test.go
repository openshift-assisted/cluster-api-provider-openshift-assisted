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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
			Enabled:          true,
			InfraStorageClass: infraSC,
		}
	}
	return oacp
}

var _ = Describe("CCM Manifests", func() {
	const (
		testInfraNS     = "infra-test-ns"
		testCCMImage    = "registry.example.com/kubevirt-ccm@sha256:abc123"
		testOseCliImage = "registry.example.com/ose-cli@sha256:cli123"
	)

	It("should return nil when CCM is not enabled", func() {
		oacp := newOACP(false, false, "")
		result := GenerateCCMManifests(oacp, testInfraNS, testCCMImage, testOseCliImage)
		Expect(result).To(BeNil())
	})

	It("should return nil when CCM spec is nil", func() {
		oacp := newOACP(false, false, "")
		oacp.Spec.Config.CloudControllerManager = nil
		result := GenerateCCMManifests(oacp, testInfraNS, testCCMImage, testOseCliImage)
		Expect(result).To(BeNil())
	})

	It("should return nil when CCM image is empty", func() {
		oacp := newOACP(true, false, "")
		result := GenerateCCMManifests(oacp, testInfraNS, "", testOseCliImage)
		Expect(result).To(BeNil())
	})

	It("should generate all required manifests when CCM is enabled", func() {
		oacp := newOACP(true, false, "")
		result := GenerateCCMManifests(oacp, testInfraNS, testCCMImage, testOseCliImage)

		Expect(result).To(HaveLen(7))
		Expect(result[0].Filename).To(Equal("01-ccm-namespace.yaml"))
		Expect(result[1].Filename).To(Equal("02-ccm-cloud-config.yaml"))
		Expect(result[2].Filename).To(Equal("03-ccm-rbac.yaml"))
		Expect(result[3].Filename).To(Equal("04-ccm-deployment.yaml"))
		Expect(result[4].Filename).To(Equal("05-ccm-operator-script.yaml"))
		Expect(result[5].Filename).To(Equal("06-ccm-operator-rbac.yaml"))
		Expect(result[6].Filename).To(Equal("07-ccm-operator-deployment.yaml"))
	})

	It("should omit operator manifests when oseCliImage is empty", func() {
		oacp := newOACP(true, false, "")
		result := GenerateCCMManifests(oacp, testInfraNS, testCCMImage, "")

		Expect(result).To(HaveLen(4))
		Expect(result[3].Filename).To(Equal("04-ccm-deployment.yaml"))
	})

	It("should set the infra namespace in cloud-config", func() {
		oacp := newOACP(true, false, "")
		result := GenerateCCMManifests(oacp, testInfraNS, testCCMImage, testOseCliImage)

		Expect(result[1].Content).To(ContainSubstring("namespace: " + testInfraNS))
	})

	It("should use the resolved CCM image directly in the deployment", func() {
		oacp := newOACP(true, false, "")
		result := GenerateCCMManifests(oacp, testInfraNS, testCCMImage, testOseCliImage)

		Expect(result[3].Content).To(ContainSubstring("image: " + testCCMImage))
	})

	It("should reference the infra credentials secret in the deployment", func() {
		oacp := newOACP(true, false, "")
		result := GenerateCCMManifests(oacp, testInfraNS, testCCMImage, testOseCliImage)

		Expect(result[3].Content).To(ContainSubstring("secretName: " + ccmCredSecretName))
	})

	It("should use ose-cli image in the operator deployment", func() {
		oacp := newOACP(true, false, "")
		result := GenerateCCMManifests(oacp, testInfraNS, testCCMImage, testOseCliImage)

		Expect(result[6].Content).To(ContainSubstring("image: " + testOseCliImage))
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
