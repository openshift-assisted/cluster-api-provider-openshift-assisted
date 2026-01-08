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

package v1alpha2

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util/testutil"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clusterv1beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

func TestV1Alpha2(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "ControlPlane API v1alpha2 Suite")
}

var testScheme = runtime.NewScheme()

var _ = BeforeSuite(func() {
	// TEST_LOGLEVEL=-9 for full debug logging
	testutil.SetupTestLoggerWithDefault(GinkgoWriter, -3)

	By("bootstrapping test environment")

	utilruntime.Must(AddToScheme(testScheme))
	utilruntime.Must(controlplanev1alpha3.AddToScheme(testScheme))
	utilruntime.Must(clusterv1beta1.AddToScheme(testScheme))
	utilruntime.Must(clusterv1.AddToScheme(testScheme))

	//+kubebuilder:scaffold:scheme
})
