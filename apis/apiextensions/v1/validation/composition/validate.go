package composition

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apivalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	unstructured2 "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
) error {
	// Perform logical checks
	if err := validation.GetLogicalChecks().Validate(comp); err != nil {
		return xperrors.Wrap(err, "invalid composition")
	}

	// Validate patches given the above CRDs, skip if any of the required CRDs is not available
	if errs := validation.ValidatePatches(comp, gvkToCRDs); len(errs) != 0 {
		return xperrors.Errorf("there were some errors while validating the patches:\n%s", errors.Join(errs...))
	}

	// Return if using unsupported/non-deterministic features, e.g. Transforms...
	if err := comp.IsUsingNonDeterministicTransforms(); err != nil {
		return nil //nolint:nilerr // we can not check anything else
	}

	// Mock any required input given their CRDs => crossplane-runtime
	compositeResGVK := schema.FromAPIVersionAndKind(comp.Spec.CompositeTypeRef.APIVersion,
		comp.Spec.CompositeTypeRef.Kind)
	compositeResCRD, ok := gvkToCRDs[compositeResGVK]
	if !ok {
		return xperrors.Errorf("cannot find CRD for composite resource %s", compositeResGVK)
	}
	compositeRes := xprcomposite.New(xprcomposite.WithGroupVersionKind(compositeResGVK))
	compositeRes.SetName("fake")
	compositeRes.SetNamespace("test")
	compositeRes.SetCompositionReference(&corev1.ObjectReference{Name: comp.GetName()})
	if err := xprvalidation.MockRequiredFields(compositeRes, compositeResCRD.Spec.Validation.OpenAPIV3Schema); err != nil {
		return xperrors.Wrap(err, "cannot mock required fields")
	}

	// create or update all required resources
	for _, obj := range []client.Object{compositeRes, comp} {
		err := c.Create(ctx, obj)
		if apierrors.IsAlreadyExists(err) {
			if err := c.Update(ctx, obj); err != nil {
				return err
			}
		}
	}

	// Render resources => reuse existing logic
	r := composite.NewReconcilerFromClient(c, resource.CompositeKind(schema.FromAPIVersionAndKind(comp.Spec.CompositeTypeRef.APIVersion,
		comp.Spec.CompositeTypeRef.Kind)))
	if _, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: "fake", Namespace: "test"}}); err != nil {
		return xperrors.Wrap(err, "cannot render resources")
	}

	// Validate resources given their CRDs => crossplane-runtime
	var validationErrs []error
	var validationWarns []error
	for gvk, crd := range gvkToCRDs {
		if gvk == compositeResGVK {
			continue
		}
		composedRes := &unstructured2.UnstructuredList{}
		composedRes.SetGroupVersionKind(gvk)
		err := c.List(ctx, composedRes, client.MatchingLabels{xcrd.LabelKeyNamePrefixForComposed: "fake"})
		if err != nil {
			return xperrors.Wrap(err, "cannot list composed resources")
		}
		for _, cd := range composedRes.Items {
			vs, _, err := apivalidation.NewSchemaValidator(crd.Spec.Validation)
			if err != nil {
				return err
			}
			r := vs.Validate(cd.Object)
			if r.HasErrors() {
				validationErrs = append(validationErrs, r.Errors...)
			}
			if r.HasWarnings() {
				validationWarns = append(validationWarns, r.Warnings...)
			}
		}
	}
	if len(validationErrs) != 0 {
		return xperrors.Errorf("there were some errors while validating the rendered resources:\n%s", errors.Join(validationErrs...))
	}
	if len(validationWarns) != 0 {
		fmt.Printf("there were some warnings while validating the rendered resources:\n%s", errors.Join(validationWarns...))
	}

	return nil
}
