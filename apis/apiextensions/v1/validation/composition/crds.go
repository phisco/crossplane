package composition

import (
	"context"
	"fmt"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/composed"
	v1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (c *CustomValidator) getNeededCRDs(ctx context.Context, comp *v1.Composition) (map[schema.GroupVersionKind]apiextensions.CustomResourceDefinition, []error) {
	var resultErrs []error
	neededCrds := make(map[schema.GroupVersionKind]apiextensions.CustomResourceDefinition)

	// Get schema for the Composite Resource Definition defined by comp.Spec.CompositeTypeRef
	compositeResGVK := schema.FromAPIVersionAndKind(comp.Spec.CompositeTypeRef.APIVersion,
		comp.Spec.CompositeTypeRef.Kind)

	compositeCRD, err := c.getCRDForGVK(ctx, &compositeResGVK)
	switch {
	case apierrors.IsNotFound(err):
		resultErrs = append(resultErrs, err)
	case err != nil:
		return nil, []error{err}
	case compositeCRD != nil:
		neededCrds[compositeResGVK] = *compositeCRD
	}

	// Get schema for all Managed Resource Definitions defined by comp.Spec.Resources
	for i, res := range comp.Spec.Resources {
		cd, err := composed.ParseToUnstructured(res.Base.Raw)
		if err != nil {
			resultErrs = append(resultErrs, fmt.Errorf("failed to parse composed resource %v (%d): %w", res.Name, i, err))
		}
		gvk := cd.GetObjectKind().GroupVersionKind()
		if _, ok := neededCrds[gvk]; ok {
			continue
		}
		crd, err := c.getCRDForGVK(ctx, &gvk)
		switch {
		case apierrors.IsNotFound(err):
			resultErrs = append(resultErrs, err)
		case err != nil:
			return nil, []error{err}
		case compositeCRD != nil:
			neededCrds[gvk] = *crd
		}
	}

	return neededCrds, resultErrs
}

// getCRDForGVK returns the validation schema for the given GVK, by looking up the CRD by group and kind using
// the provided client.
func (c *CustomValidator) getCRDForGVK(ctx context.Context, gvk *schema.GroupVersionKind) (*apiextensions.CustomResourceDefinition, error) {
	crds := extv1.CustomResourceDefinitionList{}
	if err := c.clientBuilder.build().List(ctx, &crds, client.MatchingFields{"spec.group": gvk.Group},
		client.MatchingFields{"spec.names.kind": gvk.Kind}); err != nil {
		return nil, err
	}
	if len(crds.Items) != 1 {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: "apiextensions.k8s.io", Resource: "CustomResourceDefinition"}, fmt.Sprintf("%s.%s", gvk.Kind, gvk.Group))
	}
	crd := crds.Items[0]
	internal := &apiextensions.CustomResourceDefinition{}
	return internal, extv1.Convert_v1_CustomResourceDefinition_To_apiextensions_CustomResourceDefinition(&crd, internal, nil)
}
