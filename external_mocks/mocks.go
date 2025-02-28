package external_mocks

//go:generate mockgen -destination=mocks_containerregistry.go -package=external_mocks github.com/google/go-containerregistry/pkg/v1 Image,Layer
