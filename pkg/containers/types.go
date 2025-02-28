package containers

import (
	"github.com/google/go-containerregistry/pkg/authn"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

//go:generate mockgen -destination=mock_containers.go -package=containers -source types.go RemoteImage,ContainerImage
type RemoteImage interface {
	GetImage(imageRef string, keychain authn.Keychain) (v1.Image, error)
	GetDigest(imageRef string, keychain authn.Keychain) (string, error)
}

type ContainerImage interface {
	ExtractFileFromImage(filePath string) ([]byte, error)
}

type RemoteImageRepository struct{}

func NewRemoteImageRepository() RemoteImage {
	return &RemoteImageRepository{}
}

type ImageInspector struct {
	image v1.Image
}

func NewImageInspector(image v1.Image) (ContainerImage, error) {
	return &ImageInspector{image: image}, nil
}
