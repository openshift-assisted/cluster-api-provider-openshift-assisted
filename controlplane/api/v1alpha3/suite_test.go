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

package v1alpha3

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util/testutil"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	testEnv    *envtest.Environment
	cfg        *rest.Config
	k8sClient  client.Client
	testScheme = runtime.NewScheme()
)

func TestV1Alpha3(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "ControlPlane API v1alpha3 Suite")
}

var _ = BeforeSuite(func() {
	// TEST_LOGLEVEL=-9 for full debug logging
	testutil.SetupTestLoggerWithDefault(GinkgoWriter, -3)

	By("bootstrapping test environment")
	// The generated CRD carries a machineNetwork CEL rule (isCIDR) with no maxItems.
	// A vanilla envtest apiserver rejects it for exceeding the CEL cost budget (OpenShift
	// relaxes this limit, so it installs fine on a real cluster). That rule is unrelated to
	// this suite, so load the CRD and drop it before installing.
	crd := loadOACPCRD()

	testEnv = &envtest.Environment{
		CRDs:                  []*apiextensionsv1.CustomResourceDefinition{crd},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	utilruntime.Must(AddToScheme(testScheme))

	k8sClient, err = client.New(cfg, client.Options{Scheme: testScheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	//+kubebuilder:scaffold:scheme
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	if testEnv != nil {
		Expect(testEnv.Stop()).To(Succeed())
	}
})

// loadOACPCRD reads the generated OpenshiftAssistedControlPlane CRD and strips the
// machineNetwork CEL validation, which a vanilla apiserver rejects for exceeding the
// CEL cost budget. The minProperties constraints under test are left untouched.
func loadOACPCRD() *apiextensionsv1.CustomResourceDefinition {
	path := filepath.Join("..", "..", "config", "crd", "bases",
		"controlplane.cluster.x-k8s.io_openshiftassistedcontrolplanes.yaml")
	data, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())

	crd := &apiextensionsv1.CustomResourceDefinition{}
	Expect(yaml.Unmarshal(data, crd)).To(Succeed())

	for i := range crd.Spec.Versions {
		schema := crd.Spec.Versions[i].Schema.OpenAPIV3Schema
		spec := schema.Properties["spec"]
		config := spec.Properties["config"]
		machineNetwork := config.Properties["machineNetwork"]
		machineNetwork.XValidations = nil
		config.Properties["machineNetwork"] = machineNetwork
		spec.Properties["config"] = config
		schema.Properties["spec"] = spec
	}

	return crd
}
