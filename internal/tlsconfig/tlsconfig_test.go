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

package tlsconfig

import (
	"context"
	"crypto/tls"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func newFakeControllerClient(objs ...ctrlclient.Object) ctrlclient.Client {
	scheme := runtime.NewScheme()
	_ = configv1.Install(scheme)

	return fakectrlclient.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		Build()
}

var _ = Describe("DefaultTLSConfig", func() {
	It("should return a TLS config with a minimum TLS version set", func() {
		result := DefaultTLSConfig()
		Expect(result.TLSConfig).NotTo(BeNil())

		cfg := &tls.Config{}
		result.TLSConfig(cfg)
		Expect(cfg.MinVersion).To(BeNumerically(">", 0))
	})
})

var _ = Describe("resolveOpenShiftTLSConfig", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("should use defaults when adherence policy is NoOpinion", func() {
		apiServer := &configv1.APIServer{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		}
		k8sClient := newFakeControllerClient(apiServer)

		result, err := resolveOpenShiftTLSConfig(ctx, k8sClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.TLSConfig).NotTo(BeNil())

		cfg := &tls.Config{}
		result.TLSConfig(cfg)
		Expect(cfg.MinVersion).To(BeNumerically(">", 0))
	})

	It("should honor the cluster TLS profile when adherence is StrictAllComponents", func() {
		modernProfile := configv1.TLSProfiles[configv1.TLSProfileModernType]
		apiServer := &configv1.APIServer{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1.APIServerSpec{
				TLSSecurityProfile: &configv1.TLSSecurityProfile{
					Type: configv1.TLSProfileModernType,
				},
				TLSAdherence: configv1.TLSAdherencePolicyStrictAllComponents,
			},
		}
		k8sClient := newFakeControllerClient(apiServer)

		result, err := resolveOpenShiftTLSConfig(ctx, k8sClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.TLSAdherencePolicy).To(Equal(configv1.TLSAdherencePolicyStrictAllComponents))
		Expect(result.TLSProfileSpec.MinTLSVersion).To(Equal(modernProfile.MinTLSVersion))
		Expect(result.TLSProfileSpec.Ciphers).To(Equal(modernProfile.Ciphers))
		Expect(result.TLSConfig).NotTo(BeNil())

		cfg := &tls.Config{}
		result.TLSConfig(cfg)
		Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS13)))
	})

	It("should return an error when the APIServer resource cannot be fetched", func() {
		k8sClient := newFakeControllerClient()

		_, err := resolveOpenShiftTLSConfig(ctx, k8sClient)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("fetching TLS adherence policy from APIServer"))
	})

	It("should return an error when no ciphers from the cluster profile are supported", func() {
		apiServer := &configv1.APIServer{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1.APIServerSpec{
				TLSSecurityProfile: &configv1.TLSSecurityProfile{
					Type: configv1.TLSProfileCustomType,
					Custom: &configv1.CustomTLSProfile{
						TLSProfileSpec: configv1.TLSProfileSpec{
							Ciphers:       []string{"BOGUS_CIPHER_1", "BOGUS_CIPHER_2"},
							MinTLSVersion: configv1.VersionTLS12,
						},
					},
				},
				TLSAdherence: configv1.TLSAdherencePolicyStrictAllComponents,
			},
		}
		k8sClient := newFakeControllerClient(apiServer)

		_, err := resolveOpenShiftTLSConfig(ctx, k8sClient)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("none of the ciphers from the cluster TLS profile are supported"))
	})
})

var _ = Describe("Transient API errors during TLS resolution", func() {
	It("should propagate a transient API error without retry or fallback", func() {
		scheme := runtime.NewScheme()
		_ = configv1.Install(scheme)

		getCalls := 0
		k8sClient := fakectrlclient.NewClientBuilder().
			WithScheme(scheme).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(_ context.Context, _ ctrlclient.WithWatch, _ ctrlclient.ObjectKey, _ ctrlclient.Object, _ ...ctrlclient.GetOption) error {
					getCalls++

					return apierrors.NewServiceUnavailable("transient API error")
				},
			}).
			Build()

		_, err := resolveOpenShiftTLSConfig(context.Background(), k8sClient)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("fetching TLS adherence policy from APIServer"))
		Expect(err.Error()).To(ContainSubstring("transient API error"))
		Expect(getCalls).To(Equal(1), "should call Get exactly once with no retry")
	})
})

var _ = Describe("NoOpinion adherence with configured TLS profile", func() {
	It("should ignore a configured Modern profile when adherence is NoOpinion", func() {
		apiServer := &configv1.APIServer{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1.APIServerSpec{
				TLSSecurityProfile: &configv1.TLSSecurityProfile{
					Type:   configv1.TLSProfileModernType,
					Modern: &configv1.ModernTLSProfile{},
				},
			},
		}
		k8sClient := newFakeControllerClient(apiServer)

		result, err := resolveOpenShiftTLSConfig(context.Background(), k8sClient)
		Expect(err).NotTo(HaveOccurred())

		cfg := &tls.Config{}
		result.TLSConfig(cfg)
		Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)),
			"NoOpinion should use Intermediate defaults (TLS 1.2), not the configured Modern profile (TLS 1.3)")
	})

	// ACM-34017: SecurityProfileWatcher restart loop.
	// When adherence is NoOpinion, resolveOpenShiftTLSConfig returns
	// Intermediate as TLSProfileSpec (from DefaultTLSConfig) instead of the
	// actual APIServer profile. The SecurityProfileWatcher is initialized
	// with this Intermediate spec, but on reconcile it reads the real
	// APIServer profile (Modern). The mismatch triggers OnProfileChange →
	// cancel → restart → same mismatch → infinite loop.
	// ACM-34017: Enable this test once the fix is implemented.
	PIt("should store the actual APIServer profile in TLSProfileSpec when adherence is NoOpinion", func() {
		modernProfile := configv1.TLSProfiles[configv1.TLSProfileModernType]
		apiServer := &configv1.APIServer{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1.APIServerSpec{
				TLSSecurityProfile: &configv1.TLSSecurityProfile{
					Type:   configv1.TLSProfileModernType,
					Modern: &configv1.ModernTLSProfile{},
				},
			},
		}
		k8sClient := newFakeControllerClient(apiServer)

		result, err := resolveOpenShiftTLSConfig(context.Background(), k8sClient)
		Expect(err).NotTo(HaveOccurred())

		cfg := &tls.Config{}
		result.TLSConfig(cfg)
		Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)),
			"TLS config applied to the server should use Intermediate defaults")

		Expect(result.TLSProfileSpec.MinTLSVersion).To(Equal(modernProfile.MinTLSVersion),
			"TLSProfileSpec should reflect the actual APIServer profile, not defaults — "+
				"otherwise SecurityProfileWatcher detects a spurious mismatch on startup (ACM-34017)")
	})
})
