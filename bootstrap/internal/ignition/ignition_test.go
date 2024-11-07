package ignition

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	config_31 "github.com/coreos/ignition/v2/config/v3_1"
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Ignition utils")
}

/*
METADATA_LOCAL_HOSTNAME=bmh-vm-03
METADATA_METAL3_NAME=bmh-vm-03
METADATA_METAL3_NAMESPACE=test-capi
METADATA_NAME=bmh-vm-03
METADATA_UUID=0854b65a-42ec-4155-8e7c-ffc2b634c947
*/
var _ = Describe("Ignition utils", func() {
	When("generating ignition", func() {
		It("should be parsed with no errors", func() {
			capiSuccessFile := CreateIgnitionFile("/run/cluster-api/bootstrap-success.complete",
				"root", "data:text/plain;charset=utf-8;base64,c3VjY2Vzcw==", 420, true)
			i, err := GetIgnitionConfigOverrides(capiSuccessFile)
			Expect(err).NotTo(HaveOccurred())
			cfg, rep, err := config_31.Parse([]byte(i))
			Expect(rep.Entries).To(BeNil())
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg).NotTo(BeNil())
		})
	})
})
