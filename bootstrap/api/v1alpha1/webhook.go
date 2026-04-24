package v1alpha1

import (
	"context"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-bootstrap-cluster-x-k8s-io-v1alpha1-openshiftassistedconfig,mutating=false,failurePolicy=fail,sideEffects=None,groups=bootstrap.cluster.x-k8s.io,resources=openshiftassistedconfigs,verbs=create;update;delete,name=v1alpha1.validation.openshiftassistedconfig.bootstrap.cluster.x-k8s.io,versions=v1alpha1,admissionReviewVersions=v1,serviceName=webhook-service,serviceNamespace=capoa-bootstrap-system

var _ admission.Validator[*OpenshiftAssistedConfig] = &OpenshiftAssistedConfig{}

func (webhook *OpenshiftAssistedConfig) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, webhook).
		WithValidator(webhook).
		Complete()
}

// ValidateCreate implements admission.CustomValidator so a webhook will be registered for the type.
func (webhook *OpenshiftAssistedConfig) ValidateCreate(_ context.Context, obj *OpenshiftAssistedConfig) (admission.Warnings, error) {
	return nil, nil
}

// ValidateUpdate implements admission.CustomValidator so a webhook will be registered for the type.
func (webhook *OpenshiftAssistedConfig) ValidateUpdate(ctx context.Context, oldObj, newObj *OpenshiftAssistedConfig) (admission.Warnings, error) {
	if equality.Semantic.DeepEqual(oldObj.Spec, newObj.Spec) {
		return nil, nil
	}
	return nil, apierrors.NewBadRequest("spec is immutable")
}

// ValidateDelete implements admission.CustomValidator so a webhook will be registered for the type.
func (webhook *OpenshiftAssistedConfig) ValidateDelete(_ context.Context, _ *OpenshiftAssistedConfig) (admission.Warnings, error) {
	return nil, nil
}
