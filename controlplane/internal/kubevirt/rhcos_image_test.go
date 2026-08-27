package kubevirt_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/internal/kubevirt"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testNamespace = "test-ns"

var _ = Describe("RHCOS Golden PVC", func() {
	var (
		ctx         context.Context
		scheme      *runtime.Scheme
		oacp        *controlplanev1alpha3.OpenshiftAssistedControlPlane
		namespace   string
		fakeClient  client.Client
		infraClient client.Client
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = testNamespace
		scheme = runtime.NewScheme()
		utilruntime.Must(corev1.AddToScheme(scheme))
		utilruntime.Must(batchv1.AddToScheme(scheme))
		utilruntime.Must(controlplanev1alpha3.AddToScheme(scheme))

		oacp = &controlplanev1alpha3.OpenshiftAssistedControlPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: namespace,
			},
			Spec: controlplanev1alpha3.OpenshiftAssistedControlPlaneSpec{
				DistributionVersion: "4.22.2",
			},
		}
	})

	Describe("GoldenPVCName", func() {
		It("should return name with major.minor for full version", func() {
			Expect(kubevirt.GoldenPVCName("4.22.2")).To(Equal("rhcos-golden-4.22"))
		})

		It("should return name with major.minor for two-part version", func() {
			Expect(kubevirt.GoldenPVCName("4.22")).To(Equal("rhcos-golden-4.22"))
		})

		It("should use raw version when parsing fails", func() {
			Expect(kubevirt.GoldenPVCName("latest")).To(Equal("rhcos-golden-latest"))
		})

		It("should handle empty version", func() {
			Expect(kubevirt.GoldenPVCName("")).To(Equal("rhcos-golden-"))
		})
	})

	Describe("EnsureRHCOSGoldenPVC", func() {
		Context("When the annotation is already set", func() {
			It("should return true immediately", func() {
				oacp.Annotations = map[string]string{
					kubevirt.RHCOSGoldenPVCReadyAnnotation: "rhcos-golden-4.22",
				}
				fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp).Build()
				infraClient = fake.NewClientBuilder().WithScheme(scheme).Build()

				ready, err := kubevirt.EnsureRHCOSGoldenPVC(ctx, fakeClient, infraClient, oacp,
					"quay.io/release:4.22", "pull-secret", namespace, "")
				Expect(err).NotTo(HaveOccurred())
				Expect(ready).To(BeTrue())
			})
		})

		Context("When version is invalid", func() {
			It("should return an error", func() {
				oacp.Spec.DistributionVersion = "invalid"
				fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp).Build()
				infraClient = fake.NewClientBuilder().WithScheme(scheme).Build()

				_, err := kubevirt.EnsureRHCOSGoldenPVC(ctx, fakeClient, infraClient, oacp,
					"quay.io/release:4.22", "pull-secret", namespace, "")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("cannot extract major.minor"))
			})
		})

		Context("When the URL ConfigMap already exists", func() {
			It("should proceed to PVC creation", func() {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "capoa-rhcos-url-4.22",
						Namespace: namespace,
					},
					Data: map[string]string{
						kubevirt.RHCOSURLConfigMapKey: "https://example.com/rhcos.ociarchive",
					},
				}
				fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp).Build()
				infraClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()

				ready, err := kubevirt.EnsureRHCOSGoldenPVC(ctx, fakeClient, infraClient, oacp,
					"quay.io/release:4.22", "pull-secret", namespace, "")
				Expect(err).NotTo(HaveOccurred())
				Expect(ready).To(BeFalse())

				pvc := &corev1.PersistentVolumeClaim{}
				err = infraClient.Get(ctx, client.ObjectKey{Name: "rhcos-golden-4.22", Namespace: namespace}, pvc)
				Expect(err).NotTo(HaveOccurred())
				Expect(pvc.Spec.VolumeMode).NotTo(BeNil())
				Expect(*pvc.Spec.VolumeMode).To(Equal(corev1.PersistentVolumeBlock))
			})
		})

		Context("When the import Job has succeeded", func() {
			It("should mark the OACP annotation and return true", func() {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "capoa-rhcos-url-4.22",
						Namespace: namespace,
					},
					Data: map[string]string{
						kubevirt.RHCOSURLConfigMapKey: "https://example.com/rhcos.ociarchive",
					},
				}
				pvc := &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "rhcos-golden-4.22",
						Namespace: namespace,
					},
				}
				job := &batchv1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "rhcos-import-4.22",
						Namespace: namespace,
					},
					Status: batchv1.JobStatus{
						Succeeded: 1,
					},
				}
				fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp).Build()
				infraClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, pvc, job).Build()

				ready, err := kubevirt.EnsureRHCOSGoldenPVC(ctx, fakeClient, infraClient, oacp,
					"quay.io/release:4.22", "pull-secret", namespace, "")
				Expect(err).NotTo(HaveOccurred())
				Expect(ready).To(BeTrue())

				updatedOACP := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
				err = fakeClient.Get(ctx, client.ObjectKey{Name: oacp.Name, Namespace: oacp.Namespace}, updatedOACP)
				Expect(err).NotTo(HaveOccurred())
				Expect(updatedOACP.Annotations[kubevirt.RHCOSGoldenPVCReadyAnnotation]).To(Equal("rhcos-golden-4.22"))
			})
		})

		Context("When the import Job is still running", func() {
			It("should return false without error", func() {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "capoa-rhcos-url-4.22",
						Namespace: namespace,
					},
					Data: map[string]string{
						kubevirt.RHCOSURLConfigMapKey: "https://example.com/rhcos.ociarchive",
					},
				}
				pvc := &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "rhcos-golden-4.22",
						Namespace: namespace,
					},
				}
				job := &batchv1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "rhcos-import-4.22",
						Namespace: namespace,
					},
					Status: batchv1.JobStatus{},
				}
				fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp).Build()
				infraClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, pvc, job).Build()

				ready, err := kubevirt.EnsureRHCOSGoldenPVC(ctx, fakeClient, infraClient, oacp,
					"quay.io/release:4.22", "pull-secret", namespace, "")
				Expect(err).NotTo(HaveOccurred())
				Expect(ready).To(BeFalse())
			})
		})

		Context("When no URL resolution Job exists", func() {
			It("should create the URL resolution Job and return false", func() {
				fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp).Build()
				infraClient = fake.NewClientBuilder().WithScheme(scheme).Build()

				ready, err := kubevirt.EnsureRHCOSGoldenPVC(ctx, fakeClient, infraClient, oacp,
					"quay.io/release:4.22", "pull-secret", namespace, "")
				Expect(err).NotTo(HaveOccurred())
				Expect(ready).To(BeFalse())

				job := &batchv1.Job{}
				err = infraClient.Get(ctx, client.ObjectKey{Name: "rhcos-url-resolve-4.22", Namespace: namespace}, job)
				Expect(err).NotTo(HaveOccurred())
				Expect(job.Spec.Template.Spec.Containers[0].Env[0].Value).To(Equal("quay.io/release:4.22"))
			})
		})
	})
})

var _ = Describe("ResolveCliImage", func() {
	var (
		ctx        context.Context
		scheme     *runtime.Scheme
		fakeClient client.Client
		namespace  string
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = testNamespace
		scheme = runtime.NewScheme()
		utilruntime.Must(corev1.AddToScheme(scheme))
		utilruntime.Must(batchv1.AddToScheme(scheme))
	})

	Context("When the ConfigMap already contains the CLI image", func() {
		It("should return the cached image", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      kubevirt.CliImageConfigMap,
					Namespace: namespace,
				},
				Data: map[string]string{"cli-image": "quay.io/openshift/cli:4.22"},
			}
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()

			img, err := kubevirt.ResolveCliImage(ctx, fakeClient, namespace, "quay.io/release:4.22", "pull-secret", "4.22.2")
			Expect(err).NotTo(HaveOccurred())
			Expect(img).To(Equal("quay.io/openshift/cli:4.22"))
		})
	})

	Context("When no Job exists", func() {
		It("should create the Job and return empty string", func() {
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()

			img, err := kubevirt.ResolveCliImage(ctx, fakeClient, namespace, "quay.io/release:4.22", "pull-secret", "4.22.2")
			Expect(err).NotTo(HaveOccurred())
			Expect(img).To(BeEmpty())

			job := &batchv1.Job{}
			err = fakeClient.Get(ctx, client.ObjectKey{Name: "resolve-cli-image-4.22", Namespace: namespace}, job)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("When version is invalid", func() {
		It("should return an error", func() {
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()

			_, err := kubevirt.ResolveCliImage(ctx, fakeClient, namespace, "quay.io/release:4.22", "pull-secret", "bad")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot extract major.minor"))
		})
	})
})

var _ = Describe("readURLFromJobPod", func() {
	var (
		ctx        context.Context
		scheme     *runtime.Scheme
		fakeClient client.Client
		namespace  string
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = testNamespace
		scheme = runtime.NewScheme()
		utilruntime.Must(corev1.AddToScheme(scheme))
	})

	Context("When a succeeded pod has a termination message", func() {
		It("should return the trimmed URL", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job-pod-1",
					Namespace: namespace,
					Labels:    map[string]string{"job-name": "test-job"},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodSucceeded,
					ContainerStatuses: []corev1.ContainerStatus{
						{
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									Message: "https://example.com/rhcos.ociarchive\n",
								},
							},
						},
					},
				},
			}
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()

			url, err := kubevirt.ReadURLFromJobPod(ctx, fakeClient, "test-job", namespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(url).To(Equal("https://example.com/rhcos.ociarchive"))
		})
	})

	Context("When no succeeded pod exists", func() {
		It("should return an error", func() {
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()

			_, err := kubevirt.ReadURLFromJobPod(ctx, fakeClient, "test-job", namespace)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no completed pod found"))
		})
	})
})
