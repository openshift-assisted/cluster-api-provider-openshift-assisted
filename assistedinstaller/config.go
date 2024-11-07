package assistedinstaller

type ServiceConfig struct {
	// UseInternalImageURL when set to false means that we'll use the InfraEnv's iso download URL
	// as is. When set to true, it'll use the assisted-image-service's internal IP as part of the
	// download URL.
	UseInternalImageURL bool `envconfig:"USE_INTERNAL_IMAGE_URL" default:"false"`
	// ImageServiceName is the Service CR name for the assisted-image-service
	ImageServiceName string `envconfig:"IMAGE_SERVICE_NAME" default:"assisted-image-service"`
	// ImageServiceNamespace is the namespace that the Service CR for the assisted-image-service is in
	ImageServiceNamespace string `envconfig:"IMAGE_SERVICE_NAMESPACE"`
	// AssistedServiceName is the name of the assisted-service
	AssistedServiceName string `envconfig:"ASSISTED_SERVICE_NAME" default:"assisted-service"`
	// AssistedServiceName is the namemespace of the assisted-service
	AssistedInstallerNamespace string `envconfig:"ASSISTED_INSTALLER_NAMESPACE"`
}
