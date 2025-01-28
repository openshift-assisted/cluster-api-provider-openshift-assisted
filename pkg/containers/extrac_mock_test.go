package containers_test

import (
	"archive/tar"
	"bytes"
	"io"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// Mock Layer
type MockLayer struct {
	archive tarArchive
}

func NewMockLayer(archive tarArchive) *MockLayer {
	mockLayer := &MockLayer{archive: archive}
	return mockLayer
}

func (m *MockLayer) Uncompressed() (io.ReadCloser, error) {
	return createTarArchive(m.archive), nil
}

func (m *MockLayer) Compressed() (io.ReadCloser, error) {
	panic("not implemented")
}

// Digest returns a fake digest.
func (m *MockLayer) Digest() (v1.Hash, error) {
	return v1.Hash{Algorithm: "sha256", Hex: "mocked"}, nil
}

// DiffID returns a fake DiffID.
func (m *MockLayer) DiffID() (v1.Hash, error) {
	return v1.Hash{Algorithm: "sha256", Hex: "mocked-diff"}, nil
}

// Size returns a fake size.
func (m *MockLayer) Size() (int64, error) {
	return 123, nil
}

// MediaType returns a mock media type.
func (m *MockLayer) MediaType() (types.MediaType, error) {
	return types.MediaType("application/vnd.oci.Image.layer.v1.tar+gzip"), nil
}

func NewMockImage(layers []v1.Layer) *MockImage {
	return &MockImage{
		layers: layers,
	}
}

// Mock Image
type MockImage struct {
	layers []v1.Layer
}

func (m *MockImage) Layers() ([]v1.Layer, error) {
	return m.layers, nil
}

func (m *MockImage) MediaType() (types.MediaType, error) {
	panic("not implemented")
}

func (m *MockImage) Size() (int64, error) {
	panic("not implemented")
}

func (m *MockImage) ConfigName() (v1.Hash, error) {
	panic("not implemented")
}

func (m *MockImage) ConfigFile() (*v1.ConfigFile, error) {
	panic("not implemented")
}

func (m *MockImage) RawConfigFile() ([]byte, error) {
	panic("not implemented")
}

func (m *MockImage) Digest() (v1.Hash, error) {
	panic("not implemented")
}

func (m *MockImage) Manifest() (*v1.Manifest, error) {
	panic("not implemented")
}

func (m *MockImage) RawManifest() ([]byte, error) {
	panic("not implemented")
}

func (m *MockImage) LayerByDigest(hash v1.Hash) (v1.Layer, error) {
	panic("not implemented")
}

func (m *MockImage) LayerByDiffID(hash v1.Hash) (v1.Layer, error) {
	panic("not implemented")
}

type tarArchive struct {
	filepath      string
	content       string
	isSymlink     bool
	symlinkTarget string
}

// Helper function to create a tar archive with a file
func createTarArchive(archive tarArchive) io.ReadCloser {
	buf := new(bytes.Buffer)
	tw := tar.NewWriter(buf)

	hdr := &tar.Header{
		Name: archive.filepath,
		Mode: 0600,
		Size: int64(len(archive.content)),
	}
	hdr.Typeflag = tar.TypeReg
	if archive.isSymlink {
		hdr.Typeflag = tar.TypeSymlink
		hdr.Linkname = archive.symlinkTarget
	}

	_ = tw.WriteHeader(hdr)
	if !archive.isSymlink {
		_, _ = tw.Write([]byte(archive.content))
	}
	_ = tw.Close()

	return io.NopCloser(buf)
}
