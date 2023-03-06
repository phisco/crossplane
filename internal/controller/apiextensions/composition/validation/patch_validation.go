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

package validation

import (
	"fmt"
	xprerrors "github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	v1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/pointer"
)

// PatchValidationRequest is the context for validating a patch.
type PatchValidationRequest struct {
	// CompositionValidationMode is the validation mode for the composition.
	CompositionValidationMode v1.CompositionValidationMode

	// GVKValidationMap is a map of GVK to CRD validation.
	GVKCRDValidation GVKValidationMap

	// CompositeGVK is the GVK of the composite resource.
	CompositeGVK schema.GroupVersionKind

	// ComposedGVK is the GVK of the composed resource.
	ComposedGVK schema.GroupVersionKind
}

// IsValidatablePatchType returns true if the patch type is supported for validation.
func IsValidatablePatchType(patch *v1.Patch) bool {
	switch patch.Type {
	case v1.PatchTypeToEnvironmentFieldPath, v1.PatchTypeFromEnvironmentFieldPath,
		v1.PatchTypeCombineToEnvironment, v1.PatchTypeCombineFromEnvironment,
		v1.PatchTypeCombineToComposite, v1.PatchTypeCombineFromComposite,
		v1.PatchTypeToCompositeFieldPath:
		return false
	case v1.PatchTypeFromCompositeFieldPath, v1.PatchTypePatchSet:
	}
	return true
}

// ValidatePatch validates the patch according to each patch type, if supported.
func ValidatePatch(patch v1.Patch, patchContext *PatchValidationRequest) (err error) {
	if !IsValidatablePatchType(&patch) {
		return nil
	}
	switch patch.Type {
	case v1.PatchTypeFromCompositeFieldPath:
		err = ValidateFromCompositeFieldPathPatch(patch, patchContext)
	case v1.PatchTypeCombineFromComposite:
		// TODO: implement
		// err = validateCombineFromCompositePatch(patch, PatchValidationRequest)
	case v1.PatchTypeFromEnvironmentFieldPath:
		// TODO: implement
		// err = validateFromEnvironmentFieldPathPatch(patch, PatchValidationRequest)
	case v1.PatchTypeCombineFromEnvironment:
		// TODO: implement
		// err = validateCombineFromEnvironmentPatch(patch, PatchValidationRequest)
	case v1.PatchTypeToCompositeFieldPath:
		// TODO: implement
		// err = validateToCompositeFieldPathPatch(patch, PatchValidationRequest)
	case v1.PatchTypeToEnvironmentFieldPath:
		// TODO: implement
		// err = validateToEnvironmentFieldPathPatch(patch, PatchValidationRequest)
	case v1.PatchTypeCombineToComposite:
		// TODO: implement
		// err = validateCombineToCompositePatch(patch, PatchValidationRequest)
	case v1.PatchTypeCombineToEnvironment:
		// TODO: implement
		// err = validateCombineToEnvironmentPatch(patch, PatchValidationRequest)
	case v1.PatchTypePatchSet:
		// do nothing
	}
	if err != nil {
		return err
	}
	return nil
}

// ValidateFromCompositeFieldPathPatch validates the patch type FromCompositeFieldPath.
func ValidateFromCompositeFieldPathPatch(patch v1.Patch, req *PatchValidationRequest) error {
	if len(patch.Transforms) > 0 {
		return nil
	}
	if patch.Type != v1.PatchTypeFromCompositeFieldPath {
		return xprerrors.Errorf("invalid patch type: %v", patch.Type)
	}
	compositeValidation, okCompositeValidation := req.GVKCRDValidation[req.CompositeGVK]
	if !okCompositeValidation && req.CompositionValidationMode == v1.CompositionValidationModeStrict {
		return xprerrors.Errorf("no validation found for composite resource: %v", req.CompositeGVK)
	}
	composedValidation, okComposedValidation := req.GVKCRDValidation[req.ComposedGVK]
	if !okComposedValidation && req.CompositionValidationMode == v1.CompositionValidationModeStrict {
		return xprerrors.Errorf("no validation found for composed resource: %v", req.ComposedGVK)
	}
	if !okCompositeValidation && !okComposedValidation {
		// not much we can check if we don't have schemas
		return nil
	}
	var compositeFieldpathType, composedFieldpathType string
	var requiredComposite, requiredComposed bool
	var err error
	if okCompositeValidation {
		compositeFieldpathType, requiredComposite, err = validateFieldPath(patch.FromFieldPath, compositeValidation.OpenAPIV3Schema)
		if err != nil {
			return xprerrors.Wrapf(err, "invalid fromFieldPath: %s", *patch.FromFieldPath)
		}
	}
	if okComposedValidation {
		composedFieldpathType, requiredComposed, err = validateFieldPath(patch.ToFieldPath, composedValidation.OpenAPIV3Schema)
		if err != nil {
			return xprerrors.Wrapf(err, "invalid toFieldPath: %s", *patch.ToFieldPath)
		}
		// TODO: transform can change the value type of the field path, so we should
		// validate the type of the field path after the transform is applied.
	}
	if !okCompositeValidation || !okComposedValidation {
		fmt.Println("WARNING: skipping validation of patch because one or more schemas are missing")
		// we can not compare types or requirements if we don't have both schemas
		return nil
	}
	if requiredComposed && !requiredComposite {
		return xprerrors.Errorf("field path is not required in composite resource '%s' but is required for composed resource '%s'", *patch.FromFieldPath, *patch.ToFieldPath)
	}
	if compositeFieldpathType != "" && composedFieldpathType != "" && compositeFieldpathType != composedFieldpathType {
		return xprerrors.Errorf("field path types do not match: %s, %s", compositeFieldpathType, composedFieldpathType)
	}
	return nil
}

// validateFieldPath validates that the given field path is valid for the given schema.
// It returns the type of the field path if it is valid, or an error otherwise.
func validateFieldPath(path *string, s *apiextensions.JSONSchemaProps) (fieldType string, required bool, err error) {
	if path == nil {
		return "", false, fmt.Errorf("no field path provided")
	}
	segments, err := fieldpath.Parse(*path)
	if err != nil {
		return "", false, err
	}
	if len(segments) > 0 && segments[0].Type == fieldpath.SegmentField && segments[0].Field == "metadata" {
		segments = segments[1:]
		s = &metadataSchema
	}
	current := s
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
// It returns the schema of the field path segment if it is valid, or an error otherwise.
func validateFieldPathSegment(current *apiextensions.JSONSchemaProps, segment fieldpath.Segment) (*apiextensions.JSONSchemaProps, bool, error) {
	if current == nil {
		return nil, false, nil
	}
	switch segment.Type {
	case fieldpath.SegmentField:
		propType := current.Type
		if propType == "" {
			propType = "object"
		}
		if propType != "object" {
			return nil, false, xprerrors.Errorf("trying to access field of not an object: %v", propType)
		}
		prop, exists := current.Properties[segment.Field]
		if !exists {
			if pointer.BoolDeref(current.XPreserveUnknownFields, false) {
				return nil, false, nil
			}
			if current.AdditionalProperties != nil && current.AdditionalProperties.Allows {
				return current.AdditionalProperties.Schema, false, nil
			}
			return nil, false, xprerrors.Errorf("unable to find field: %s", segment.Field)
		}
		var required bool
		for _, req := range current.Required {
			if req == segment.Field {
				required = true
				break
			}
		}
		return &prop, required, nil
	case fieldpath.SegmentIndex:
		if current.Type != "array" {
			return nil, false, xprerrors.Errorf("accessing by index a %s field", current.Type)
		}
		if current.Items == nil {
			return nil, false, xprerrors.New("no items found in array")
		}
		if s := current.Items.Schema; s != nil {
			return s, false, nil
		}
		schemas := current.Items.JSONSchemas
		if len(schemas) < int(segment.Index) {
			return nil, false, xprerrors.Errorf("no schemas ")
		}

		// means there is no schema at all for this array
		return nil, false, nil
	}
	return nil, false, nil
}
