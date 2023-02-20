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
	"fmt"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/composed"
	composite2 "github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/composite"
	"github.com/crossplane/crossplane/apis/apiextensions/v1"
	"github.com/crossplane/crossplane/internal/controller/apiextensions/composite"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
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

// A CompositionValidatorInterface validates the supplied Composition.
type CompositionValidatorInterface interface {
	Validate(comp *v1.Composition) error
}

// A CompositionValidatorFn validates the supplied Composition.
type CompositionValidatorFn func(comp *v1.Composition) error

type gvkValidationMap map[schema.GroupVersionKind]apiextensions.CustomResourceValidation

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

	return errors.New(errMixed)
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
			return errors.New(errDuplicate)
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
			return errors.New(errFnsRequireNames)
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
				return errors.New(errFnMissingContainerConfig)
			}
		default:
			return errors.Errorf(errFmtUnknownFnType, fn.Type)
		}
	}
	return nil
}

type ClientCompositionValidator struct {
	client         client.Client
	validationMode v1.CompositionValidationMode
	renderer       composite.Renderer
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
	return ctrl.NewWebhookManagedBy(mgr).
		WithValidator(c).
		For(&v1.Composition{}).
		Complete()
}

func (c *ClientCompositionValidator) ValidateCreate(ctx context.Context, obj runtime.Object) error {

	comp, ok := obj.(*v1.Composition)
	if !ok {
		return errors.New(errUnexpectedType)
	}

	// Get the validation mode set through annotations for the composition
	validationMode, err := getCompositionValidationMode(comp)
	if err != nil {
		return err
	}

	// Validate general assertions
	if err := defaultCompositionValidationChain.Validate(comp); err != nil {
		fmt.Errorf("HERE: defaultCompositionValidationChain failed: %v", err)
		return err
	}

	c.validationMode = validationMode

	// Get schema for Composite Resource Definition defined by comp.Spec.CompositeTypeRef

	compositeResGVK := schema.FromAPIVersionAndKind(comp.Spec.CompositeTypeRef.APIVersion,
		comp.Spec.CompositeTypeRef.Kind)

	compositeCrdValidation, err := c.getCRDValidationForGVK(ctx, &compositeResGVK)
	if err != nil {
		return err
	}
	// Get schema for all Managed Resources in comp.Spec.Resources[*].Base
	managedResourcesCRDs, err := c.getBasesCRDs(ctx, comp.Spec.Resources)
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

	composedResources := make([]runtime.Object, len(resources))

	compositeRes := composite2.New(composite2.WithGroupVersionKind(compositeResGVK))
	compositeRes.SetUID("validation-uid")
	compositeRes.SetName("validation-name")
	composite.NewPureAPINamingConfigurator().Configure(ctx, compositeRes, nil)

	// Validate all patches given the schemas above
	for i, resource := range resources {
		// validate patches using it and the compositeCrd resource
		cd := composed.New()
		if err := json.Unmarshal(resource.Base.Raw, cd); err != nil {
			return err
		}
		composedGVK := cd.GetObjectKind().GroupVersionKind()
		patchCtx := patchContext{
			gvkCRDValidation:          managedResourcesCRDs,
			compositionValidationMode: validationMode,
			composedGVK:               composedGVK,
			compositeGVK:              compositeResGVK,
		}
		for _, patch := range resource.Patches {
			if err := validatePatch(patch, &patchCtx); err != nil {
				return err
			}
		}

		// TODO: handle env too
		if err := c.renderer.Render(ctx, compositeRes, cd, resource, nil); err != nil {
			return err
		}
		composedResources[i] = cd
	}

	// Validate Rendered Composed Resources from Composition
	for _, renderedComposed := range composedResources {
		crdV, ok := managedResourcesCRDs[renderedComposed.GetObjectKind().GroupVersionKind()]
		if !ok {
			if c.validationMode == v1.CompositionValidationModeStrict {
				return errors.Errorf("No CRD validation found for rendered resource: %v", renderedComposed.GetObjectKind().GroupVersionKind())
			}
			continue
		}
		vs, _, err := validation.NewSchemaValidator(&crdV)
		if err != nil {
			return err
		}
		r := vs.Validate(renderedComposed)
		if r.HasErrors() {
			return r.AsError()
		}
		if r.HasWarnings() {
			return errors.Errorf("warnings: %v", r.Warnings)
		}
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
	return "", errors.Errorf("invalid composition validation mode: %s", mode)
}

func (c *ClientCompositionValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) error {
	return c.ValidateCreate(ctx, newObj)
}

func (c *ClientCompositionValidator) ValidateDelete(ctx context.Context, obj runtime.Object) error {
	return nil
}

func (c *ClientCompositionValidator) getCRDValidationForGVK(ctx context.Context, gvk *schema.GroupVersionKind) (*apiextensions.CustomResourceValidation, error) {
	crds := extv1.CustomResourceDefinitionList{}
	if err := c.client.List(ctx, &crds, client.MatchingFields{"spec.group": gvk.Group}, client.MatchingFields{"spec.names.kind": gvk.Kind}); err != nil {
		return nil, err
	}
	switch len(crds.Items) {
	case 0:
		if c.validationMode == v1.CompositionValidationModeStrict {
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

func (c *ClientCompositionValidator) getBasesCRDs(ctx context.Context, resources []v1.ComposedTemplate) (gvkValidationMap, error) {
	gvkToCRDV := make(gvkValidationMap)
	for _, resource := range resources {
		cd := composed.New()
		if err := json.Unmarshal(resource.Base.Raw, cd); err != nil {
			return nil, err
		}
		gvk := cd.GetObjectKind().GroupVersionKind()
		if _, ok := gvkToCRDV[gvk]; ok {
			continue
		}
		crdv, err := c.getCRDValidationForGVK(ctx, &gvk)
		if err != nil {
			return nil, err
		}
		if crdv != nil {
			gvkToCRDV[gvk] = *crdv
		}
	}
	return gvkToCRDV, nil
}

type patchContext struct {
	// compositionValidationMode is the validation mode for the composition.
	compositionValidationMode v1.CompositionValidationMode

	// gvkValidationMap is a map of GVK to CRD validation.
	gvkCRDValidation gvkValidationMap

	// compositeGVK is the GVK of the composite resource.
	compositeGVK schema.GroupVersionKind

	// composedGVK is the GVK of the composed resource.
	composedGVK schema.GroupVersionKind
}

func validatePatch(patch v1.Patch, patchContext *patchContext) (err error) {
	switch patch.Type {
	case v1.PatchTypeFromCompositeFieldPath:
		err = validateFromCompositeFieldPathPatch(patch, patchContext)
	}
	if err != nil {
		return err
	}
	return nil
}

func validateFromCompositeFieldPathPatch(patch v1.Patch, c *patchContext) error {
	compositeValidation, ok := c.gvkCRDValidation[c.compositeGVK]
	if !ok && c.compositionValidationMode == v1.CompositionValidationModeStrict {
		return errors.Errorf("no validation found for composite resource: %v", c.compositeGVK)
	}
	composedValidation, ok := c.gvkCRDValidation[c.composedGVK]
	if !ok && c.compositionValidationMode == v1.CompositionValidationModeStrict {
		return errors.Errorf("no validation found for composed resource: %v", c.composedGVK)
	}
	compositeFieldpathType, err := validateFieldPath(patch.FromFieldPath, compositeValidation.OpenAPIV3Schema)
	if err != nil {
		return errors.Wrapf(err, "invalid fromFieldPath: %s", patch.FromFieldPath)
	}
	composedFieldpathType, err := validateFieldPath(patch.ToFieldPath, composedValidation.OpenAPIV3Schema)
	if err != nil {
		return errors.Wrapf(err, "invalid toFieldPath: %s", patch.ToFieldPath)
	}
	if compositeFieldpathType != "" && composedFieldpathType != "" && compositeFieldpathType != composedFieldpathType {
		return errors.Errorf("field path types do not match: %s, %s", compositeFieldpathType, composedFieldpathType)
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
			return nil, errors.Errorf("trying to access field of not an object: %v", propType)
		}
		if pointer.BoolDeref(current.XPreserveUnknownFields, false) {
			return nil, nil
		}
		prop, exists := current.Properties[segment.Field]
		if !exists {
			if current.AdditionalProperties != nil && current.AdditionalProperties.Allows {
				return current.AdditionalProperties.Schema, nil
			}
			return nil, errors.Errorf("unable to find field: %s", segment.Field)
		}
		return &prop, nil
	case fieldpath.SegmentIndex:
		if current.Type != "array" {
			return nil, errors.Errorf("accessing by index a %s field", current.Type)
		}
		if current.Items == nil {
			return nil, errors.New("no items found in array")
		}
		if s := current.Items.Schema; s != nil {
			return s, nil
		}
		schemas := current.Items.JSONSchemas
		if len(schemas) < int(segment.Index) {
			return nil, errors.Errorf("")
		}

		return current.Items.Schema, nil
	}
	return nil, nil
}
