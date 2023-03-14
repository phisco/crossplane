package validation

import (
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/composed"
	v1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/pointer"
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
func ValidatePatches(comp *v1.Composition, gvkToCRD map[schema.GroupVersionKind]apiextensions.CustomResourceDefinition) []error {
	var errs []error
	for _, resource := range comp.Spec.Resources {
		for _, patch := range resource.Patches {
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
	switch patch.Type {
	//TODO implement other patch types
	case v1.PatchTypeFromCompositeFieldPath, "":
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
	default:
		return nil
	}
	return nil
}

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
	if fromType == toType {
		return nil
	}
	if len(patch.Transforms) == 0 {
		return errors.Errorf("from field path (%s) and to field path (%s) have different types (%s != %s) and no transforms are provided", fromFieldPath, toFieldPath, fromType, toType)
	}

	// TODO handle transforms

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
			return nil, false, errors.Errorf("trying to access field of not an object: %v", propType)
		}
		prop, exists := current.Properties[segment.Field]
		if !exists {
			if pointer.BoolDeref(current.XPreserveUnknownFields, false) {
				return nil, false, nil
			}
			if current.AdditionalProperties != nil && current.AdditionalProperties.Allows {
				return current.AdditionalProperties.Schema, false, nil
			}
			return nil, false, errors.Errorf("unable to find field: %s", segment.Field)
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
			return nil, false, errors.Errorf("accessing by index a %s field", current.Type)
		}
		if current.Items == nil {
			return nil, false, errors.New("no items found in array")
		}
		if s := current.Items.Schema; s != nil {
			return s, false, nil
		}
		schemas := current.Items.JSONSchemas
		if len(schemas) < int(segment.Index) {
			return nil, false, errors.Errorf("no schemas ")
		}

		// means there is no schema at all for this array
		return nil, false, nil
	}
	return nil, false, nil
}
