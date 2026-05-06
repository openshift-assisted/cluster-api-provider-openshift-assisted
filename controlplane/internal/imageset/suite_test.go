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

package imageset_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util/testutil"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
)

func TestImageSet(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ImageSet Suite")
}

var testScheme = runtime.NewScheme()

var _ = BeforeSuite(func() {
	// TEST_LOGLEVEL=-9 for full debug logging
	testutil.SetupTestLoggerWithDefault(GinkgoWriter, -3)

	utilruntime.Must(controlplanev1alpha3.AddToScheme(testScheme))
	utilruntime.Must(hivev1.AddToScheme(testScheme))
	utilruntime.Must(corev1.AddToScheme(testScheme))
})
