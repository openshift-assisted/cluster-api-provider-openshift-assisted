package kubevirt_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestKubevirt(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "KubeVirt Suite")
}
