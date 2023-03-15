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

	compositeCRD, compositeOK := gvkToCRD[schema.FromAPIVersionAndKind(
		comp.Spec.CompositeTypeRef.APIVersion,
		comp.Spec.CompositeTypeRef.Kind,
	)]
	if !compositeOK {
		return errors.Errorf("cannot find composite type %s", comp.Spec.CompositeTypeRef)
	}
	resourceCRD, resourceOK := gvkToCRD[schema.FromAPIVersionAndKind(
		res.GetAPIVersion(),
		res.GetKind(),
	)]
	if !resourceOK {
		return errors.Errorf("cannot find resource type %s", res.GroupVersionKind())
	}

	switch patch.GetType() { //nolint:exhaustive // TODO implement other patch types
	case v1.PatchTypeFromCompositeFieldPath:
		return ValidateFromCompositeFieldPathPatch(
			patch,
			compositeCRD.Spec.Validation.OpenAPIV3Schema,
			resourceCRD.Spec.Validation.OpenAPIV3Schema,
		)
	case v1.PatchTypeToCompositeFieldPath:
		return ValidateFromCompositeFieldPathPatch(
			patch,
			resourceCRD.Spec.Validation.OpenAPIV3Schema,
			compositeCRD.Spec.Validation.OpenAPIV3Schema,
		)
	case v1.PatchTypeCombineFromComposite:
		return ValidateCombineFromCompositePathPatch(
			patch,
			compositeCRD.Spec.Validation.OpenAPIV3Schema,
			resourceCRD.Spec.Validation.OpenAPIV3Schema)
	case v1.PatchTypeCombineToComposite:
		return ValidateCombineFromCompositePathPatch(
			patch,
			resourceCRD.Spec.Validation.OpenAPIV3Schema,
			compositeCRD.Spec.Validation.OpenAPIV3Schema)
	}
	return nil
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
		fromType = "string"
	default:
		return errors.Errorf("combine strategy %s is not supported", patch.Combine.Strategy)
	}

	// TODO(lsviben) check if we could validate the patch combine format

	if err := validateTransforms(patch.Transforms, fromType, toType); err != nil {
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
		return err
	}
	toType, toRequired, err := validateFieldPath(to, toFieldPath)
	if err != nil {
		return err
	}
	if toRequired && !fromRequired {
		return errors.Errorf("from field path (%s) is not required but to field path is (%s)", fromFieldPath, toFieldPath)
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
		transformedToType, err = composition.TransformOutputType(transform)
		if err != nil {
			return err
		}
	}

	if transformedToType == "any" {
		return nil
	}

	// integer is a subset of number per JSON specification:
	// https://datatracker.ietf.org/doc/html/draft-zyp-json-schema-04#section-3.5
	if transformedToType == "integer" && toType == "number" {
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
