package version_test

import (
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util/testutil"
)

var testScheme = runtime.NewScheme()

var _ = BeforeSuite(func() {
	// TEST_LOGLEVEL=-9 for full debug logging
	testutil.SetupTestLoggerWithDefault(GinkgoWriter, -3)

	utilruntime.Must(configv1.AddToScheme(testScheme))
})

func TestVersion(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Version Suite")
}
