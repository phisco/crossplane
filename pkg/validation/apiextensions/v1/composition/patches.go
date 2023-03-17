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
	"encoding/json"
	"fmt"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/pointer"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	schema2 "github.com/crossplane/crossplane/pkg/validation/schema"

	v1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
	"github.com/crossplane/crossplane/internal/controller/apiextensions/composition"
)

// validatePatchesWithSchemas validates the patches of a composition against the resources schemas.
func validatePatchesWithSchemas(comp *v1.Composition, gvkToCRD map[schema.GroupVersionKind]apiextensions.CustomResourceDefinition) (errs field.ErrorList) {
	// Let's first dereference patchSets
	resources, err := composition.ComposedTemplates(comp.Spec)
	if err != nil {
		errs = append(errs, field.Invalid(field.NewPath("spec", "resources"), comp.Spec.Resources, err.Error()))
		return errs
	}
	for i, resource := range resources {
		for j := range resource.Patches {
			if err := validatePatchWithSchemas(comp, i, j, gvkToCRD); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errs
}

// validatePatchWithSchemas validates a patch against the resources schemas.
func validatePatchWithSchemas( //nolint:gocyclo // TODO(phisco): refactor
	comp *v1.Composition,
	resourceNumber, patchNumber int,
	gvkToCRD map[schema.GroupVersionKind]apiextensions.CustomResourceDefinition,
) *field.Error {
	if len(comp.Spec.Resources) <= resourceNumber {
		return field.InternalError(field.NewPath("spec", "resource").Index(resourceNumber), errors.Errorf("cannot find resource"))
	}
	if len(comp.Spec.Resources[resourceNumber].Patches) <= patchNumber {
		return field.InternalError(field.NewPath("spec", "resource").Index(resourceNumber).Child("patches").Index(patchNumber), errors.Errorf("cannot find patch"))
	}
	resource := comp.Spec.Resources[resourceNumber]
	patch := resource.Patches[patchNumber]
	res, err := resource.GetBaseObject()
	if err != nil {
		return field.Invalid(field.NewPath("spec", "resource").Index(resourceNumber).Child("base"), resource.Base, err.Error())
	}

	// TODO(phisco): what about patch.Policy ?

	compositeCRD, compositeOK := gvkToCRD[schema.FromAPIVersionAndKind(
		comp.Spec.CompositeTypeRef.APIVersion,
		comp.Spec.CompositeTypeRef.Kind,
	)]
	if !compositeOK {
		return field.InternalError(field.NewPath("spec"), errors.Errorf("cannot find composite type %s", comp.Spec.CompositeTypeRef))
	}
	resourceCRD, resourceOK := gvkToCRD[res.GetObjectKind().GroupVersionKind()]
	if !resourceOK {
		return field.InternalError(field.NewPath("spec"), errors.Errorf("cannot find resource type %s", res.GetObjectKind().GroupVersionKind()))
	}

	var validationErr *field.Error
	var fromType, toType string
	switch patch.GetType() { //nolint:exhaustive // TODO implement other patch types
	case v1.PatchTypeFromCompositeFieldPath:
		fromType, toType, validationErr = ValidateFromCompositeFieldPathPatch(
			patch,
			compositeCRD.Spec.Validation.OpenAPIV3Schema,
			resourceCRD.Spec.Validation.OpenAPIV3Schema,
		)
	case v1.PatchTypeToCompositeFieldPath:
		fromType, toType, validationErr = ValidateFromCompositeFieldPathPatch(
			patch,
			resourceCRD.Spec.Validation.OpenAPIV3Schema,
			compositeCRD.Spec.Validation.OpenAPIV3Schema,
		)
	case v1.PatchTypeCombineFromComposite:
		fromType, toType, validationErr = ValidateCombineFromCompositePathPatch(
			patch,
			compositeCRD.Spec.Validation.OpenAPIV3Schema,
			resourceCRD.Spec.Validation.OpenAPIV3Schema)
	case v1.PatchTypeCombineToComposite:
		fromType, toType, validationErr = ValidateCombineFromCompositePathPatch(
			patch,
			resourceCRD.Spec.Validation.OpenAPIV3Schema,
			compositeCRD.Spec.Validation.OpenAPIV3Schema)
	}
	if validationErr != nil {
		return field.Invalid(field.NewPath(validationErr.Field, "spec", "resource").Index(resourceNumber).Child("patches").Index(patchNumber), tryJSONMarshal(patch), validationErr.Error())
	}

	return AddPath(
		field.NewPath("spec", "resource").Index(resourceNumber).Child("patches").Index(patchNumber),
		validateTransformsIOTypes(patch.Transforms, fromType, toType),
	)
}

// AddPath wraps an error in a field.Error if it is not nil, adding the given field path to the error as a prefix.
func AddPath(f *field.Path, err *field.Error) *field.Error {
	if err == nil {
		return nil
	}
	if f != nil {
		err.Field = f.Child(err.Field).String()
	}
	return err
}

func tryJSONMarshal(v any) string {
	b, err := json.Marshal(v)
	if err == nil {
		return string(b)
	}
	return fmt.Sprintf("%+v", v)
}

// ValidateCombineFromCompositePathPatch validates Combine Patch types, by going through and validating the fromField
// path variables, checking if they all need to be required, checking if the right combine strategy is set and
// validating transforms.
func ValidateCombineFromCompositePathPatch(
	patch v1.Patch,
	from *apiextensions.JSONSchemaProps,
	to *apiextensions.JSONSchemaProps,
) (fromType, toType string, err *field.Error) {
	fromRequired := true
	for _, variable := range patch.Combine.Variables {
		fromFieldPath := variable.FromFieldPath
		_, required, err := validateFieldPath(from, fromFieldPath)
		if err != nil {
			return "", "", field.Invalid(field.NewPath("fromFieldPath"), fromFieldPath, err.Error())
		}
		fromRequired = fromRequired && required
	}

	if patch.ToFieldPath == nil {
		return "", "", field.Required(field.NewPath("toFieldPath"), "ToFieldPath is required by type Combine")
	}

	toFieldPath := safeDeref(patch.ToFieldPath)
	toType, toRequired, toFieldPathErr := validateFieldPath(to, toFieldPath)
	if toFieldPathErr != nil {
		return "", "", field.Invalid(field.NewPath("toFieldPath"), toFieldPath, toFieldPathErr.Error())
	}

	if toRequired && !fromRequired {
		return "", "", field.Invalid(
			field.NewPath("combine"),
			patch.Combine.Variables,
			fmt.Sprintf("from field paths (%v) are not required but to field path is (%s)", patch.Combine.Variables, toFieldPath),
		)
	}

	switch patch.Combine.Strategy {
	case v1.CombineStrategyString:
		if patch.Combine.String == nil {
			return "", "", field.Required(field.NewPath("combine", "string"), "string combine strategy requires configuration")
		}
		fromType = string(schema2.StringKnownJSONType)
	default:
		return "", "", field.Invalid(field.NewPath("combine", "strategy"), patch.Combine.Strategy, "combine strategy is not supported")
	}

	// TODO(lsviben) check if we could validate the patch combine format

	return fromType, toType, nil
}

// ValidateFromCompositeFieldPathPatch validates a patch of type FromCompositeFieldPath.
func ValidateFromCompositeFieldPathPatch(patch v1.Patch, from, to *apiextensions.JSONSchemaProps) (fromType, toType string, res *field.Error) {
	fromFieldPath := safeDeref(patch.FromFieldPath)
	toFieldPath := safeDeref(patch.ToFieldPath)
	if toFieldPath == "" {
		toFieldPath = fromFieldPath
	}
	fromType, fromRequired, err := validateFieldPath(from, fromFieldPath)
	if err != nil {
		return "", "", field.Invalid(field.NewPath("fromFieldPath"), fromFieldPath, err.Error())
	}
	toType, toRequired, err := validateFieldPath(to, toFieldPath)
	if err != nil {
		return "", "", field.Invalid(field.NewPath("toFieldPath"), toFieldPath, err.Error())
	}
	if toRequired && !fromRequired {
		return "", "", field.Invalid(field.NewPath("fromFieldPath"), fromFieldPath, fmt.Sprintf(
			"from field path is not required but to field path is (%s)",
			toFieldPath,
		))
	}

	return fromType, toType, nil
}

func validateTransformsIOTypes(transforms []v1.Transform, fromType, toType string) *field.Error {
	var err error
	transformedToType := fromType
	for i, transform := range transforms {
		transformedToType, err = transform.ValidateIO(transformedToType)
		if err != nil {
			return field.Invalid(field.NewPath("transforms").Index(i), transforms, err.Error())
		}
	}

	if transformedToType == v1.TransformOutputTypeAny {
		return nil
	}

	// integer is a subset of number per JSON specification:
	// https://datatracker.ietf.org/doc/html/draft-zyp-json-schema-04#section-3.5
	if transformedToType == string(schema2.IntegerKnownJSONType) && toType == string(schema2.NumberKnownJSONType) {
		return nil
	}

	if transformedToType != toType {
		return field.Invalid(field.NewPath("transforms"), transforms, fmt.Sprintf("transformed output type and to field path have different types (%s != %s)", transformedToType, toType))
	}
	return nil
}

func safeDeref[T any](ptr *T) T {
	var zero T
	if ptr == nil {
		return zero
	}
	return *ptr
}

func validateFieldPath(schema *apiextensions.JSONSchemaProps, fieldPath string) (fieldType string, required bool, err error) {
	if fieldPath == "" {
		return "", false, nil
	}
	segments, err := fieldpath.Parse(fieldPath)
	if err != nil {
		return "", false, err
	}
	if len(segments) > 0 && segments[0].Type == fieldpath.SegmentField && segments[0].Field == "metadata" {
		segments = segments[1:]
		schema = &metadataSchema
	}
	current := schema
	for _, segment := range segments {
		var err error
		current, required, err = validateFieldPathSegment(current, segment)
		if err != nil {
			return "", false, err
		}
		if current == nil {
			return "", false, nil
		}
	}

	return current.Type, required, nil
}

// validateFieldPathSegment validates that the given field path segment is valid for the given schema.
// It returns the schema for the segment, whether the segment is required, and an error if the segment is invalid.
//
//nolint:gocyclo // TODO(phisco): refactor this function, add test cases
func validateFieldPathSegment(parent *apiextensions.JSONSchemaProps, segment fieldpath.Segment) (
	current *apiextensions.JSONSchemaProps,
	required bool,
	err error,
) {
	if parent == nil {
		return nil, false, nil
	}
	switch segment.Type {
	case fieldpath.SegmentField:
		propType := parent.Type
		if propType == "" {
			propType = string(schema2.ObjectKnownJSONType)
		}
		if propType != string(schema2.ObjectKnownJSONType) {
			return nil, false, errors.Errorf("trying to access field of not an object: %v", propType)
		}
		prop, exists := parent.Properties[segment.Field]
		if !exists {
			// TODO(phisco): handle x-kubernetes-preserve-unknown-fields
			if pointer.BoolDeref(parent.XPreserveUnknownFields, false) {
				return nil, false, nil
			}
			if parent.AdditionalProperties != nil && parent.AdditionalProperties.Allows {
				return parent.AdditionalProperties.Schema, false, nil
			}
			return nil, false, errors.Errorf("unable to find field: %s", segment.Field)
		}
		// TODO(lsviben): what about CEL?
		var required bool
		for _, req := range parent.Required {
			if req == segment.Field {
				required = true
				break
			}
		}
		return &prop, required, nil
	case fieldpath.SegmentIndex:
		if parent.Type != string(schema2.ArrayKnownJSONType) {
			return nil, false, errors.Errorf("accessing by index a %s field", parent.Type)
		}
		if parent.Items == nil {
			return nil, false, errors.New("no items found in array")
		}
		if s := parent.Items.Schema; s != nil {
			return s, false, nil
		}
		schemas := parent.Items.JSONSchemas
		if len(schemas) < int(segment.Index) {
			return nil, false, errors.Errorf("no schemas ")
		}

		// means there is no schema at all for this array
		return nil, false, nil
	}
	return nil, false, nil
}
