/*
Copyright 2023 The Crossplane Authors.

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

// +kubebuilder:webhook:verbs=update;create,path=/validate-apiextensions-crossplane-io-v1-composition,mutating=false,failurePolicy=fail,groups=apiextensions.crossplane.io,resources=compositions,versions=v1,name=compositions.apiextensions.crossplane.io,sideEffects=None,admissionReviewVersions=v1

package v1

import (
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	CompositionValidationModeAnnotation = "crossplane.io/composition-validation-mode"
)

type CompositionValidationMode string

var (
	DefaultCompositionValidationMode                           = CompositionValidationModeLoose
	CompositionValidationModeLoose   CompositionValidationMode = "loose"
	CompositionValidationModeStrict  CompositionValidationMode = "strict"
)

func (in *Composition) SetupWebhookWithManager(mgr ctrl.Manager, validator admission.CustomValidator) error {
	return ctrl.NewWebhookManagedBy(mgr).
		WithValidator(validator).
		For(in).
		Complete()
}
