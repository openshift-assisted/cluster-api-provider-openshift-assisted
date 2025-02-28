package containers_test

import (
	"io"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-assisted/cluster-api-agent/external_mocks"
	"github.com/openshift-assisted/cluster-api-agent/util/test"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/openshift-assisted/cluster-api-agent/pkg/containers"
)

var _ = Describe("ImageInspector", func() {
	var (
		ctrl      *gomock.Controller
		extractor containers.ContainerImage
	)
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
	})
	AfterEach(func() {
		ctrl.Finish()
	})
	Context("when extracting a regular file", func() {
		It("should return the correct file content", func() {
			mockLayer := external_mocks.NewMockLayer(ctrl)
			mockLayer.EXPECT().Uncompressed().Return(test.CreateTarArchive(test.TarArchive{
				Filepath: "test.txt",
				Content:  "Hello, World!",
			}), nil)
			mockImage := external_mocks.NewMockImage(ctrl)
			mockImage.EXPECT().Layers().Return([]v1.Layer{mockLayer}, nil)
			var err error
			extractor, err = containers.NewImageInspector(mockImage)
			Expect(err).NotTo(HaveOccurred())
			content, err := extractor.ExtractFileFromImage("/test.txt")

			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal("Hello, World!"))
		})
	})

	Context("when the file does not exist", func() {
		It("should return an error", func() {
			mockLayer := external_mocks.NewMockLayer(ctrl)
			mockLayer.EXPECT().Uncompressed().Return(test.CreateTarArchive(test.TarArchive{
				Filepath: "somefile.txt",
				Content:  "Data",
			}), nil)
			mockImage := external_mocks.NewMockImage(ctrl)
			mockImage.EXPECT().Layers().Return([]v1.Layer{mockLayer}, nil)
			var err error
			extractor, err = containers.NewImageInspector(mockImage)
			Expect(err).NotTo(HaveOccurred())

			_, err = extractor.ExtractFileFromImage("/missing.txt")

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("file /missing.txt not found"))
		})
	})

	Context("when handling symlinks", func() {
		It("should correctly resolve symlinks", func() {
			mockLayer1 := external_mocks.NewMockLayer(ctrl)
			mockLayer1.EXPECT().Uncompressed().DoAndReturn(
				func() (io.ReadCloser, error) {
					return test.CreateTarArchive(test.TarArchive{
						Filepath:      "link.txt",
						IsSymlink:     true,
						SymlinkTarget: "real.txt",
					}), nil
				})
			mockLayer2 := external_mocks.NewMockLayer(ctrl)
			mockLayer2.EXPECT().Uncompressed().DoAndReturn(
				func() (io.ReadCloser, error) {
					return test.CreateTarArchive(test.TarArchive{
						Filepath: "real.txt",
						Content:  "Actual content",
					}), nil
				}).Times(2)

			mockImage := external_mocks.NewMockImage(ctrl)
			mockImage.EXPECT().Layers().Return([]v1.Layer{mockLayer1, mockLayer2}, nil).Times(2)

			var err error
			extractor, err = containers.NewImageInspector(mockImage)
			Expect(err).NotTo(HaveOccurred())
			content, err := extractor.ExtractFileFromImage("/link.txt")

			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal("Actual content"))
		})

		It("should detect symlink loops", func() {
			mockLayer1 := external_mocks.NewMockLayer(ctrl)
			mockLayer1.EXPECT().Uncompressed().DoAndReturn(
				func() (io.ReadCloser, error) {
					return test.CreateTarArchive(test.TarArchive{
						Filepath:      "loop1.txt",
						IsSymlink:     true,
						SymlinkTarget: "loop2.txt",
					}), nil
				}).AnyTimes()
			mockLayer2 := external_mocks.NewMockLayer(ctrl)
			mockLayer2.EXPECT().Uncompressed().DoAndReturn(
				func() (io.ReadCloser, error) {
					return test.CreateTarArchive(test.TarArchive{
						Filepath:      "loop2.txt",
						IsSymlink:     true,
						SymlinkTarget: "loop1.txt",
					}), nil
				}).AnyTimes()

			mockImage := external_mocks.NewMockImage(ctrl)
			mockImage.EXPECT().Layers().Return([]v1.Layer{mockLayer1, mockLayer2}, nil).AnyTimes()

			var err error
			extractor, err = containers.NewImageInspector(mockImage)
			Expect(err).NotTo(HaveOccurred())

			_, err = extractor.ExtractFileFromImage("/loop1.txt")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("too many symlink resolutions"))
		})
	})

	Context("when handling layers", func() {
		It("should respect layer ordering (newer files override older ones)", func() {
			mockLayer1 := external_mocks.NewMockLayer(ctrl)
			mockLayer1.EXPECT().Uncompressed().DoAndReturn(
				func() (io.ReadCloser, error) {
					return test.CreateTarArchive(test.TarArchive{
						Filepath:      "test.txt",
						Content:       "Old data",
						IsSymlink:     false,
						SymlinkTarget: "",
					}), nil
				}).Times(0)
			mockLayer2 := external_mocks.NewMockLayer(ctrl)
			mockLayer2.EXPECT().Uncompressed().DoAndReturn(
				func() (io.ReadCloser, error) {
					return test.CreateTarArchive(test.TarArchive{
						Filepath: "test.txt",
						Content:  "New data",
					}), nil
				})

			mockImage := external_mocks.NewMockImage(ctrl)
			mockImage.EXPECT().Layers().Return([]v1.Layer{mockLayer1, mockLayer2}, nil)

			var err error
			var content []byte
			extractor, err = containers.NewImageInspector(mockImage)
			Expect(err).NotTo(HaveOccurred())
			content, err = extractor.ExtractFileFromImage("/test.txt")
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal("New data"))
		})
	})
})
