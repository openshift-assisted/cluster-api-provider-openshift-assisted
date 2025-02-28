package auth

import (
	"context"
	"fmt"

	controlplanev1alpha2 "github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func GetPullSecret(c client.Client, ctx context.Context, oacp *controlplanev1alpha2.OpenshiftAssistedControlPlane) ([]byte, error) {
	secret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: oacp.Namespace, Name: oacp.Spec.Config.PullSecretRef.Name}, secret); err != nil {
		return nil, err
	}
	pullSecret, ok := secret.Data[PullsecretDataKey]
	if !ok {
		return nil, fmt.Errorf("pullsecret secret does not have key %s", PullsecretDataKey)
	}
	return pullSecret, nil
}
