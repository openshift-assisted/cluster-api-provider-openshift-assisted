package auth

import (
	"context"
	"fmt"

	"github.com/openshift-assisted/cluster-api-provider-openshift-assisted/assistedinstaller"

	controlplanev1alpha2 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetPullSecret(c client.Client, ctx context.Context, oacp *controlplanev1alpha2.OpenshiftAssistedControlPlane) ([]byte, error) {
	secret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: oacp.Namespace, Name: oacp.Spec.Config.PullSecretRef.Name}, secret); err != nil {
		return nil, err
	}
	pullSecret, ok := secret.Data[assistedinstaller.PullsecretDataKey]
	if !ok {
		return nil, fmt.Errorf("pullsecret secret does not have key %s", assistedinstaller.PullsecretDataKey)
	}
	return pullSecret, nil
}
