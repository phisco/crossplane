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
)

// A CompositionValidatorInterface validates the supplied Composition.
type CompositionValidatorInterface interface {
	Validate(comp *v1.Composition) error
}

// A CompositionValidatorFn validates the supplied Composition.
type CompositionValidatorFn func(comp *v1.Composition) error

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
	return ctrl.NewWebhookManagedBy(mgr).
		WithValidator(c).
		For(&v1.Composition{}).
		Complete()
}

func (c *ClientCompositionValidator) ValidateCreate(ctx context.Context, obj runtime.Object) error {
	fmt.Println("HERE: GOT A CREATE")

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
		return err
	}

	c.validationMode = validationMode

	// Get schema for Composite Resource Definition defined by comp.Spec.CompositeTypeRef
	gvk, err := typeReferenceToGVK(&comp.Spec.CompositeTypeRef)
	if err != nil {
		return err
	}
	compositeCrd, err := c.getCRDVerisonForGVK(ctx, gvk)
	if err != nil {
		return err
	}
	// Get schema for all Managed Resources in comp.Spec.Resources[*].Base
	managedResourcesCRDs, err := c.getBasesCRDs(ctx, comp.Spec.Resources)
	if err != nil {
		return err
	}

	// dereference all patches first
	resources, err := composite.ComposedTemplates(comp.Spec)
	if err != nil {
		return err
	}

	composedResources := make([]runtime.Object, len(resources))

	// Validate all patches given the schemas above
	for i, resource := range resources {
		// get schema for resource.Base from managedResourcesCRDs
		gvk := resource.Base.Object.GetObjectKind().GroupVersionKind()
		crd := managedResourcesCRDs[gvk]
		// validate patches using it and the compositeCrd resource

		cd := composed.New()
		if err := json.Unmarshal(resource.Base.Raw, cd); err != nil {
			return err
		}
		compositeRes := composite2.New(composite2.WithGroupVersionKind(gvk))
		for _, patch := range resource.Patches {
			if err := validatePatch(patch, compositeCrd, &crd); err != nil {
				return err
			}

			err := composite.ApplyToObjects(patch, compositeRes, cd, v1.PatchTypeFromCompositeFieldPath)
			if err != nil {
				return nil
			}
		}
		composedResources[i] = cd
	}

	// Validate Rendered Composed Resources from Composition

	for _, renderedComposed := range composedResources {
		vs, _, err := validation.NewSchemaValidator(managedResourcesCRDs[renderedComposed.GetObjectKind().GroupVersionKind()].Schema)
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
	fmt.Println("HERE: GOT AN UPDATE")
	return c.ValidateCreate(ctx, newObj)
}

func (c *ClientCompositionValidator) ValidateDelete(ctx context.Context, obj runtime.Object) error {
	fmt.Println("HERE: GOT A DELETE")
	return nil
}

func (c *ClientCompositionValidator) getCRDVerisonForGVK(ctx context.Context, gvk *schema.GroupVersionKind) (*apiextensions.CustomResourceDefinitionVersion, error) {
	crds := extv1.CustomResourceDefinitionList{}
	if err := c.client.List(ctx, &crds, client.MatchingFields{"spec.group": gvk.Group}, client.MatchingFields{"spec.names.kind": gvk.Kind}); err != nil {
		return nil, err
	}
	switch len(crds.Items) {
	case 0:
		return nil, fmt.Errorf("no CRDs found: %v", gvk)
	case 1:
		crd := crds.Items[0]
		internal := &apiextensions.CustomResourceDefinition{}
		if err := extv1.Convert_v1_CustomResourceDefinition_To_apiextensions_CustomResourceDefinition(&crd, internal, nil); err != nil {
			return nil, err
		}
		for _, version := range internal.Spec.Versions {
			if version.Name == gvk.Version {
				return &version, nil
			}
		}
		return nil, fmt.Errorf("no CRD found for version: %v, %v", gvk, crd)
	default:
		return nil, fmt.Errorf("too many CRDs found: %v, %v", gvk, crds)
	}
	if len(crds.Items) == 1 {
	}

	return nil, fmt.Errorf("found too many crds, %v, %v", gvk, crds)
}

func typeReferenceToGVK(ref *v1.TypeReference) (*schema.GroupVersionKind, error) {
	gv, err := schema.ParseGroupVersion(ref.APIVersion)
	if err != nil {
		return nil, err
	}
	return &schema.GroupVersionKind{
		Kind:    ref.Kind,
		Group:   gv.Group,
		Version: gv.Version,
	}, nil
}

func (c *ClientCompositionValidator) getBasesCRDs(ctx context.Context, resources []v1.ComposedTemplate) (map[schema.GroupVersionKind]apiextensions.CustomResourceDefinitionVersion, error) {
	gvkToCRDV := make(map[schema.GroupVersionKind]apiextensions.CustomResourceDefinitionVersion)
	for _, resource := range resources {
		cd := composed.New()
		if err := json.Unmarshal(resource.Base.Raw, cd); err != nil {
			return nil, err
		}
		gvk := cd.GetObjectKind().GroupVersionKind()
		if _, ok := gvkToCRDV[gvk]; ok {
			continue
		}
		crdv, err := c.getCRDVerisonForGVK(ctx, &gvk)
		if err != nil {
			return nil, err
		}
		gvkToCRDV[gvk] = *crdv
	}
	return gvkToCRDV, nil
}

func validatePatch(patch v1.Patch, compositeTypeCRD *apiextensions.CustomResourceDefinitionVersion, basesCRD *apiextensions.CustomResourceDefinitionVersion) error {
	switch patch.Type {
	default:
		return nil
	}
}
