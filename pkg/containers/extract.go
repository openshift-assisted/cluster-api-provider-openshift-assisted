package containers

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

const maxSymlinkDepth = 5

type ContainerImageExtractor interface {
	ExtractFileFromImage(filePath string) ([]byte, error)
}

type ImageExtractor struct {
	Image v1.Image
}

func NewImageExtractor(imageRef string, keychain authn.Keychain) (ContainerImageExtractor, error) {
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Image reference: %w", err)
	}
	img, err := remote.Image(ref, remote.WithAuthFromKeychain(keychain))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Image: %w", err)
	}
	return &ImageExtractor{Image: img}, nil
}

func (e *ImageExtractor) ExtractFileFromImage(filePath string) ([]byte, error) {
	return e.extractFileFromImageInternal(filePath, 0)
}

// ExtractFileFromImage pulls an OCI Image and extracts a file, resolving symlinks
func (e *ImageExtractor) extractFileFromImageInternal(filePath string, depth int) ([]byte, error) {
	if depth > maxSymlinkDepth {
		return nil, fmt.Errorf("too many symlink resolutions, possible loop")
	}

	layers, err := e.Image.Layers()
	if err != nil {
		return nil, fmt.Errorf("failed to get Image layers: %w", err)
	}
	fmt.Printf("checking layers [%d]\n", len(layers))

	// Iterate from the latest layer to the oldest
	for i := len(layers) - 1; i >= 0; i-- {
		layer := layers[i]
		layerReader, err := layer.Uncompressed()
		if err != nil {
			return nil, fmt.Errorf("failed to get layer data: %w", err)
		}

		tr := tar.NewReader(layerReader)
		for {
			header, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("error reading tar: %w", err)
			}

			// Normalize the tar file path
			tarPath := "/" + header.Name
			fmt.Printf("Analyzing %s\n", tarPath)
			// If it's a symlink, follow it
			if tarPath == filePath && header.Typeflag == tar.TypeSymlink {
				fmt.Printf("%s is symlink\n", tarPath)

				resolvedPath := header.Linkname
				if !strings.HasPrefix(resolvedPath, "/") {
					resolvedPath = filepath.Join(filepath.Dir(filePath), resolvedPath)
				}
				fmt.Printf("%s resolves to %s\n", tarPath, resolvedPath)
				return e.extractFileFromImageInternal(resolvedPath, depth+1) // Follow symlink
			}

			// If it's the actual file, return contents
			if tarPath == filePath {
				fmt.Printf("%s is %s\n", tarPath, filePath)

				var buf bytes.Buffer
				_, err := io.Copy(&buf, tr)
				if err != nil {
					return nil, fmt.Errorf("failed to read file content: %w", err)
				}
				return buf.Bytes(), nil
			}
		}
		err = layerReader.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to close tar: %w", err)
		}
	}

	return nil, fmt.Errorf("file %s not found in Image", filePath)
}
