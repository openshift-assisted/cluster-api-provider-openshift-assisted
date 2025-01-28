package containers_test

import (
	"encoding/base64"
	"fmt"

	"github.com/google/go-containerregistry/pkg/name"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/openshift-assisted/cluster-api-agent/pkg/containers"
)

var _ = Describe("PullSecretKeyChain", func() {
	var validPullSecret string
	var invalidBase64Secret string
	var malformedAuthSecret string

	BeforeEach(func() {
		// Create a valid pull secret JSON
		authStr := base64.StdEncoding.EncodeToString([]byte("testuser:testpassword"))
		validPullSecret = fmt.Sprintf(`{"auths": {"registry.example.com": {"auth": "%s"}}}`, authStr)

		// Create an invalid base64-encoded pull secret
		invalidBase64Secret = `{"auths": {"registry.example.com": {"auth": "invalidbase64"}}}`

		// Create a malformed auth entry (missing colon separator)
		malformedAuthSecret = `{"auths": {"registry.example.com": {"auth": "` +
			base64.StdEncoding.EncodeToString([]byte("malformeddata")) + "}}}"
	})

	Describe("PullSecretKeyChainFromString", func() {
		It("should parse a valid pull secret", func() {
			keychain, err := containers.PullSecretKeyChainFromString(validPullSecret)
			Expect(err).NotTo(HaveOccurred())
			Expect(keychain).NotTo(BeNil())
			registry, _ := name.NewRegistry("registry.example.com")
			authenticator, err := keychain.Resolve(registry)
			Expect(err).NotTo(HaveOccurred())
			Expect(authenticator).NotTo(Equal(authn.Anonymous))
		})

		It("should return an error for invalid base64 encoding", func() {
			_, err := containers.PullSecretKeyChainFromString(invalidBase64Secret)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(
				MatchRegexp("failed to decode auth for registry.example.com: illegal base64 data at input byte.*"),
			))
		})

		It("should return an error for malformed auth format", func() {
			_, err := containers.PullSecretKeyChainFromString(malformedAuthSecret)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError("failed to parse pullsecret: unexpected end of JSON input"))
		})

		It("should return anonymous authenticator for unknown registry", func() {
			keychain, err := containers.PullSecretKeyChainFromString(validPullSecret)
			Expect(err).NotTo(HaveOccurred())
			Expect(keychain).NotTo(BeNil())

			registry, _ := name.NewRegistry("unknown.example.com")
			authenticator, err := keychain.Resolve(registry)
			Expect(err).NotTo(HaveOccurred())
			Expect(authenticator).To(Equal(authn.Anonymous))
		})
	})
})
