/*
Copyright 2022 The Crossplane Authors.

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
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/crossplane/crossplane-runtime/pkg/validation"
)

// Error strings
const (
	errMixed           = "cannot mix named and anonymous resource templates - ensure all resource templates are named"
	errDuplicate       = "resource template names must be unique within their Composition"
	errFnsRequireNames = "cannot use functions with anonymous resource templates - ensure all resource templates are named"
)

var (
	defaultValidationChain = validation.Chain[Composition]{
		validation.ValidatorFn[Composition](RejectMixedTemplates),
		validation.ValidatorFn[Composition](RejectDuplicateNames),
		validation.ValidatorFn[Composition](RejectAnonymousTemplatesWithFunctions),
		validation.ValidatorFn[Composition](RejectFunctionsWithoutRequiredConfig),
	}
)

// RejectMixedTemplates validates that the supplied Composition does not attempt
// to mix named and anonymous templates. If some but not all templates are named
// it's safest to refuse to operate. We don't have enough information to use the
// named composer, but using the anonymous composer may be surprising. There's a
// risk that someone added a new anonymous template to a Composition that
// otherwise uses named templates. If they added the new template to the
// beginning or middle of the resources array using the anonymous composer would
// be destructive, because it assumes template N always corresponds to existing
// template N.
func RejectMixedTemplates(comp *Composition) field.ErrorList {
	named := 0
	for _, tmpl := range comp.Spec.Resources {
		if tmpl.Name != nil {
			named++
		} else {
			named--
		}
	}

	if l := len(comp.Spec.Resources); named == l || named == -l {
		// All templates are named or all templates are anonymous.
		return nil
	}

	return field.ErrorList{field.Invalid(field.NewPath("spec", "resources"), comp.Spec.Resources, errMixed)}
}

// RejectDuplicateNames validates that all template names are unique within the
// supplied Composition.
func RejectDuplicateNames(comp *Composition) (errs field.ErrorList) {
	seen := map[string]bool{}
	for i, tmpl := range comp.Spec.Resources {
		if tmpl.Name == nil {
			continue
		}
		if seen[*tmpl.Name] {
			errs = append(errs, field.Invalid(field.NewPath("spec", "resources").Index(i), tmpl.Name, errDuplicate))
			continue
		}
		seen[*tmpl.Name] = true
	}
	return errs
}

// RejectAnonymousTemplatesWithFunctions validates that all templates are named
// when Composition Functions are in use. This is necessary for the
// FunctionComposer to be able to associate entries in the spec.resources array
// with entries in a FunctionIO's observed and desired arrays.
func RejectAnonymousTemplatesWithFunctions(comp *Composition) (errs field.ErrorList) {
	if len(comp.Spec.Functions) == 0 {
		// Composition Functions do not appear to be in use.
		return nil
	}

	for i, tmpl := range comp.Spec.Resources {
		if tmpl.Name == nil {
			errs = append(errs, field.Invalid(field.NewPath("spec", "resources").Index(i), tmpl.Name, errFnsRequireNames))
		}
	}

	return errs
}

// TODO(negz): Ideally we'd apply the below pattern everywhere in our APIs, i.e.
// patches, transforms, etc. Currently each patch type (for example) ensures it
// has the required configuration at call time.

// RejectFunctionsWithoutRequiredConfig rejects Composition Functions missing
// the configuration for their type - for example a function of type: Container
// must include a container configuration.
func RejectFunctionsWithoutRequiredConfig(comp *Composition) (errs field.ErrorList) {
	for i, fn := range comp.Spec.Functions {
		if err := fn.Validate(); err != nil {
			errs = append(errs, field.Invalid(field.NewPath("spec", "functions").Index(i), fn, err.Error()))
		}
	}
	return errs
}
