package kubevirt_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/kubevirt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
)

func buildMachineTemplate(interfaces []interface{}, annotations map[string]interface{}) *unstructured.Unstructured {
	vmiMetadata := map[string]interface{}{}
	if annotations != nil {
		vmiMetadata["annotations"] = annotations
	}
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						"virtualMachineTemplate": map[string]interface{}{
							"spec": map[string]interface{}{
								"template": map[string]interface{}{
									"spec": map[string]interface{}{
										"domain": map[string]interface{}{
											"devices": map[string]interface{}{
												"interfaces": interfaces,
											},
										},
										"networks": []interface{}{
											map[string]interface{}{
												"name": "default",
												"pod":  map[string]interface{}{},
											},
										},
									},
									"metadata": vmiMetadata,
								},
							},
						},
					},
				},
			},
		},
	}
}

var _ = Describe("ValidateNetworkingRequirements", func() {
	Context("When the template has all required settings", func() {
		It("should pass validation", func() {
			tmpl := buildMachineTemplate(
				[]interface{}{map[string]interface{}{"name": "default", "bridge": map[string]interface{}{}}},
				map[string]interface{}{"kubevirt.io/allow-pod-bridge-network-live-migration": ""},
			)
			err := kubevirt.ValidateNetworkingRequirements(tmpl)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("When using masquerade instead of bridge", func() {
		It("should return an error mentioning bridge requirement", func() {
			tmpl := buildMachineTemplate(
				[]interface{}{map[string]interface{}{"name": "default", "masquerade": map[string]interface{}{}}},
				map[string]interface{}{"kubevirt.io/allow-pod-bridge-network-live-migration": ""},
			)
			err := kubevirt.ValidateNetworkingRequirements(tmpl)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("bridge"))
			Expect(err.Error()).To(ContainSubstring("masquerade"))
		})
	})

	Context("When the live migration annotation is missing", func() {
		It("should return an error mentioning the annotation", func() {
			tmpl := buildMachineTemplate(
				[]interface{}{map[string]interface{}{"name": "default", "bridge": map[string]interface{}{}}},
				nil,
			)
			err := kubevirt.ValidateNetworkingRequirements(tmpl)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("allow-pod-bridge-network-live-migration"))
		})
	})

	Context("When no interfaces are configured", func() {
		It("should return an error", func() {
			tmpl := buildMachineTemplate(nil,
				map[string]interface{}{"kubevirt.io/allow-pod-bridge-network-live-migration": ""},
			)
			err := kubevirt.ValidateNetworkingRequirements(tmpl)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no network interfaces"))
		})
	})

	Context("When VMI spec is missing", func() {
		It("should return an error", func() {
			empty := &unstructured.Unstructured{Object: map[string]interface{}{"spec": map[string]interface{}{}}}
			err := kubevirt.ValidateNetworkingRequirements(empty)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to find VMI spec"))
		})
	})
})

var _ = Describe("GenerateNetworkManifests", func() {
	Context("When network config has MTU and GenevePort", func() {
		It("should generate a manifest with both values", func() {
			oacp := &controlplanev1alpha3.OpenshiftAssistedControlPlane{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Spec: controlplanev1alpha3.OpenshiftAssistedControlPlaneSpec{
					Config: controlplanev1alpha3.OpenshiftAssistedControlPlaneConfigSpec{
						Network: &controlplanev1alpha3.WorkloadClusterNetworkSpec{
							OVNKubernetes: &controlplanev1alpha3.OVNKubernetesConfig{
								MTU:        ptr.To[uint32](1300),
								GenevePort: ptr.To[uint32](9880),
							},
						},
					},
				},
			}
			manifests := kubevirt.GenerateNetworkManifests(oacp)
			Expect(manifests).To(HaveLen(1))
			Expect(manifests[0].Content).To(ContainSubstring("mtu: 1300"))
			Expect(manifests[0].Content).To(ContainSubstring("genevePort: 9880"))
		})
	})

	Context("When only MTU is set", func() {
		It("should generate a manifest with MTU only", func() {
			oacp := &controlplanev1alpha3.OpenshiftAssistedControlPlane{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Spec: controlplanev1alpha3.OpenshiftAssistedControlPlaneSpec{
					Config: controlplanev1alpha3.OpenshiftAssistedControlPlaneConfigSpec{
						Network: &controlplanev1alpha3.WorkloadClusterNetworkSpec{
							OVNKubernetes: &controlplanev1alpha3.OVNKubernetesConfig{
								MTU: ptr.To[uint32](1400),
							},
						},
					},
				},
			}
			manifests := kubevirt.GenerateNetworkManifests(oacp)
			Expect(manifests).To(HaveLen(1))
			Expect(manifests[0].Content).To(ContainSubstring("mtu: 1400"))
			Expect(manifests[0].Content).NotTo(ContainSubstring("genevePort"))
		})
	})

	Context("When network config is nil", func() {
		It("should return nil", func() {
			oacp := &controlplanev1alpha3.OpenshiftAssistedControlPlane{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			}
			Expect(kubevirt.GenerateNetworkManifests(oacp)).To(BeNil())
		})
	})
})
