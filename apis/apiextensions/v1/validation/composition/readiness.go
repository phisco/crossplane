/*
Copyright 2023 the Crossplane Authors.

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

package composition

import (
	"fmt"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/crossplane/crossplane-runtime/pkg/errors"

	v1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
)

// ValidateReadinessCheck validates the readiness check of a composition, given the CRDs of the composed resources.
// It checks that the readiness check field path is valid and that the fields required for the readiness check type are set and valid.
func ValidateReadinessCheck( //nolint:gocyclo // TODO(lsviben): refactor
	comp *v1.Composition,
	gvkToCRD map[schema.GroupVersionKind]apiextensions.CustomResourceDefinition,
) (errs field.ErrorList) {
	for i, resource := range comp.Spec.Resources {
		gvk, err := resource.GetObjectGVK()
		if err != nil {
			return append(errs, field.InternalError(field.NewPath("spec", "resource").Index(i).Child("base"), errors.Wrap(err, "cannot get object gvk")))
		}
		crd, ok := gvkToCRD[gvk]
		if !ok {
			return append(errs, field.InternalError(
				field.NewPath("spec", "resource").Index(i).Child("base"),
				fmt.Errorf("crd for gvk %q not found", gvk),
			))
		}
		for j, r := range resource.ReadinessChecks {
			if err := r.Validate(); err != nil {
				errs = append(errs, field.Invalid(field.NewPath("spec", "resource").Index(i).Child("base").Child("readinessCheck").Index(j), r, err.Error()))
				continue
			}

			matchType := ""
			switch r.Type {
			case v1.ReadinessCheckTypeNone:
				continue
			// NOTE: ComposedTemplate doesn't use pointer values for optional
			// strings, so today the empty string and 0 are equivalent to "unset".
			case v1.ReadinessCheckTypeMatchString:
				matchType = "string"
			case v1.ReadinessCheckTypeMatchInteger:
				matchType = "integer"
			case v1.ReadinessCheckTypeNonEmpty:
			}
			fieldType, _, err := validateFieldPath(crd.Spec.Validation.OpenAPIV3Schema, r.FieldPath)
			if err != nil {
				errs = append(errs, field.Invalid(field.NewPath("spec", "resource").Index(i).Child("base").Child("readinessCheck").Index(j).Child("fieldPath"), r.FieldPath, err.Error()))
				continue
			}
			if matchType != "" && matchType != fieldType {
				errs = append(errs, field.Invalid(field.NewPath("spec", "resource").Index(i).Child("base").Child("readinessCheck").Index(j).Child("fieldPath"), r.FieldPath, fmt.Sprintf("expected field path to be of type %s", matchType)))
			}
		}
	}

	return errs
}
