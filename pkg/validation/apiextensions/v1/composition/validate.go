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
	"context"
	"encoding/json"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apivalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/crossplane/crossplane/apis"

	xperrors "github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	xprcomposite "github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/composite"
	xprvalidation "github.com/crossplane/crossplane-runtime/pkg/validation"
	"github.com/crossplane/crossplane/internal/xcrd"

	v1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
	"github.com/crossplane/crossplane/internal/controller/apiextensions/composite"
)

const (
	compositeResourceValidationName      = "validationName"
	compositeResourceValidationNamespace = "validationNamespace"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	_ = extv1.AddToScheme(scheme)
	_ = apis.AddToScheme(scheme)
}

// ValidateComposition validates the Composition by rendering it and then validating the rendered resources using the
// provided CustomValidator.
//
//nolint:gocyclo // TODO(phisco): Refactor this function.
func ValidateComposition(
	comp *v1.Composition,
	gvkToCRDs map[schema.GroupVersionKind]apiextensions.CustomResourceDefinition,
) (errs field.ErrorList) {
	ctx := context.Background()
	if errs := comp.Validate(); len(errs) != 0 {
		return errs
	}

	// Validate patches given the above CRDs, skip if any of the required CRDs is not available
	if patchErrs := validatePatchesWithSchemas(comp, gvkToCRDs); len(patchErrs) > 0 {
		errs = append(errs, patchErrs...)
		return errs
	}

	if connErrs := validateConnectionDetailsWithSchemas(comp, gvkToCRDs); len(connErrs) > 0 {
		errs = append(errs, connErrs...)
		return errs
	}

	if readErrs := validateReadinessCheckWithSchemas(comp, gvkToCRDs); len(readErrs) > 0 {
		errs = append(errs, readErrs...)
		return errs
	}

	// Return if using unsupported/non-deterministic features, e.g. Transforms...
	if err := comp.IsUsingFunctions(); err != nil {
		// TODO(lsviben) we should send out a warning that we are not rendering and validating the whole Composition
		return nil
	}

	// Mock any required input given their CRDs
	compositeRes, compositeResGVK := genCompositeResource(comp)
	compositeResCRD, ok := gvkToCRDs[compositeResGVK]
	if !ok {
		return append(errs, field.Invalid(
			field.NewPath("spec", "compositeTypeRef"),
			comp.Spec.CompositeTypeRef,
			fmt.Sprintf("cannot find CRD for composite resource %s", compositeResGVK),
		))
	}
	if err := xprvalidation.MockRequiredFields(compositeRes, compositeResCRD.Spec.Validation.OpenAPIV3Schema); err != nil {
		errs = append(errs, field.InternalError(field.NewPath("spec", "compositeTypeRef"), err))
		return errs
	}
	c := xprvalidation.NewMapClient(scheme)
	// create all required resources
	for _, obj := range []client.Object{compositeRes, comp} {
		err := c.Create(ctx, obj)
		if err != nil {
			errs = append(errs, field.InternalError(field.NewPath("spec"), xperrors.Wrap(err, "cannot create required mock resources")))
			return errs
		}
	}

	// Render resources => reuse existing logic
	r := composite.NewReconcilerFromClient(
		c,
		resource.CompositeKind(schema.FromAPIVersionAndKind(comp.Spec.CompositeTypeRef.APIVersion,
			comp.Spec.CompositeTypeRef.Kind)),
		composite.WithCompositionValidator(func(in *v1.Composition) field.ErrorList {
			return nil
		}),
		// TODO(phisco): handle logger
	)
	if _, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: compositeResourceValidationName, Namespace: compositeResourceValidationNamespace}}); err != nil {
		errs = append(errs, field.InternalError(field.NewPath("spec"), xperrors.Wrap(err, "cannot render resources")))
		return errs
	}

	// Validate resources given their CRDs
	var validationWarns []error
	// TODO (lsviben) we are currently validating only things we have schema for, instead of everything created by the reconciler
	for gvk, crd := range gvkToCRDs {
		if gvk == compositeResGVK {
			continue
		}
		composedRes := &unstructured.UnstructuredList{}
		composedRes.SetGroupVersionKind(gvk)
		// TODO(phisco) add a method to the MappedClient to get everything created
		err := c.List(ctx, composedRes, client.MatchingLabels{xcrd.LabelKeyNamePrefixForComposed: compositeResourceValidationName})
		if err != nil {
			errs = append(errs, field.InternalError(field.NewPath("spec"), xperrors.Wrap(err, "cannot list composed resources")))
			return errs
		}
		for _, cd := range composedRes.Items {
			vs, _, err := apivalidation.NewSchemaValidator(crd.Spec.Validation)
			if err != nil {
				errs = append(errs, field.InternalError(field.NewPath("spec"), xperrors.Wrap(err, "cannot create schema validator")))
				return errs
			}
			r := vs.Validate(cd.Object)
			if r.HasErrors() {
				sourceResourceIndex := findSourceResourceIndex(comp.Spec.Resources, cd)
				for _, err := range r.Errors {
					cdString, marshalErr := json.Marshal(cd)
					if marshalErr != nil {
						cdString = []byte(fmt.Sprintf("%+v", cd))
					}

					// if we can get the sourceResourceIndex, we can send out an error with more context.
					if sourceResourceIndex >= 0 {
						errs = append(errs, field.Invalid(
							field.NewPath("spec", "resources").Index(sourceResourceIndex).Child("base"),
							string(comp.Spec.Resources[sourceResourceIndex].Base.Raw),
							err.Error(),
						))
					} else {
						errs = append(errs, field.Invalid(field.NewPath("composedResource"), string(cdString), err.Error()))
					}
				}
			}
			if r.HasWarnings() {
				validationWarns = append(validationWarns, r.Warnings...)
			}
		}
	}
	if len(errs) != 0 {
		return errs
	}
	if len(validationWarns) != 0 {
		// TODO (lsviben) send the warnings back
		fmt.Printf("there were some warnings while validating the rendered resources:\n%s", errors.Join(validationWarns...))
	}

	return nil
}

func genCompositeResource(comp *v1.Composition) (*xprcomposite.Unstructured, schema.GroupVersionKind) {
	compositeResGVK := schema.FromAPIVersionAndKind(comp.Spec.CompositeTypeRef.APIVersion,
		comp.Spec.CompositeTypeRef.Kind)
	compositeRes := xprcomposite.New(xprcomposite.WithGroupVersionKind(compositeResGVK))
	compositeRes.SetName(compositeResourceValidationName)
	compositeRes.SetNamespace(compositeResourceValidationNamespace)
	compositeRes.SetCompositionReference(&corev1.ObjectReference{Name: comp.GetName()})
	return compositeRes, compositeResGVK
}

func findSourceResourceIndex(resources []v1.ComposedTemplate, composed unstructured.Unstructured) int {
	for i, r := range resources {
		if obj, err := r.GetBaseObject(); err == nil {
			if obj.GetObjectKind().GroupVersionKind() == composed.GetObjectKind().GroupVersionKind() && obj.GetName() == composite.GetCompositionResourceName(&composed) {
				return i
			}
		}
	}
	return -1
}
