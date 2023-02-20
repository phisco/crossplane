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

package validation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	xprerrors "github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/composed"
	composite2 "github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/composite"
	"github.com/crossplane/crossplane/apis/apiextensions/v1"
	"github.com/crossplane/crossplane/internal/controller/apiextensions/composite"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Error strings
const (
	errMixed                    = "cannot mix named and anonymous resource templates - ensure all resource templates are named"
	errDuplicate                = "resource template names must be unique within their Composition"
	errFnsRequireNames          = "cannot use functions with anonymous resource templates - ensure all resource templates are named"
	errFnMissingContainerConfig = "functions of type: Container must specify container configuration"
	errUnexpectedType           = "unexpected type"

	errFmtUnknownFnType = "unknown function type %q"
)

var (
	defaultCompositionValidationChain = ValidationChain{
		CompositionValidatorFn(RejectMixedTemplates),
		CompositionValidatorFn(RejectDuplicateNames),
		CompositionValidatorFn(RejectAnonymousTemplatesWithFunctions),
		CompositionValidatorFn(RejectFunctionsWithoutRequiredConfig),
	}

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

func GetDefaultCompositionValidationChain() ValidationChain {
	return defaultCompositionValidationChain
}

// A CompositionValidatorInterface validates the supplied Composition.
type CompositionValidatorInterface interface {
	Validate(comp *v1.Composition) error
}

// A CompositionValidatorFn validates the supplied Composition.
type CompositionValidatorFn func(comp *v1.Composition) error

// GVKValidationMap is a map of GVK to a CustomResourceValidation.
type GVKValidationMap map[schema.GroupVersionKind]apiextensions.CustomResourceValidation

// Validate the supplied Composition.
func (fn CompositionValidatorFn) Validate(comp *v1.Composition) error {
	return fn(comp)
}

// A ValidationChain runs multiple validations.
type ValidationChain []CompositionValidatorInterface

// Validate the supplied Composition.
func (vs ValidationChain) Validate(comp *v1.Composition) error {
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
func RejectMixedTemplates(comp *v1.Composition) error {
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

	return xprerrors.New(errMixed)
}

// RejectDuplicateNames validates that all template names are unique within the
// supplied Composition.
func RejectDuplicateNames(comp *v1.Composition) error {
	seen := map[string]bool{}
	for _, tmpl := range comp.Spec.Resources {
		if tmpl.Name == nil {
			continue
		}
		if seen[*tmpl.Name] {
			return xprerrors.New(errDuplicate)
		}
		seen[*tmpl.Name] = true
	}
	return nil
}

// RejectAnonymousTemplatesWithFunctions validates that all templates are named
// when Composition Functions are in use. This is necessary for the
// FunctionComposer to be able to associate entries in the spec.resources array
// with entries in a FunctionIO's observed and desired arrays.
func RejectAnonymousTemplatesWithFunctions(comp *v1.Composition) error {
	if len(comp.Spec.Functions) == 0 {
		// Composition Functions do not appear to be in use.
		return nil
	}

	for _, tmpl := range comp.Spec.Resources {
		if tmpl.Name == nil {
			return xprerrors.New(errFnsRequireNames)
		}
	}

	return nil
}

// TODO(negz): Ideally we'd apply the below pattern everywhere in our APIs, i.e.
// patches, transforms, etc. Currently each patch type (for example) ensures it
// has the required configuration at call time.

// RejectFunctionsWithoutRequiredConfig rejects Composition Functions missing
// the configuration for their type - for example a function of type: Container
// must include a container configuration.
func RejectFunctionsWithoutRequiredConfig(comp *v1.Composition) error {
	for _, fn := range comp.Spec.Functions {
		switch fn.Type {
		case v1.FunctionTypeContainer:
			if fn.Container == nil {
				return xprerrors.New(errFnMissingContainerConfig)
			}
		default:
			return xprerrors.Errorf(errFmtUnknownFnType, fn.Type)
		}
	}
	return nil
}

type ClientCompositionValidator struct {
	client                 client.Client
	renderer               composite.Renderer
	logicalValidationChain ValidationChain
}

func (c *ClientCompositionValidator) SetupWithManager(mgr ctrl.Manager) error {
	indexer := mgr.GetFieldIndexer()
	if err := indexer.IndexField(context.Background(), &extv1.CustomResourceDefinition{}, "spec.group", func(obj client.Object) []string {
		return []string{obj.(*extv1.CustomResourceDefinition).Spec.Group}
	}); err != nil {
		return err
	}
	if err := indexer.IndexField(context.Background(), &extv1.CustomResourceDefinition{}, "spec.names.kind", func(obj client.Object) []string {
		return []string{obj.(*extv1.CustomResourceDefinition).Spec.Names.Kind}
	}); err != nil {
		return err
	}
	c.client = unstructured.NewClient(mgr.GetClient())
	c.renderer = composite.NewPureRenderer()
	c.logicalValidationChain = GetDefaultCompositionValidationChain()
	return ctrl.NewWebhookManagedBy(mgr).
		WithValidator(c).
		For(&v1.Composition{}).
		Complete()
}

func (c *ClientCompositionValidator) ValidateCreate(ctx context.Context, obj runtime.Object) error {
	comp, ok := obj.(*v1.Composition)
	if !ok {
		return xprerrors.New(errUnexpectedType)
	}

	if err := IsValidatable(comp); err != nil {
		fmt.Println("HERE: Composition is not validatable", err)
		return nil
	}

	// Get the validation mode set through annotations for the composition
	validationMode, err := getCompositionValidationMode(comp)
	if err != nil {
		return err
	}

	// Get schema for Composite Resource Definition defined by comp.Spec.CompositeTypeRef
	compositeResGVK := schema.FromAPIVersionAndKind(comp.Spec.CompositeTypeRef.APIVersion,
		comp.Spec.CompositeTypeRef.Kind)

	// Get schema for
	compositeCrdValidation, err := c.getCRDValidationForGVK(ctx, &compositeResGVK, validationMode)
	if err != nil {
		return err
	}
	// Get schema for all Managed Resources in comp.Spec.Resources[*].Base
	managedResourcesCRDs, err := c.getBasesCRDs(ctx, comp.Spec.Resources, validationMode)
	if err != nil {
		return err
	}
	if compositeCrdValidation != nil {
		managedResourcesCRDs[compositeResGVK] = *compositeCrdValidation
	}

	// dereference all patches first
	resources, err := composite.ComposedTemplates(comp.Spec)
	if err != nil {
		return err
	}

	validationChain := c.logicalValidationChain
	// Validate general assertions
	if err := validationChain.Validate(comp); err != nil {
		return err
	}

	// Create a composite resource to validate patches against, setting all required fields
	compositeRes := composite2.New(composite2.WithGroupVersionKind(compositeResGVK))
	compositeRes.SetUID("validation-uid")
	compositeRes.SetName("validation-name")
	composite.NewPureAPINamingConfigurator().Configure(ctx, compositeRes, nil)

	composedResources := make([]runtime.Object, len(resources))
	var patchingErr error
	// Validate all patches given the schemas above
	for i, resource := range resources {
		// validate patches using it and the compositeCrd resource
		cd := composed.New()
		if err := json.Unmarshal(resource.Base.Raw, cd); err != nil {
			patchingErr = errors.Join(patchingErr, fmt.Errorf("resource %s (%d): %w", *resource.Name, i, err))
			continue
		}
		composedGVK := cd.GetObjectKind().GroupVersionKind()
		patchCtx := PatchValidationContext{
			GVKCRDValidation:          managedResourcesCRDs,
			CompositionValidationMode: validationMode,
			ComposedGVK:               composedGVK,
			CompositeGVK:              compositeResGVK,
		}
		for j, patch := range resource.Patches {
			if err := ValidatePatch(patch, &patchCtx); err != nil {
				patchingErr = errors.Join(patchingErr, fmt.Errorf("resource %s (%d), patch %d: %w", *resource.Name, i, j, err))
				continue
			}
		}

		// TODO: handle env too
		if err := c.renderer.Render(ctx, compositeRes, cd, resource, nil); err != nil {
			patchingErr = errors.Join(patchingErr, err)
			continue
		}
		composedResources[i] = cd
	}

	if patchingErr != nil {
		return apierrors.NewBadRequest(errors.Join(errors.New("invalid composition"), patchingErr).Error())
	}

	var renderError error
	// Validate Rendered Composed Resources from Composition
	for _, renderedComposed := range composedResources {
		crdV, ok := managedResourcesCRDs[renderedComposed.GetObjectKind().GroupVersionKind()]
		if !ok {
			if validationMode == v1.CompositionValidationModeStrict {
				renderError = errors.Join(renderError, xprerrors.Errorf("No CRD validation found for rendered resource: %v", renderedComposed.GetObjectKind().GroupVersionKind()))
				continue
			}
			continue
		}
		vs, _, err := validation.NewSchemaValidator(&crdV)
		if err != nil {
			return err
		}
		r := vs.Validate(renderedComposed)
		if r.HasErrors() {
			renderError = errors.Join(renderError, errors.Join(r.Errors...))
		}
		// TODO: handle warnings
	}

	if renderError != nil {
		return apierrors.NewBadRequest(errors.Join(errors.New("invalid composition"), renderError).Error())
	}

	return nil
}

func getCompositionValidationMode(comp *v1.Composition) (v1.CompositionValidationMode, error) {
	if comp.Annotations == nil {
		return v1.DefaultCompositionValidationMode, nil
	}

	mode, ok := comp.Annotations[v1.CompositionValidationModeAnnotation]
	if !ok {
		return v1.DefaultCompositionValidationMode, nil
	}

	switch mode := v1.CompositionValidationMode(mode); mode {
	case v1.CompositionValidationModeStrict, v1.CompositionValidationModeLoose:
		return mode, nil
	}
	return "", xprerrors.Errorf("invalid composition validation mode: %s", mode)
}

func (c *ClientCompositionValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) error {
	return c.ValidateCreate(ctx, newObj)
}

func (c *ClientCompositionValidator) ValidateDelete(ctx context.Context, obj runtime.Object) error {
	return nil
}

func (c *ClientCompositionValidator) getCRDValidationForGVK(ctx context.Context, gvk *schema.GroupVersionKind, validationMode v1.CompositionValidationMode) (*apiextensions.CustomResourceValidation, error) {
	crds := extv1.CustomResourceDefinitionList{}
	if err := c.client.List(ctx, &crds, client.MatchingFields{"spec.group": gvk.Group}, client.MatchingFields{"spec.names.kind": gvk.Kind}); err != nil {
		return nil, err
	}
	switch len(crds.Items) {
	case 0:
		if validationMode == v1.CompositionValidationModeStrict {
			return nil, fmt.Errorf("no CRDs found: %v", gvk)
		}
		return nil, nil
	case 1:
		crd := crds.Items[0]
		internal := &apiextensions.CustomResourceDefinition{}
		if err := extv1.Convert_v1_CustomResourceDefinition_To_apiextensions_CustomResourceDefinition(&crd, internal, nil); err != nil {
			return nil, err
		}
		if v := internal.Spec.Validation; v != nil {
			return v, nil
		}
		for _, version := range internal.Spec.Versions {
			if version.Name == gvk.Version {
				return version.Schema, nil
			}
		}
		return nil, fmt.Errorf("no CRD found for version: %v, %v", gvk, crd)
	}

	return nil, fmt.Errorf("too many CRDs found: %v, %v", gvk, crds)
}

func (c *ClientCompositionValidator) getBasesCRDs(ctx context.Context, resources []v1.ComposedTemplate, validationMode v1.CompositionValidationMode) (GVKValidationMap, error) {
	gvkToCRDV := make(GVKValidationMap)
	for _, resource := range resources {
		cd := composed.New()
		if err := json.Unmarshal(resource.Base.Raw, cd); err != nil {
			return nil, err
		}
		gvk := cd.GetObjectKind().GroupVersionKind()
		if _, ok := gvkToCRDV[gvk]; ok {
			continue
		}
		crdv, err := c.getCRDValidationForGVK(ctx, &gvk, validationMode)
		if err != nil {
			return nil, err
		}
		if crdv != nil {
			gvkToCRDV[gvk] = *crdv
		}
	}
	return gvkToCRDV, nil
}

// PatchValidationContext is the context for validating a patch.
type PatchValidationContext struct {
	// CompositionValidationMode is the validation mode for the composition.
	CompositionValidationMode v1.CompositionValidationMode

	// GVKValidationMap is a map of GVK to CRD validation.
	GVKCRDValidation GVKValidationMap

	// CompositeGVK is the GVK of the composite resource.
	CompositeGVK schema.GroupVersionKind

	// ComposedGVK is the GVK of the composed resource.
	ComposedGVK schema.GroupVersionKind
}

// IsValidatable returns true if the composition is validatable.
func IsValidatable(comp *v1.Composition) error {
	if comp == nil {
		return fmt.Errorf("composition is nil")
	}
	// If the composition has any functions, it is not validatable.
	if len(comp.Spec.Functions) > 0 {
		return fmt.Errorf("composition has functions")
	}
	// If the composition uses any patch that we don't yet handle, it is not validatable.
	for _, set := range comp.Spec.PatchSets {
		for _, patch := range set.Patches {
			if !IsValidatablePatchType(&patch) {
				return fmt.Errorf("composition uses patch type that is not yet validatable: %s", patch.Type)
			}
		}
	}
	for _, resource := range comp.Spec.Resources {
		for _, patch := range resource.Patches {
			if !IsValidatablePatchType(&patch) {
				return fmt.Errorf("composition uses patch type that is not yet validatable: %s", patch.Type)
			}
		}
	}
	return nil
}

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

// ValidatePatch validates the patch according to each patch type, if supported
func ValidatePatch(patch v1.Patch, patchContext *PatchValidationContext) (err error) {
	if !IsValidatablePatchType(&patch) {
		return nil
	}
	switch patch.Type {
	case v1.PatchTypeFromCompositeFieldPath:
		err = ValidateFromCompositeFieldPathPatch(patch, patchContext)
	case v1.PatchTypeCombineFromComposite:
		//TODO: implement
		//err = validateCombineFromCompositePatch(patch, PatchValidationContext)
	case v1.PatchTypeFromEnvironmentFieldPath:
		//TODO: implement
		//err = validateFromEnvironmentFieldPathPatch(patch, PatchValidationContext)
	case v1.PatchTypeCombineFromEnvironment:
		//TODO: implement
		//err = validateCombineFromEnvironmentPatch(patch, PatchValidationContext)
	case v1.PatchTypeToCompositeFieldPath:
		//TODO: implement
		//err = validateToCompositeFieldPathPatch(patch, PatchValidationContext)
	case v1.PatchTypeToEnvironmentFieldPath:
		//TODO: implement
		//err = validateToEnvironmentFieldPathPatch(patch, PatchValidationContext)
	case v1.PatchTypeCombineToComposite:
		//TODO: implement
		//err = validateCombineToCompositePatch(patch, PatchValidationContext)
	case v1.PatchTypeCombineToEnvironment:
		//TODO: implement
		//err = validateCombineToEnvironmentPatch(patch, PatchValidationContext)
	case v1.PatchTypePatchSet:
		//do nothing
	}
	if err != nil {
		return err
	}
	return nil
}

// validateFromCompositeFieldPathPatch validates the patch type FromCompositeFieldPath.
func ValidateFromCompositeFieldPathPatch(patch v1.Patch, c *PatchValidationContext) error {
	if patch.Type != v1.PatchTypeFromCompositeFieldPath {
		return xprerrors.Errorf("invalid patch type: %s", patch.Type)
	}
	compositeValidation, ok := c.GVKCRDValidation[c.CompositeGVK]
	if !ok && c.CompositionValidationMode == v1.CompositionValidationModeStrict {
		return xprerrors.Errorf("no validation found for composite resource: %v", c.CompositeGVK)
	}
	composedValidation, ok := c.GVKCRDValidation[c.ComposedGVK]
	if !ok && c.CompositionValidationMode == v1.CompositionValidationModeStrict {
		return xprerrors.Errorf("no validation found for composed resource: %v", c.ComposedGVK)
	}
	compositeFieldpathType, err := validateFieldPath(patch.FromFieldPath, compositeValidation.OpenAPIV3Schema)
	if err != nil {
		return xprerrors.Wrapf(err, "invalid fromFieldPath: %s", patch.FromFieldPath)
	}
	composedFieldpathType, err := validateFieldPath(patch.ToFieldPath, composedValidation.OpenAPIV3Schema)
	if err != nil {
		return xprerrors.Wrapf(err, "invalid toFieldPath: %s", patch.ToFieldPath)
	}
	// TODO: transform can change the value type of the field path, so we should
	// validate the type of the field path after the transform is applied.
	if len(patch.Transforms) == 0 &&
		compositeFieldpathType != "" && composedFieldpathType != "" && compositeFieldpathType != composedFieldpathType {
		return xprerrors.Errorf("field path types do not match: %s, %s", compositeFieldpathType, composedFieldpathType)
	}
	return nil
}

// validateFieldPath validates that the given field path is valid for the given schema.
// It returns the type of the field path if it is valid, or an error otherwise.
func validateFieldPath(path *string, s *apiextensions.JSONSchemaProps) (fieldType string, err error) {
	if path == nil {
		return "", nil
	}
	segments, err := fieldpath.Parse(*path)
	if len(segments) > 0 && segments[0].Type == fieldpath.SegmentField && segments[0].Field == "metadata" {
		segments = segments[1:]
		s = &metadataSchema
	}
	if err != nil {
		return "", nil
	}
	current := s
	for _, segment := range segments {
		var err error
		current, err = validateFieldPathSegment(current, segment)
		if err != nil {
			return "", err
		}
		if current == nil {
			return "", nil
		}
	}
	return current.Type, nil
}

// validateFieldPathSegment validates that the given field path segment is valid for the given schema.
// It returns the schema of the field path segment if it is valid, or an error otherwise.
func validateFieldPathSegment(current *apiextensions.JSONSchemaProps, segment fieldpath.Segment) (*apiextensions.JSONSchemaProps, error) {
	if current == nil {
		return nil, nil
	}
	switch segment.Type {
	case fieldpath.SegmentField:
		propType := current.Type
		if propType == "" {
			propType = "object"
		}
		if propType != "object" {
			return nil, xprerrors.Errorf("trying to access field of not an object: %v", propType)
		}
		if pointer.BoolDeref(current.XPreserveUnknownFields, false) {
			return nil, nil
		}
		prop, exists := current.Properties[segment.Field]
		if !exists {
			if current.AdditionalProperties != nil && current.AdditionalProperties.Allows {
				return current.AdditionalProperties.Schema, nil
			}
			return nil, xprerrors.Errorf("unable to find field: %s", segment.Field)
		}
		return &prop, nil
	case fieldpath.SegmentIndex:
		if current.Type != "array" {
			return nil, xprerrors.Errorf("accessing by index a %s field", current.Type)
		}
		if current.Items == nil {
			return nil, xprerrors.New("no items found in array")
		}
		if s := current.Items.Schema; s != nil {
			return s, nil
		}
		schemas := current.Items.JSONSchemas
		if len(schemas) < int(segment.Index) {
			return nil, xprerrors.Errorf("")
		}

		return current.Items.Schema, nil
	}
	return nil, nil
}
