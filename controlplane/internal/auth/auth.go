package auth

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const PullsecretDataKey = ".dockerconfigjson"

// Assisted-service expects the pull secret to
// 1. Have .dockerconfigjson as a key
// 2. Have the value of .dockerconfigjson be a base64-encoded JSON
// 3. The JSON must have the key "auths" followed by repository with an "auth" key
func GenerateFakePullSecret(name, namespace string) *corev1.Secret {
	// placeholder:secret base64 encoded is cGxhY2Vob2xkZXI6c2VjcmV0Cg==
	fakePullSecret := "{\"auths\":{\"fake-pull-secret\":{\"auth\":\"cGxhY2Vob2xkZXI6c2VjcmV0Cg==\"}}}"

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			PullsecretDataKey: []byte(fakePullSecret),
		},
	}
}
