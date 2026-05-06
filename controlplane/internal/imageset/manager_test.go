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
	"context"
	"fmt"

	"github.com/golang/mock/gomock"
	"github.com/google/go-containerregistry/pkg/authn"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/imageset"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/pkg/containers"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testNamespace        = "test-namespace"
	testClusterName      = "test-cluster"
	testImageSetName     = "test-imageset"
	testReleaseImage     = "quay.io/openshift-release-dev/ocp-release:4.16.0-x86_64"
	testReleaseImageRepo = "quay.io/openshift-release-dev/ocp-release"
	testDigest           = "sha256:abc123def456"
	testDigestImage      = testReleaseImageRepo + "@" + testDigest
	testPullSecretName   = "test-pull-secret"
	testPullSecret       = `{"auths":{"quay.io":{"auth":"dGVzdDp0ZXN0"}}}`
)

var _ = Describe("ImageSet Manager", func() {
	var (
		ctx             context.Context
		mockCtrl        *gomock.Controller
		mockRemoteImage *containers.MockRemoteImage
		fakeClient      client.Client
		manager         *imageset.Manager
		acp             *controlplanev1alpha3.OpenshiftAssistedControlPlane
	)

	BeforeEach(func() {
		ctx = context.Background()
		mockCtrl = gomock.NewController(GinkgoT())
		mockRemoteImage = containers.NewMockRemoteImage(mockCtrl)
		fakeClient = fake.NewClientBuilder().WithScheme(testScheme).Build()
		manager = imageset.NewManager(fakeClient, mockRemoteImage)

		acp = &controlplanev1alpha3.OpenshiftAssistedControlPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testClusterName,
				Namespace: testNamespace,
			},
			Spec: controlplanev1alpha3.OpenshiftAssistedControlPlaneSpec{
				Config: controlplanev1alpha3.OpenshiftAssistedControlPlaneConfig{
					PullSecretRef: &corev1.LocalObjectReference{
						Name: testPullSecretName,
					},
				},
			},
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Describe("EnsureDigestBasedClusterImageSet", func() {
		Context("When ClusterImageSet does not exist", func() {
			It("resolves digest and creates ClusterImageSet", func() {
				// Create pull secret
				pullSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testPullSecretName,
						Namespace: testNamespace,
					},
					Data: map[string][]byte{
						".dockerconfigjson": []byte(testPullSecret),
					},
				}
				Expect(fakeClient.Create(ctx, pullSecret)).To(Succeed())

				// Mock digest resolution
				mockRemoteImage.EXPECT().
					GetDigest(testReleaseImage, gomock.Any()).
					Return(testDigest, nil)

				// Execute
				err := manager.EnsureDigestBasedClusterImageSet(ctx, testImageSetName, testReleaseImage, acp)
				Expect(err).NotTo(HaveOccurred())

				// Verify ClusterImageSet created with digest
				imageSet := &hivev1.ClusterImageSet{}
				err = fakeClient.Get(ctx, client.ObjectKey{Name: testImageSetName}, imageSet)
				Expect(err).NotTo(HaveOccurred())
				Expect(imageSet.Spec.ReleaseImage).To(Equal(testDigestImage))
				Expect(imageSet.Annotations).To(HaveKeyWithValue("cluster.x-k8s.io/release-image-source", testReleaseImage))
			})

			It("works without pull secret (anonymous auth)", func() {
				// No pull secret configured
				acp.Spec.Config.PullSecretRef = nil

				// Mock digest resolution with anonymous auth (nil keychain)
				mockRemoteImage.EXPECT().
					GetDigest(testReleaseImage, nil).
					Return(testDigest, nil)

				// Execute
				err := manager.EnsureDigestBasedClusterImageSet(ctx, testImageSetName, testReleaseImage, acp)
				Expect(err).NotTo(HaveOccurred())

				// Verify ClusterImageSet created
				imageSet := &hivev1.ClusterImageSet{}
				err = fakeClient.Get(ctx, client.ObjectKey{Name: testImageSetName}, imageSet)
				Expect(err).NotTo(HaveOccurred())
				Expect(imageSet.Spec.ReleaseImage).To(Equal(testDigestImage))
			})

			It("returns error when digest resolution fails", func() {
				// Create pull secret
				pullSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testPullSecretName,
						Namespace: testNamespace,
					},
					Data: map[string][]byte{
						".dockerconfigjson": []byte(testPullSecret),
					},
				}
				Expect(fakeClient.Create(ctx, pullSecret)).To(Succeed())

				// Mock digest resolution failure
				mockRemoteImage.EXPECT().
					GetDigest(testReleaseImage, gomock.Any()).
					Return("", fmt.Errorf("registry unreachable"))

				// Execute
				err := manager.EnsureDigestBasedClusterImageSet(ctx, testImageSetName, testReleaseImage, acp)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to resolve release image digest"))
				Expect(err.Error()).To(ContainSubstring("registry unreachable"))
			})

			It("returns error when pull secret is missing", func() {
				// Pull secret not created

				// Execute
				err := manager.EnsureDigestBasedClusterImageSet(ctx, testImageSetName, testReleaseImage, acp)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to retrieve pull secret"))
			})
		})

		Context("When ClusterImageSet exists with same source", func() {
			It("reuses existing digest without re-resolving", func() {
				// Pre-create ClusterImageSet with digest
				existingImageSet := &hivev1.ClusterImageSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: testImageSetName,
						Annotations: map[string]string{
							"cluster.x-k8s.io/release-image-source": testReleaseImage,
						},
					},
					Spec: hivev1.ClusterImageSetSpec{
						ReleaseImage: testDigestImage,
					},
				}
				Expect(fakeClient.Create(ctx, existingImageSet)).To(Succeed())

				// No mock calls expected - should not re-resolve

				// Execute
				err := manager.EnsureDigestBasedClusterImageSet(ctx, testImageSetName, testReleaseImage, acp)
				Expect(err).NotTo(HaveOccurred())

				// Verify ClusterImageSet unchanged
				imageSet := &hivev1.ClusterImageSet{}
				err = fakeClient.Get(ctx, client.ObjectKey{Name: testImageSetName}, imageSet)
				Expect(err).NotTo(HaveOccurred())
				Expect(imageSet.Spec.ReleaseImage).To(Equal(testDigestImage))
			})
		})

		Context("When ClusterImageSet exists with different source", func() {
			It("re-resolves digest and updates ClusterImageSet", func() {
				oldReleaseImage := "quay.io/openshift-release-dev/ocp-release:4.15.0-x86_64"
				oldDigest := "sha256:old123"

				// Pre-create ClusterImageSet with old digest
				existingImageSet := &hivev1.ClusterImageSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: testImageSetName,
						Annotations: map[string]string{
							"cluster.x-k8s.io/release-image-source": oldReleaseImage,
						},
					},
					Spec: hivev1.ClusterImageSetSpec{
						ReleaseImage: testReleaseImageRepo + "@" + oldDigest,
					},
				}
				Expect(fakeClient.Create(ctx, existingImageSet)).To(Succeed())

				// Create pull secret
				pullSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testPullSecretName,
						Namespace: testNamespace,
					},
					Data: map[string][]byte{
						".dockerconfigjson": []byte(testPullSecret),
					},
				}
				Expect(fakeClient.Create(ctx, pullSecret)).To(Succeed())

				// Mock digest resolution for new image
				mockRemoteImage.EXPECT().
					GetDigest(testReleaseImage, gomock.Any()).
					Return(testDigest, nil)

				// Execute
				err := manager.EnsureDigestBasedClusterImageSet(ctx, testImageSetName, testReleaseImage, acp)
				Expect(err).NotTo(HaveOccurred())

				// Verify ClusterImageSet updated with new digest
				imageSet := &hivev1.ClusterImageSet{}
				err = fakeClient.Get(ctx, client.ObjectKey{Name: testImageSetName}, imageSet)
				Expect(err).NotTo(HaveOccurred())
				Expect(imageSet.Spec.ReleaseImage).To(Equal(testDigestImage))
				Expect(imageSet.Annotations).To(HaveKeyWithValue("cluster.x-k8s.io/release-image-source", testReleaseImage))
			})
		})

		Context("When ClusterImageSet exists with tag-based image", func() {
			It("resolves digest and updates to digest-based image", func() {
				// Pre-create ClusterImageSet with tag-based image
				existingImageSet := &hivev1.ClusterImageSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: testImageSetName,
						Annotations: map[string]string{
							"cluster.x-k8s.io/release-image-source": testReleaseImage,
						},
					},
					Spec: hivev1.ClusterImageSetSpec{
						ReleaseImage: testReleaseImage, // Tag-based
					},
				}
				Expect(fakeClient.Create(ctx, existingImageSet)).To(Succeed())

				// Create pull secret
				pullSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testPullSecretName,
						Namespace: testNamespace,
					},
					Data: map[string][]byte{
						".dockerconfigjson": []byte(testPullSecret),
					},
				}
				Expect(fakeClient.Create(ctx, pullSecret)).To(Succeed())

				// Mock digest resolution
				mockRemoteImage.EXPECT().
					GetDigest(testReleaseImage, gomock.Any()).
					Return(testDigest, nil)

				// Execute
				err := manager.EnsureDigestBasedClusterImageSet(ctx, testImageSetName, testReleaseImage, acp)
				Expect(err).NotTo(HaveOccurred())

				// Verify ClusterImageSet updated to digest-based
				imageSet := &hivev1.ClusterImageSet{}
				err = fakeClient.Get(ctx, client.ObjectKey{Name: testImageSetName}, imageSet)
				Expect(err).NotTo(HaveOccurred())
				Expect(imageSet.Spec.ReleaseImage).To(Equal(testDigestImage))
			})
		})
	})

	Describe("NewManager", func() {
		It("creates a new manager successfully", func() {
			mgr := imageset.NewManager(fakeClient, mockRemoteImage)
			Expect(mgr).NotTo(BeNil())
		})
	})
})
