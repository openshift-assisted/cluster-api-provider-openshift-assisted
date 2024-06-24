package imageregistry

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	providedCMName = "user-provided-config-map"
	testNamespace  = "test-namespace"
	certificate    = "    -----BEGIN CERTIFICATE-----\n    certificate contents\n    -----END CERTIFICATE------"
	sourceRegistry = "quay.io"
	mirrorRegistry = "example-user-registry.com"
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ImageRegistry Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	var err error
	err = configv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = corev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme
})

var _ = Describe("ImageRegistry Test", func() {
	Context("GenerateImageRegistryConfigmap", func() {
		var (
			mockCtrl *gomock.Controller
		)

		BeforeEach(func() {
			mockCtrl = gomock.NewController(GinkgoT())
		})

		AfterEach(func() {
			mockCtrl.Finish()
		})

		When("the user-provided image registry ConfigMap contains correct data", func() {
			It("successfully creates the image registry configmap for the spoke cluster", func() {
				By("Calling the GenerateImageRegistryConfigmap function")
				imageRegistryConfigMap, err := GenerateImageRegistryConfigmap(
					newUserProvidedRegistryCM(getSecureRegistryToml(), certificate),
					testNamespace,
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(imageRegistryConfigMap).NotTo(BeNil())

				expectedImageDigestMirrorSet := getImageDigestMirrorSetString(sourceRegistry, []string{mirrorRegistry})
				expectedClusterImage := getImageConfigString(registryCertConfigMapName, []string{})
				expectedCertificateCM := getCMString(
					registryCertConfigMapName,
					registryCertConfigMapNamespace,
					map[string]string{registryCertKey: certificate},
				)

				By("Checking the ConfigMap contains the correct data")
				Expect(imageRegistryConfigMap.Data).NotTo(BeNil())
				Expect(imageRegistryConfigMap.Data).NotTo(HaveKey(imageTagMirrorSetKey))
				Expect(imageRegistryConfigMap.Data).To(HaveKey(imageDigestMirrorSetKey))
				Expect(imageRegistryConfigMap.Data[imageDigestMirrorSetKey]).To(Equal(expectedImageDigestMirrorSet))
				Expect(imageRegistryConfigMap.Data).To(HaveKey(registryCertConfigMapKey))
				Expect(imageRegistryConfigMap.Data[registryCertConfigMapKey]).To(Equal(expectedCertificateCM))
				Expect(imageRegistryConfigMap.Data).To(HaveKey(imageConfigKey))
				Expect(imageRegistryConfigMap.Data[imageConfigKey]).To(Equal(expectedClusterImage))
			})
		})

		When("the user-provided image registry ConfigMap contains an insecure registry", func() {
			It("successfully creates the image registry configmap with the insecure registry", func() {
				By("Calling the GenerateImageRegistryConfigmap function")
				imageRegistryConfigMap, err := GenerateImageRegistryConfigmap(
					newUserProvidedRegistryCM(getInsecureRegistryToml(), certificate),
					testNamespace,
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(imageRegistryConfigMap).NotTo(BeNil())

				insecureRegistries := []string{mirrorRegistry}
				expectedImageDigestMirrorSet := getImageDigestMirrorSetString(sourceRegistry, []string{mirrorRegistry})
				expectedClusterImage := getImageConfigString(registryCertConfigMapName, insecureRegistries)
				expectedCertificateCM := getCMString(
					registryCertConfigMapName,
					registryCertConfigMapNamespace,
					map[string]string{registryCertKey: certificate},
				)

				By("Checking the ConfigMap contains the correct data")
				Expect(imageRegistryConfigMap.Data).NotTo(BeNil())
				Expect(imageRegistryConfigMap.Data).NotTo(HaveKey(imageTagMirrorSetKey))
				Expect(imageRegistryConfigMap.Data).To(HaveKey(imageDigestMirrorSetKey))
				Expect(imageRegistryConfigMap.Data[imageDigestMirrorSetKey]).To(Equal(expectedImageDigestMirrorSet))
				Expect(imageRegistryConfigMap.Data).To(HaveKey(registryCertConfigMapKey))
				Expect(imageRegistryConfigMap.Data[registryCertConfigMapKey]).To(Equal(expectedCertificateCM))
				Expect(imageRegistryConfigMap.Data).To(HaveKey(imageConfigKey))
				Expect(imageRegistryConfigMap.Data[imageConfigKey]).To(Equal(expectedClusterImage))
			})
		})

		When("the user-provided image registry ConfigMap pulls by tag", func() {
			It("successfully creates the image registry configmap that pulls by tag", func() {
				userRegistryCM := newUserProvidedRegistryCM(getSecureRegistryTagOnlyToml(), certificate)

				By("Calling the GenerateImageRegistryConfigmap function")
				imageRegistryConfigMap, err := GenerateImageRegistryConfigmap(
					userRegistryCM,
					testNamespace,
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(imageRegistryConfigMap).NotTo(BeNil())

				expectedImageTagMirrorSet := getImagTagMirrorSetString(sourceRegistry, []string{mirrorRegistry})
				expectedClusterImage := getImageConfigString(registryCertConfigMapName, []string{})
				expectedCertificateCM := getCMString(
					registryCertConfigMapName,
					registryCertConfigMapNamespace,
					map[string]string{registryCertKey: certificate},
				)

				By("Checking the ConfigMap contains the correct data")
				Expect(imageRegistryConfigMap.Data).NotTo(BeNil())
				Expect(imageRegistryConfigMap.Data).NotTo(HaveKey(imageDigestMirrorSetKey))
				Expect(imageRegistryConfigMap.Data).To(HaveKey(imageTagMirrorSetKey))
				Expect(imageRegistryConfigMap.Data[imageTagMirrorSetKey]).To(Equal(expectedImageTagMirrorSet))
				Expect(imageRegistryConfigMap.Data).To(HaveKey(registryCertConfigMapKey))
				Expect(imageRegistryConfigMap.Data[registryCertConfigMapKey]).To(Equal(expectedCertificateCM))
				Expect(imageRegistryConfigMap.Data).To(HaveKey(imageConfigKey))
				Expect(imageRegistryConfigMap.Data[imageConfigKey]).To(Equal(expectedClusterImage))
			})
		})

		When("the user-provided image registry ConfigMap doesn't have additional certificates", func() {
			It("successfully creates the image registry configmap for the spoke cluster", func() {
				userRegistryCM := newUserProvidedRegistryCM(getSecureRegistryToml(), "")

				By("Calling the GenerateImageRegistryConfigmap function")
				imageRegistryConfigMap, err := GenerateImageRegistryConfigmap(
					userRegistryCM,
					testNamespace,
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(imageRegistryConfigMap).NotTo(BeNil())

				expectedImageDigestMirrorSet := getImageDigestMirrorSetString(sourceRegistry, []string{mirrorRegistry})

				By("Checking the ConfigMap contains the correct data")
				Expect(imageRegistryConfigMap.Data).NotTo(BeNil())
				Expect(imageRegistryConfigMap.Data[imageDigestMirrorSetKey]).NotTo(BeNil())
				Expect(imageRegistryConfigMap.Data[imageDigestMirrorSetKey]).To(Equal(expectedImageDigestMirrorSet))
				Expect(imageRegistryConfigMap.Data[registryCertConfigMapKey]).To(BeEmpty())
				Expect(imageRegistryConfigMap.Data[imageConfigKey]).To(BeEmpty())
			})
		})

		When("the user-provided image registry ConfigMap is missing the source in the registries.conf", func() {
			It("fails to create the image registry configmap", func() {
				By("Calling the GenerateImageRegistryConfigmap function")
				imageRegistryConfigMap, err := GenerateImageRegistryConfigmap(
					newUserProvidedRegistryCM(getRegistryMissingSourceToml(), ""),
					testNamespace,
				)
				Expect(err).To(HaveOccurred())
				Expect(imageRegistryConfigMap).To(BeNil())
			})
		})

		When("the user-provided image registry ConfigMap is missing the mirror in the registries.conf", func() {
			It("fails to create the image registry configmap", func() {
				By("Calling the GenerateImageRegistryConfigmap function")
				imageRegistryConfigMap, err := GenerateImageRegistryConfigmap(
					newUserProvidedRegistryCM(getRegistryMissingMirrorToml(), certificate),
					testNamespace,
				)
				Expect(err).To(HaveOccurred())
				Expect(imageRegistryConfigMap).To(BeNil())
			})
		})

		When("the user-provided image registry ConfigMap registries.conf toml is not well-formatted", func() {
			It("fails to create the image registry configmap", func() {
				userRegistryCM := newUserProvidedRegistryCM(fmt.Sprintf("location=%s", sourceRegistry), certificate)

				By("Calling the GenerateImageRegistryConfigmap function")
				imageRegistryConfigMap, err := GenerateImageRegistryConfigmap(
					userRegistryCM,
					testNamespace,
				)
				Expect(err).To(HaveOccurred())
				Expect(imageRegistryConfigMap).To(BeNil())
			})
		})

		When("the user-provided image registry ConfigMap is missing the registries.conf key", func() {
			It("fails to create the image registry configmap", func() {
				userRegistryCM := newUserProvidedRegistryCM("", certificate)

				By("Calling the GenerateImageRegistryConfigmap function")
				imageRegistryConfigMap, err := GenerateImageRegistryConfigmap(
					userRegistryCM,
					testNamespace,
				)
				Expect(err).To(HaveOccurred())
				Expect(imageRegistryConfigMap).To(BeNil())
			})
		})

		When("the user-provided image registry ConfigMap has an invalid toml configuration", func() {
			It("returns parsing error and fails to create configmap", func() {
				userRegistryCM := newUserProvidedRegistryCM(getInvalidRegistryToml(), certificate)
				By("Calling the GenerateImageRegistryConfigmap function")
				imageRegistryConfigMap, err := GenerateImageRegistryConfigmap(
					userRegistryCM,
					testNamespace,
				)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal(
					"failed to load value of registries.conf into toml tree; incorrectly formatted toml: (6, 13): cannot have two dots in one float"))
				Expect(imageRegistryConfigMap).To(BeNil())
			})
		})

	})
})

func getSecureRegistryToml() string {
	return fmt.Sprintf(`
[[registry]]
location = "%s"

[[registry.mirror]]
location = "%s"
`,
		sourceRegistry,
		mirrorRegistry,
	)
}

func getSecureRegistryTagOnlyToml() string {
	return fmt.Sprintf(`
[[registry]]
location = "%s"

[[registry.mirror]]
location = "%s"
pull-from-mirror = "tag-only"
`,
		sourceRegistry,
		mirrorRegistry,
	)
}

func getInsecureRegistryToml() string {
	return fmt.Sprintf(`
[[registry]]
location = "%s"

[[registry.mirror]]
location = "%s"
insecure = true
`,
		sourceRegistry,
		mirrorRegistry,
	)
}

func getRegistryMissingMirrorToml() string {
	return fmt.Sprintf(`
[[registry]]
location = "%s"
`,
		sourceRegistry,
	)
}

func getRegistryMissingSourceToml() string {
	return fmt.Sprintf(`
[[registry.mirror]]
location = "%s"
`,
		mirrorRegistry,
	)
}

func getInvalidRegistryToml() string {
	// location has no quotes, will be parsed as double
	return `
[[registry]]
	location = "%s"

	[[registry.mirror]]
	location = 192.168.1.1:5000
	insecure = true
`
}

func newUserProvidedRegistryCM(registry, certificate string) *corev1.ConfigMap {
	data := map[string]string{}
	if registry != "" {
		data[registryConfKey] = registry
	}
	if certificate != "" {
		data[registryCertKey] = certificate
	}
	return generateConfigMap(testNamespace, data)
}

func generateConfigMap(namespace string, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      providedCMName,
			Namespace: namespace,
		},
		Data: data,
	}
}

func getImageDigestMirrorSetString(source string, mirrors []string) string {
	imageDigestMirror := configv1.ImageDigestMirrors{}
	if source != "" {
		imageDigestMirror.Source = source
	}
	for _, mirror := range mirrors {
		imageDigestMirror.Mirrors = append(imageDigestMirror.Mirrors, configv1.ImageMirror(mirror))
	}

	expectedImageDigestMirrorSet, _ := json.Marshal(configv1.ImageDigestMirrorSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ImageDigestMirrorSet",
			APIVersion: configv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{Name: imageRegistryName},
		Spec: configv1.ImageDigestMirrorSetSpec{
			ImageDigestMirrors: []configv1.ImageDigestMirrors{imageDigestMirror},
		},
	})
	return string(expectedImageDigestMirrorSet)
}

func getImagTagMirrorSetString(source string, mirrors []string) string {
	imageTagMirror := configv1.ImageTagMirrors{}
	if source != "" {
		imageTagMirror.Source = source
	}
	for _, mirror := range mirrors {
		imageTagMirror.Mirrors = append(imageTagMirror.Mirrors, configv1.ImageMirror(mirror))
	}

	expectedImageTagMirrorSet, _ := json.Marshal(configv1.ImageTagMirrorSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ImageTagMirrorSet",
			APIVersion: configv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{Name: imageRegistryName},
		Spec: configv1.ImageTagMirrorSetSpec{
			ImageTagMirrors: []configv1.ImageTagMirrors{imageTagMirror},
		},
	})
	return string(expectedImageTagMirrorSet)
}

func getImageConfigString(certCMName string, insecureRegistries []string) string {
	imageConfig := configv1.Image{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Image",
			APIVersion: configv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec: configv1.ImageSpec{
			AdditionalTrustedCA: configv1.ConfigMapNameReference{Name: certCMName},
		},
	}
	if len(insecureRegistries) > 0 {
		imageConfig.Spec.RegistrySources.InsecureRegistries = insecureRegistries
	}
	imageConfigJSON, _ := json.Marshal(imageConfig)
	return string(imageConfigJSON)
}

func getCMString(name, namespace string, data map[string]string) string {
	configMap, _ := json.Marshal(corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: data,
	})
	return string(configMap)
}
