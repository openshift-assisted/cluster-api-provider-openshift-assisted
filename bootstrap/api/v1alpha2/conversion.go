/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha2

import (
	ctrl "sigs.k8s.io/controller-runtime"
)

// Hub marks v1alpha2 as the hub version (storage version) for conversion
func (*OpenshiftAssistedConfig) Hub()         {}
func (*OpenshiftAssistedConfigTemplate) Hub() {}

// SetupWebhookWithManager sets up the conversion webhook for OpenshiftAssistedConfigTemplate.
// This registers the /convert endpoint that handles v1alpha1 <-> v1alpha2 conversions.
func (r *OpenshiftAssistedConfigTemplate) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}
