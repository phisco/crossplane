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

	"k8s.io/apimachinery/pkg/util/validation/field"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apivalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	xperrors "github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	xprcomposite "github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/composite"
	xprvalidation "github.com/crossplane/crossplane-runtime/pkg/validation"

	v1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
	"github.com/crossplane/crossplane/internal/controller/apiextensions/composite"
	"github.com/crossplane/crossplane/internal/controller/apiextensions/composition/validation"
	"github.com/crossplane/crossplane/internal/xcrd"
)

// ValidateComposition validates the Composition by rendering it and then validating the rendered resources using the
// provided CustomValidator.
//
//nolint:gocyclo // TODO(phisco): Refactor this function.
func ValidateComposition(
	ctx context.Context,
	comp *v1.Composition,
	gvkToCRDs map[schema.GroupVersionKind]apiextensions.CustomResourceDefinition,
	c client.Client,
) (errs field.ErrorList) {
	// Perform logical checks
	if err := validation.GetLogicalChecks().Validate(comp); err != nil {
		errs = append(errs, err...)
		return errs
	}

	// Validate patches given the above CRDs, skip if any of the required CRDs is not available
	if patchErrs := ValidatePatches(comp, gvkToCRDs); len(patchErrs) > 0 {
		errs = append(errs, patchErrs...)
		return errs
	}

	if connErrs := ValidateConnectionDetails(comp, gvkToCRDs); len(connErrs) > 0 {
		errs = append(errs, connErrs...)
		return errs
	}

	if readErrs := ValidateReadinessCheck(comp, gvkToCRDs); len(readErrs) > 0 {
		errs = append(errs, readErrs...)
		return errs
	}

	// Return if using unsupported/non-deterministic features, e.g. Transforms...
	if err := comp.IsUsingNonDeterministicTransforms(); err != nil {
		return nil
	}

	// Mock any required input given their CRDs => crossplane-runtime
	// TODO(lsviben) refactor
	compositeResGVK := schema.FromAPIVersionAndKind(comp.Spec.CompositeTypeRef.APIVersion,
		comp.Spec.CompositeTypeRef.Kind)
	compositeResCRD, ok := gvkToCRDs[compositeResGVK]
	if !ok {
		errs = append(errs, field.Invalid(
			field.NewPath("spec", "compositeTypeRef"),
			comp.Spec.CompositeTypeRef,
			fmt.Sprintf("cannot find CRD for composite resource %s", compositeResGVK),
		))
		return errs
	}
	compositeRes := xprcomposite.New(xprcomposite.WithGroupVersionKind(compositeResGVK))
	compositeRes.SetName("fake")
	compositeRes.SetNamespace("test")
	compositeRes.SetCompositionReference(&corev1.ObjectReference{Name: comp.GetName()})
	if err := xprvalidation.MockRequiredFields(compositeRes, compositeResCRD.Spec.Validation.OpenAPIV3Schema); err != nil {
		errs = append(errs, field.InternalError(field.NewPath("spec", "compositeTypeRef"), err))
		return errs
	}

	// create or update all required resources
	for _, obj := range []client.Object{compositeRes, comp} {
		err := c.Create(ctx, obj)
		if apierrors.IsAlreadyExists(err) {
			if err := c.Update(ctx, obj); err != nil {
				errs = append(errs, field.InternalError(field.NewPath("spec"), xperrors.Wrap(err, "cannot update required resources")))
				return errs
			}
		} else if err != nil {
			errs = append(errs, field.InternalError(field.NewPath("spec"), xperrors.Wrap(err, "cannot create required resources")))
			return errs
		}
	}

	// Render resources => reuse existing logic
	r := composite.NewReconcilerFromClient(c, resource.CompositeKind(schema.FromAPIVersionAndKind(comp.Spec.CompositeTypeRef.APIVersion,
		comp.Spec.CompositeTypeRef.Kind)))
	if _, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: "fake", Namespace: "test"}}); err != nil {
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
		err := c.List(ctx, composedRes, client.MatchingLabels{xcrd.LabelKeyNamePrefixForComposed: "fake"})
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
