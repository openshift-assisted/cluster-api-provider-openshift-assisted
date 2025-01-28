package version

import (
	"context"
	"fmt"

	controlplanev1alpha2 "github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1alpha2"
	"github.com/openshift-assisted/cluster-api-agent/controlplane/internal/auth"
	"github.com/openshift-assisted/cluster-api-agent/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewOpenShiftVersion(client client.Client) *OpenShiftVersion {
	return &OpenShiftVersion{
		Client: client,
	}
}

type Versioner interface {
	GetK8sVersionFromReleaseImage(ctx context.Context, releaseImage string, oacp *controlplanev1alpha2.OpenshiftAssistedControlPlane) (*string, error)
}

type OpenShiftVersion struct {
	Client client.Client
}

func (o *OpenShiftVersion) GetK8sVersionFromReleaseImage(ctx context.Context, releaseImage string, oacp *controlplanev1alpha2.OpenshiftAssistedControlPlane) (*string, error) {
	if releaseImage == "" || oacp.Spec.Config.PullSecretRef == nil {
		return nil, fmt.Errorf("invalid release image or empty pull secret")
	}
	secret := &corev1.Secret{}
	if err := o.Client.Get(ctx, types.NamespacedName{Namespace: oacp.Namespace, Name: oacp.Spec.Config.PullSecretRef.Name}, secret); err != nil {
		return nil, err
	}
	pullSecret, ok := secret.Data[auth.PullsecretDataKey]
	if !ok {
		return nil, fmt.Errorf("pullsecret secret does not have key %s", auth.PullsecretDataKey)
	}

	k8sVersion, err := util.GetK8sVersionFromImageRef(releaseImage, string(pullSecret))
	return &k8sVersion, err
}
