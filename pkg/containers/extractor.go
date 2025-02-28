package containers

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

const maxSymlinkDepth = 5

func (e *ImageInspector) ExtractFileFromImage(filePath string) ([]byte, error) {
	if e.image == nil {
		return nil, fmt.Errorf("image not defined")
	}
	return e.extractFileFromImageInternal(e.image, filePath, 0)
}

// ExtractFileFromImage pulls an OCI Image and extracts a file, resolving symlinks
func (e *ImageInspector) extractFileFromImageInternal(image v1.Image, filePath string, depth int) ([]byte, error) {
	if depth > maxSymlinkDepth {
		return nil, fmt.Errorf("too many symlink resolutions, possible loop")
	}

	layers, err := image.Layers()
	if err != nil {
		return nil, fmt.Errorf("failed to get Image layers: %w", err)
	}

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
			// If it's a symlink, follow it
			if tarPath == filePath && header.Typeflag == tar.TypeSymlink {

				resolvedPath := header.Linkname
				if !strings.HasPrefix(resolvedPath, "/") {
					resolvedPath = filepath.Join(filepath.Dir(filePath), resolvedPath)
				}
				return e.extractFileFromImageInternal(image, resolvedPath, depth+1) // Follow symlink
			}

			// If it's the actual file, return contents
			if tarPath == filePath {

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
