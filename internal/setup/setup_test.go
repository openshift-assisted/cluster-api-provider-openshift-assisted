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

package setup

import (
	"context"
	"crypto/tls"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"

	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/internal/tlsconfig"
)

var _ = Describe("SetupSecurityProfileWatcher OnProfileChange callback behavior", func() {
	var (
		cancel    context.CancelFunc
		cancelled bool
		tlsResult tlsconfig.TLSConfigResult
	)

	BeforeEach(func() {
		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		cancelled = false

		// Wrap cancel to track if it was called
		originalCancel := cancel
		cancel = func() {
			cancelled = true
			originalCancel()
		}

		_ = ctx // Used only for context.WithCancel
	})

	AfterEach(func() {
		if !cancelled {
			cancel()
		}
	})

	Context("when tlsAdherence is NoOpinion", func() {
		BeforeEach(func() {
			tlsResult = tlsconfig.TLSConfigResult{
				TLSAdherencePolicy: configv1.TLSAdherencePolicyNoOpinion,
				TLSProfileSpec:     *configv1.TLSProfiles[configv1.TLSProfileIntermediateType],
			}
		})

		It("should NOT call cancel when profile changes", func() {
			// This simulates what the OnProfileChange callback should do
			if !libgocrypto.ShouldHonorClusterTLSProfile(tlsResult.TLSAdherencePolicy) {
				// Guard should prevent cancel
				Expect(cancelled).To(BeFalse())
				return
			}
			cancel()

			Expect(cancelled).To(BeFalse(), "cancel should not be called for NoOpinion policy")
		})
	})

	Context("when tlsAdherence is LegacyAdheringComponentsOnly", func() {
		BeforeEach(func() {
			tlsResult = tlsconfig.TLSConfigResult{
				TLSAdherencePolicy: configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
				TLSProfileSpec:     *configv1.TLSProfiles[configv1.TLSProfileIntermediateType],
			}
		})

		It("should NOT call cancel when profile changes", func() {
			// This simulates what the OnProfileChange callback should do
			if !libgocrypto.ShouldHonorClusterTLSProfile(tlsResult.TLSAdherencePolicy) {
				// Guard should prevent cancel
				Expect(cancelled).To(BeFalse())
				return
			}
			cancel()

			Expect(cancelled).To(BeFalse(), "cancel should not be called for LegacyAdheringComponentsOnly policy")
		})
	})

	Context("when tlsAdherence is StrictAllComponents", func() {
		BeforeEach(func() {
			tlsResult = tlsconfig.TLSConfigResult{
				TLSAdherencePolicy: configv1.TLSAdherencePolicyStrictAllComponents,
				TLSProfileSpec:     *configv1.TLSProfiles[configv1.TLSProfileIntermediateType],
			}
		})

		It("should call cancel when profile changes", func() {
			// This simulates what the OnProfileChange callback should do
			if !libgocrypto.ShouldHonorClusterTLSProfile(tlsResult.TLSAdherencePolicy) {
				// Guard should prevent cancel
				Expect(cancelled).To(BeFalse())
				return
			}
			cancel()

			Expect(cancelled).To(BeTrue(), "cancel should be called for StrictAllComponents policy")
		})

		It("should honor cluster TLS profile", func() {
			shouldHonor := libgocrypto.ShouldHonorClusterTLSProfile(tlsResult.TLSAdherencePolicy)
			Expect(shouldHonor).To(BeTrue())
		})
	})

	Context("ShouldHonorClusterTLSProfile function behavior", func() {
		It("should return false for NoOpinion", func() {
			result := libgocrypto.ShouldHonorClusterTLSProfile(configv1.TLSAdherencePolicyNoOpinion)
			Expect(result).To(BeFalse())
		})

		It("should return false for LegacyAdheringComponentsOnly", func() {
			result := libgocrypto.ShouldHonorClusterTLSProfile(configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly)
			Expect(result).To(BeFalse())
		})

		It("should return true for StrictAllComponents", func() {
			result := libgocrypto.ShouldHonorClusterTLSProfile(configv1.TLSAdherencePolicyStrictAllComponents)
			Expect(result).To(BeTrue())
		})
	})
})

var _ = Describe("ResolveTLSConfig", func() {
	It("should return Intermediate defaults when isOpenShift is false", func() {
		result, err := ResolveTLSConfig(context.Background(), nil, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.TLSConfig).NotTo(BeNil())
		Expect(result.TLSAdherencePolicy).To(Equal(configv1.TLSAdherencePolicy("")))

		cfg := &tls.Config{}
		result.TLSConfig(cfg)
		Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
	})
})

var _ = Describe("SetupSecurityProfileWatcher", func() {
	It("should be a no-op when isOpenShift is false", func() {
		called := false
		cancel := func() { called = true }

		err := SetupSecurityProfileWatcher(nil, tlsconfig.TLSConfigResult{}, false, cancel)
		Expect(err).NotTo(HaveOccurred())
		Expect(called).To(BeFalse())
	})
})

