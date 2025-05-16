package v1alpha1

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-bootstrap-cluster-x-k8s-io-v1alpha1-openshiftassistedconfig,mutating=false,failurePolicy=fail,sideEffects=None,groups=bootstrap.cluster.x-k8s.io,resources=openshiftassistedconfigs,verbs=create;update;delete,name=validation.openshiftassistedconfig.bootstrap.cluster.x-k8s.io,versions=v1alpha1,admissionReviewVersions=v1,serviceName=webhook-service,serviceNamespace=capoa-bootstrap-system

var _ webhook.CustomValidator = &OpenshiftAssistedConfig{}

func (webhook *OpenshiftAssistedConfig) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(webhook).
		WithValidator(webhook).
		Complete()
}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type.
func (webhook *OpenshiftAssistedConfig) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	_, ok := obj.(*OpenshiftAssistedConfig)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected an OpenshiftAssistedConfig but got a %T", obj))
	}

	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type.
func (webhook *OpenshiftAssistedConfig) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	oldConfig, ok := oldObj.(*OpenshiftAssistedConfig)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a OpenshiftAssistedConfig but got a %T", oldObj))
	}

	newConfig, ok := newObj.(*OpenshiftAssistedConfig)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a OpenshiftAssistedConfig but got a %T", newObj))
	}
	if equality.Semantic.DeepEqual(oldConfig.Spec, newConfig.Spec) {
		return nil, nil
	}
	return nil, apierrors.NewBadRequest("spec is immutable")
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type.
func (webhook *OpenshiftAssistedConfig) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
