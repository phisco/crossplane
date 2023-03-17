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

	v1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
	"github.com/crossplane/crossplane/internal/controller/apiextensions/composition"
)

type KnownJSONTypes string

const (
	ArrayKnownJSONType   KnownJSONTypes = "array"
	BooleanKnownJSONType KnownJSONTypes = "boolean"
	IntegerKnownJSONType KnownJSONTypes = "integer"
	NullKnownJSONType    KnownJSONTypes = "null"
	NumberKnownJSONType  KnownJSONTypes = "number"
	ObjectKnownJSONType  KnownJSONTypes = "object"
	StringKnownJSONType  KnownJSONTypes = "string"
)

var (
	metadataSchema = apiextensions.JSONSchemaProps{
		Type: string(ObjectKnownJSONType),
		AdditionalProperties: &apiextensions.JSONSchemaPropsOrBool{
			Allows: true,
		},
		Properties: map[string]apiextensions.JSONSchemaProps{
			"name": {
				Type: string(StringKnownJSONType),
			},
			"namespace": {
				Type: string(StringKnownJSONType),
			},
			"labels": {
				Type: string(ObjectKnownJSONType),
				AdditionalProperties: &apiextensions.JSONSchemaPropsOrBool{
					Schema: &apiextensions.JSONSchemaProps{
						Type: string(StringKnownJSONType),
					},
				},
			},
			"annotations": {
				Type: string(ObjectKnownJSONType),
				AdditionalProperties: &apiextensions.JSONSchemaPropsOrBool{
					Schema: &apiextensions.JSONSchemaProps{
						Type: string(StringKnownJSONType),
					},
				},
			},
			"uid": {
				Type: string(StringKnownJSONType),
			},
		},
	}
)

// ValidatePatchesWithSchemas validates the patches of a composition against the resources schemas.
func ValidatePatchesWithSchemas(comp *v1.Composition, gvkToCRD map[schema.GroupVersionKind]apiextensions.CustomResourceDefinition) (errs field.ErrorList) {
	// Let's first dereference patchSets
	resources, err := composition.ComposedTemplates(comp.Spec)
	if err != nil {
		errs = append(errs, field.Invalid(field.NewPath("spec", "resources"), comp.Spec.Resources, err.Error()))
		return errs
	}
	for i, resource := range resources {
		for j := range resource.Patches {
			if err := ValidatePatchWithSchemas(comp, i, j, gvkToCRD); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errs
}

// ValidatePatchWithSchemas validates a patch against the resources schemas.
func ValidatePatchWithSchemas( //nolint:gocyclo // TODO(phisco): refactor
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

	var validationErr error
	switch patch.GetType() { //nolint:exhaustive // TODO implement other patch types
	// TODO return fromType toType and validate transforms in one place
	case v1.PatchTypeFromCompositeFieldPath:
		validationErr = ValidateFromCompositeFieldPathPatch(
			patch,
			compositeCRD.Spec.Validation.OpenAPIV3Schema,
			resourceCRD.Spec.Validation.OpenAPIV3Schema,
		)
	case v1.PatchTypeToCompositeFieldPath:
		validationErr = ValidateFromCompositeFieldPathPatch(
			patch,
			resourceCRD.Spec.Validation.OpenAPIV3Schema,
			compositeCRD.Spec.Validation.OpenAPIV3Schema,
		)
	case v1.PatchTypeCombineFromComposite:
		validationErr = ValidateCombineFromCompositePathPatch(
			patch,
			compositeCRD.Spec.Validation.OpenAPIV3Schema,
			resourceCRD.Spec.Validation.OpenAPIV3Schema)
	case v1.PatchTypeCombineToComposite:
		validationErr = ValidateCombineFromCompositePathPatch(
			patch,
			resourceCRD.Spec.Validation.OpenAPIV3Schema,
			compositeCRD.Spec.Validation.OpenAPIV3Schema)
	}
	if validationErr != nil {
		return field.Invalid(field.NewPath("spec", "resource").Index(resourceNumber).Child("patches").Index(patchNumber), tryJSONMarshal(patch), validationErr.Error())
	}
	return nil
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
//
//nolint:gocyclo // TODO refactor it a bit, its just over the limit
func ValidateCombineFromCompositePathPatch(
	patch v1.Patch,
	from *apiextensions.JSONSchemaProps,
	to *apiextensions.JSONSchemaProps,
) error {
	fromRequired := true
	for _, variable := range patch.Combine.Variables {
		fromFieldPath := variable.FromFieldPath
		_, required, err := validateFieldPath(from, fromFieldPath)
		if err != nil {
			return err
		}
		fromRequired = fromRequired && required
	}

	if patch.ToFieldPath == nil {
		return errors.Errorf("%s is required by type %s", "ToFieldPath", patch.Type)
	}

	toFieldPath := safeDeref(patch.ToFieldPath)
	toType, toRequired, err := validateFieldPath(to, toFieldPath)
	if err != nil {
		return err
	}

	if toRequired && !fromRequired {
		return errors.Errorf("from field paths (%v) are not required but to field path is (%s)",
			patch.Combine.Variables, toFieldPath)
	}

	var fromType string
	switch patch.Combine.Strategy {
	case v1.CombineStrategyString:
		if patch.Combine.String == nil {
			return errors.Errorf("given combine strategy %s requires configuration", patch.Combine.Strategy)
		}
		fromType = string(StringKnownJSONType)
	default:
		return errors.Errorf("combine strategy %s is not supported", patch.Combine.Strategy)
	}

	// TODO(lsviben) check if we could validate the patch combine format

	if err := validateTransformsIOTypes(patch.Transforms, fromType, toType); err != nil {
		return errors.Wrapf(
			err,
			"cannot validate transforms for patch from field paths (%v) to field path (%s)",
			patch.Combine.Variables,
			toFieldPath,
		)
	}

	return nil
}

// ValidateFromCompositeFieldPathPatch validates a patch of type FromCompositeFieldPath.
func ValidateFromCompositeFieldPathPatch(patch v1.Patch, from, to *apiextensions.JSONSchemaProps) error {
	fromFieldPath := safeDeref(patch.FromFieldPath)
	toFieldPath := safeDeref(patch.ToFieldPath)
	if toFieldPath == "" {
		toFieldPath = fromFieldPath
	}
	fromType, fromRequired, err := validateFieldPath(from, fromFieldPath)
	if err != nil {
		return field.Invalid(field.NewPath("fromFieldPath"), fromFieldPath, err.Error())
	}
	toType, toRequired, err := validateFieldPath(to, toFieldPath)
	if err != nil {
		return err
	}
	if toRequired && !fromRequired {
		return errors.Errorf("from field path (%s) is not required but to field path is (%s)", fromFieldPath, toFieldPath)
	}

	if err := validateTransformsIOTypes(patch.Transforms, fromType, toType); err != nil {
		return errors.Wrapf(err, "cannot validate transforms for patch from field path (%s) to field path (%s)", fromFieldPath, toFieldPath)
	}

	return nil
}

func validateTransformsIOTypes(transforms []v1.Transform, fromType, toType string) (err error) {
	transformedToType := fromType
	for _, transform := range transforms {
		transformedToType, err = composition.ValidateTransformIOTypes(transform, transformedToType)
		if err != nil {
			return field.Invalid(field.NewPath("transforms"), transforms, err.Error())
		}
	}

	if transformedToType == composition.TransformOutputTypeAny {
		return nil
	}

	// integer is a subset of number per JSON specification:
	// https://datatracker.ietf.org/doc/html/draft-zyp-json-schema-04#section-3.5
	if transformedToType == string(BooleanKnownJSONType) && toType == string(NumberKnownJSONType) {
		return nil
	}

	if transformedToType != toType {
		return errors.Errorf("transformed output type and to field path have different types (%s != %s)", transformedToType, toType)
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
			propType = string(ObjectKnownJSONType)
		}
		if propType != string(ObjectKnownJSONType) {
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
		if parent.Type != string(ArrayKnownJSONType) {
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
