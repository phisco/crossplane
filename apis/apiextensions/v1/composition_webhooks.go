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

package v1

import (
	"context"
	"fmt"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	errMixed = "cannot mix named and anonymous resource templates"

	errDuplicate = "resource template names must be unique within their Composition"
)

func DefaultCompositeValidationChain() ValidationChain {
	return ValidationChain{
		RejectMixedTemplates,
		RejectDuplicateNames,
	}
}

// +kubebuilder:webhook:verbs=update;create,path=/validate-apiextensions-crossplane-io-v1-composition,mutating=false,failurePolicy=fail,groups=apiextensions.crossplane.io,resources=compositions,versions=v1,name=compositions.apiextensions.crossplane.io,sideEffects=None,admissionReviewVersions=v1

func (in *Composition) ValidateCreate(ctx context.Context, obj runtime.Object) error {
	fmt.Println("HERE: GOT A CREATE")
	return in.Validate()
}

func (in *Composition) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) error {
	fmt.Println("HERE: GOT AN UPDATE")
	return in.Validate()
}

func (in *Composition) ValidateDelete(ctx context.Context, obj runtime.Object) error {
	fmt.Println("HERE: GOT A DELETE")
	return nil
}

func (in *Composition) Validate(ctx context.Context) error {
	fmt.Println("HERE: VALIDATING")
	return DefaultCompositeValidationChain().Validate(ctx, in)
}

// SetupWebhookWithManager sets up webhook with manager.
func (in *Composition) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		WithValidator(in).
		For(in).
		Complete()
}

// A CompositionValidatorFn validates the supplied Composition.
type CompositionValidatorFn func(comp *Composition) error

// Validate the supplied Composition.
func (fn CompositionValidatorFn) Validate(comp *Composition) error {
	return fn(comp)
}

// A ValidationChain runs multiple validations.
type ValidationChain []CompositionValidatorFn

// Validate the supplied Composition.
func (vs ValidationChain) Validate(comp *Composition) error {
	for _, v := range vs {
		if err := v.Validate(comp); err != nil {
			return err
		}
	}
	return nil
}

// RejectMixedTemplates validates that the supplied Composition does not attempt
// to mix named and anonymous templates. If some but not all templates are named
// it's safest to refuse to operate. We don't have enough information to use the
// named composer, but using the anonymous composer may be surprising. There's a
// risk that someone added a new anonymous template to a Composition that
// otherwise uses named templates. If they added the new template to the
// beginning or middle of the resources array using the anonymous composer would
// be destructive, because it assumes template N always corresponds to existing
// template N.
func RejectMixedTemplates(comp *Composition) error {
	named := 0
	for _, tmpl := range comp.Spec.Resources {
		if tmpl.Name != nil {
			named++
		}
	}

	// We're using only anonymous templates.
	if named == 0 {
		return nil
	}

	// We're using only named templates.
	if named == len(comp.Spec.Resources) {
		return nil
	}

	return errors.New(errMixed)
}

// RejectDuplicateNames validates that all template names are unique within the
// supplied Composition.
func RejectDuplicateNames(comp *Composition) error {
	seen := map[string]bool{}
	for _, tmpl := range comp.Spec.Resources {
		if tmpl.Name == nil {
			continue
		}
		if seen[*tmpl.Name] {
			return errors.New(errDuplicate)
		}
		seen[*tmpl.Name] = true
	}
	return nil
}
