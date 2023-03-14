package validation

import (
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/pointer"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/composed"
	v1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
	"github.com/crossplane/crossplane/internal/controller/apiextensions/composition"
)

var (
	metadataSchema = apiextensions.JSONSchemaProps{
		Type: "object",
		AdditionalProperties: &apiextensions.JSONSchemaPropsOrBool{
			Allows: true,
		},
		Properties: map[string]apiextensions.JSONSchemaProps{
			"name": {
				Type: "string",
			},
			"namespace": {
				Type: "string",
			},
			"labels": {
				Type: "object",
				AdditionalProperties: &apiextensions.JSONSchemaPropsOrBool{
					Schema: &apiextensions.JSONSchemaProps{
						Type: "string",
					},
				},
			},
			"annotations": {
				Type: "object",
				AdditionalProperties: &apiextensions.JSONSchemaPropsOrBool{
					Schema: &apiextensions.JSONSchemaProps{
						Type: "string",
					},
				},
			},
			"uid": {
				Type: "string",
			},
		},
	}
)

// ValidatePatches validates the patches of a composition.
func ValidatePatches(comp *v1.Composition, gvkToCRD map[schema.GroupVersionKind]apiextensions.CustomResourceDefinition) (errs []error) {
	// Let's first dereference patchSets
	resources, err := composition.ComposedTemplates(comp.Spec)
	if err != nil {
		return []error{errors.Wrap(err, "cannot get composed templates")}
	}
	for _, resource := range resources {
		for _, patch := range resource.Patches {
			resource := resource
			if err := ValidatePatch(comp, &resource, patch, gvkToCRD); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errs
}

// ValidatePatch validates a patch.
func ValidatePatch(
	comp *v1.Composition,
	resource *v1.ComposedTemplate,
	patch v1.Patch,
	gvkToCRD map[schema.GroupVersionKind]apiextensions.CustomResourceDefinition,
) error {
	res, err := composed.ParseToUnstructured(resource.Base.Raw)
	if err != nil {
		return err
	}
	if err := patch.Validate(); err != nil {
		return err
	}
	switch patch.GetType() { //nolint:exhaustive // TODO implement other patch types
	case v1.PatchTypeFromCompositeFieldPath:
		return ValidateFromCompositeFieldPathPatch(
			patch,
			gvkToCRD[schema.FromAPIVersionAndKind(
				comp.Spec.CompositeTypeRef.APIVersion,
				comp.Spec.CompositeTypeRef.Kind,
			)].Spec.Validation.OpenAPIV3Schema,
			gvkToCRD[schema.FromAPIVersionAndKind(
				res.GetAPIVersion(),
				res.GetKind(),
			)].Spec.Validation.OpenAPIV3Schema,
		)
	case v1.PatchTypePatchSet:
		// already handled
		return nil
	}
	return nil
}

// ValidateFromCompositeFieldPathPatch validates a patch of type FromCompositeFieldPath.
//

func ValidateFromCompositeFieldPathPatch(patch v1.Patch, from, to *apiextensions.JSONSchemaProps) error {
	fromFieldPath := safeDeref(patch.FromFieldPath)
	toFieldPath := safeDeref(patch.ToFieldPath)
	if toFieldPath == "" {
		toFieldPath = fromFieldPath
	}
	fromType, fromRequired, err := validateFieldPath(from, fromFieldPath)
	if err != nil {
		return err
	}
	toType, toRequired, err := validateFieldPath(to, toFieldPath)
	if err != nil {
		return err
	}
	if toRequired && !fromRequired {
		return errors.Errorf("from field path (%s) is not required but to field path is (%s)", fromFieldPath, toFieldPath)
	}
	if len(patch.Transforms) == 0 && fromType == toType {
		return nil
	}

	if err := validateTransforms(patch.Transforms, fromType, toType); err != nil {
		return errors.Wrapf(err, "cannot validate transforms for patch from field path (%s) to field path (%s)", fromFieldPath, toFieldPath)
	}

	return nil
}

func validateTransforms(transforms []v1.Transform, fromType, toType string) (err error) {
	transformedToType := fromType
	for _, transform := range transforms {
		err = composition.ValidateTransform(transform, transformedToType)
		if err != nil {
			return err
		}
		transformedToType = composition.TransformOutputType(transform)
	}
	// TODO(phisco): handle "" types
	if transformedToType == "any" || transformedToType == "" {
		return nil
	}
	if transformedToType != toType {
		return errors.Errorf("from field path and to field path have different types (%s != %s) and transforms do not resolve to the same type", fromType, toType)
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
//nolint:gocyclo // TODO(phisco): refactor this function
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
			propType = "object"
		}
		if propType != "object" {
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
		var required bool
		for _, req := range parent.Required {
			if req == segment.Field {
				required = true
				break
			}
		}
		return &prop, required, nil
	case fieldpath.SegmentIndex:
		if parent.Type != "array" {
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
