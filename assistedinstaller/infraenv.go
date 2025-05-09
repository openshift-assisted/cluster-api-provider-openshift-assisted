package assistedinstaller

import (
	"fmt"
	"net/url"

	bootstrapv1alpha1 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/bootstrap/api/v1alpha1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

func GetInfraEnvFromConfig(
	infraEnvName string,
	config *bootstrapv1alpha1.OpenshiftAssistedConfig,
	clusterDeployment *hivev1.ClusterDeployment,
) *aiv1beta1.InfraEnv {
	infraEnv := &aiv1beta1.InfraEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name:      infraEnvName,
			Namespace: config.Namespace,
			Labels: map[string]string{
				bootstrapv1alpha1.OpenshiftAssistedConfigLabel: config.Name,
			},
		},
	}

	clusterName, ok := config.Labels[clusterv1.ClusterNameLabel]
	if ok {
		infraEnv.Labels[clusterv1.ClusterNameLabel] = clusterName
	}

	var pullSecret *corev1.LocalObjectReference
	if config.Spec.PullSecretRef != nil {
		pullSecret = config.Spec.PullSecretRef
	}
	infraEnv.Spec = aiv1beta1.InfraEnvSpec{
		AdditionalNTPSources:  config.Spec.AdditionalNTPSources,
		AdditionalTrustBundle: config.Spec.AdditionalTrustBundle,
		ClusterRef: &aiv1beta1.ClusterReference{
			Name:      clusterDeployment.Name,
			Namespace: clusterDeployment.Namespace,
		},
		CpuArchitecture:            config.Spec.CpuArchitecture,
		KernelArguments:            config.Spec.KernelArguments,
		NMStateConfigLabelSelector: config.Spec.NMStateConfigLabelSelector,
		OSImageVersion:             config.Spec.OSImageVersion,
		Proxy:                      config.Spec.Proxy,
		PullSecretRef:              pullSecret,
		SSHAuthorizedKey:           config.Spec.SSHAuthorizedKey,
		// Must be full-iso to ensure static networking settings is generated in the ignition
		ImageType: "full-iso",
	}
	return infraEnv
}

// Query ignition and set it as user data secret
func GetIgnitionURLFromInfraEnv(config ServiceConfig, infraEnv aiv1beta1.InfraEnv) (*url.URL, error) {
	if infraEnv.Status.InfraEnvDebugInfo.EventsURL == "" {
		return nil, fmt.Errorf("cannot generate ignition url if events URL is not generated")
	}
	advertisedURL, err := url.Parse(infraEnv.Status.InfraEnvDebugInfo.EventsURL)
	if err != nil {
		return nil, err
	}
	scheme := advertisedURL.Scheme
	hostname := advertisedURL.Hostname()
	port := advertisedURL.Port()
	if port != "" {
		hostname = hostname + ":" + port
	}
	if config.UseInternalImageURL {
		scheme = "http"
		port = "8090"
		ns := config.AssistedInstallerNamespace
		if ns == "" {
			ns = infraEnv.Namespace
		}
		svc := config.AssistedServiceName

		hostname = fmt.Sprintf("%s.%s.svc.cluster.local:%s", svc, ns, port)
	}
	q, err := url.ParseQuery(advertisedURL.RawQuery)
	if err != nil {
		return nil, err
	}

	qApiKey, ok := q["api_key"]
	if !ok || len(qApiKey) < 1 {
		return nil, fmt.Errorf("no api_key found in query string")
	}
	apiKey := qApiKey[0]

	qInfraEnvID := q["infra_env_id"]
	if !ok || len(qInfraEnvID) < 1 {
		return nil, fmt.Errorf("no infra_env_id found in query string")
	}
	infraEnvID := qInfraEnvID[0]

	ignitionURL := &url.URL{
		Scheme: scheme,
		Host:   hostname,
		Path:   fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/files", infraEnvID),
	}
	queryValues := url.Values{}
	queryValues.Set("file_name", "discovery.ign")
	queryValues.Set("api_key", apiKey)
	ignitionURL.RawQuery = queryValues.Encode()
	return ignitionURL, nil
}
