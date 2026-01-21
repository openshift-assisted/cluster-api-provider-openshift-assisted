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

package controller_test

import (
	"testing"

	metal3v1beta1 "github.com/metal3-io/cluster-api-provider-metal3/api/v1beta1"
	bootstrapv1alpha2 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/bootstrap/api/v1alpha2"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util/testutil"
	configv1 "github.com/openshift/api/config/v1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	corev1 "k8s.io/api/core/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var testScheme = runtime.NewScheme()

var _ = BeforeSuite(func() {
	// TEST_LOGLEVEL=-9 for full debug logging
	testutil.SetupTestLoggerWithDefault(GinkgoWriter, -9)

	By("bootstrapping test environment")

	utilruntime.Must(controlplanev1alpha3.AddToScheme(testScheme))
	utilruntime.Must(corev1.AddToScheme(testScheme))
	utilruntime.Must(configv1.AddToScheme(testScheme))
	utilruntime.Must(clusterv1.AddToScheme(testScheme))
	utilruntime.Must(hivev1.AddToScheme(testScheme))
	utilruntime.Must(hiveext.AddToScheme(testScheme))
	utilruntime.Must(metal3v1beta1.AddToScheme(testScheme))
	utilruntime.Must(bootstrapv1alpha2.AddToScheme(testScheme))

})
