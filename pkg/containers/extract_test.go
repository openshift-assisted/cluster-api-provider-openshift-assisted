package containers_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/google/go-containerregistry/pkg/authn"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/openshift-assisted/cluster-api-agent/pkg/containers"
)

var _ = Describe("ImageExtractor", func() {
	var (
		extractor *containers.ImageExtractor
	)

	Context("when extracting a regular file", func() {
		It("should return the correct file content", func() {
			mockLayer := NewMockLayer(tarArchive{"test.txt", "Hello, World!", false, ""})
			mockImage := NewMockImage([]v1.Layer{mockLayer})
			extractor = &containers.ImageExtractor{Image: mockImage}

			content, err := extractor.ExtractFileFromImage("/test.txt")

			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal("Hello, World!"))
		})
	})

	Context("when the file does not exist", func() {
		It("should return an error", func() {
			mockLayer := NewMockLayer(tarArchive{"somefile.txt", "Data", false, ""})
			mockImage := NewMockImage([]v1.Layer{mockLayer})
			extractor = &containers.ImageExtractor{Image: mockImage}

			_, err := extractor.ExtractFileFromImage("/missing.txt")

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("file /missing.txt not found"))
		})
	})

	Context("when handling symlinks", func() {
		It("should correctly resolve symlinks", func() {
			layer1 := NewMockLayer(tarArchive{"link.txt", "", true, "real.txt"})
			layer2 := NewMockLayer(tarArchive{"real.txt", "Actual content", false, ""})
			mockImage := NewMockImage([]v1.Layer{layer1, layer2})
			extractor = &containers.ImageExtractor{Image: mockImage}

			content, err := extractor.ExtractFileFromImage("/link.txt")

			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal("Actual content"))
		})

		It("should detect symlink loops", func() {
			layer1 := NewMockLayer(tarArchive{"loop1.txt", "", true, "loop2.txt"})

			layer2 := NewMockLayer(tarArchive{"loop2.txt", "", true, "loop1.txt"})
			mockImage := NewMockImage([]v1.Layer{layer1, layer2})
			extractor = &containers.ImageExtractor{Image: mockImage}

			_, err := extractor.ExtractFileFromImage("/loop1.txt")

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("too many symlink resolutions"))
		})
	})

	Context("when handling layers", func() {
		It("should respect layer ordering (newer files override older ones)", func() {
			layer1 := NewMockLayer(tarArchive{"test.txt", "Old data", false, ""})

			layer2 := NewMockLayer(tarArchive{"test.txt", "New data", false, ""})
			mockImage := NewMockImage([]v1.Layer{layer1, layer2})
			extractor = &containers.ImageExtractor{Image: mockImage}

			content, err := extractor.ExtractFileFromImage("/test.txt")

			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal("New data"))
		})
	})

	Context("when authentication fails", func() {
		It("should return an error for invalid Image reference", func() {
			keychain := authn.DefaultKeychain
			_, err := containers.NewImageExtractor("invalid Image", keychain)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse Image reference"))
		})
	})
})
