package validation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	xprerrors "github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/composed"
	composite2 "github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/composite"
	v1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
	"github.com/crossplane/crossplane/internal/controller/apiextensions/composite"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	"k8s.io/apimachinery/pkg/runtime"
)

type RenderValidator interface {
	RenderAndValidate(ctx context.Context, comp *v1.Composition, req *CompositionRenderValidationRequest) error
}

type PureValidator struct {
	Renderer               composite.Renderer
	LogicalValidationChain ValidationChain
}

func NewPureValidator() *PureValidator {
	return &PureValidator{
		Renderer:               composite.NewPureRenderer(),
		LogicalValidationChain: GetDefaultCompositionValidationChain(),
	}
}

func (p *PureValidator) RenderAndValidate(
	ctx context.Context,
	comp *v1.Composition,
	req *CompositionRenderValidationRequest,
) error {

	// dereference all patches first
	resources, err := composite.ComposedTemplates(comp.Spec)
	if err != nil {
		return err
	}

	// RenderAndValidate general assertions
	if err := p.LogicalValidationChain.Validate(comp); err != nil {
		return err
	}

	// Create a composite resource to validate patches against, setting all required fields
	compositeRes := composite2.New(composite2.WithGroupVersionKind(req.CompositeResGVK))
	compositeRes.SetUID("validation-uid")
	compositeRes.SetName("validation-name")
	composite.NewPureAPINamingConfigurator().Configure(ctx, compositeRes, nil)

	composedResources := make([]runtime.Object, len(resources))
	var patchingErr error
	// RenderAndValidate all patches given the schemas above
	for i, resource := range resources {
		// validate patches using it and the compositeCrd resource
		cd := composed.New()
		if err := json.Unmarshal(resource.Base.Raw, cd); err != nil {
			patchingErr = errors.Join(patchingErr, fmt.Errorf("resource %s (%d): %w", *resource.Name, i, err))
			continue
		}
		composedGVK := cd.GetObjectKind().GroupVersionKind()
		patchCtx := PatchValidationRequest{
			GVKCRDValidation:          req.ManagedResourcesCRDs,
			CompositionValidationMode: req.ValidationMode,
			ComposedGVK:               composedGVK,
			CompositeGVK:              req.CompositeResGVK,
		}
		for j, patch := range resource.Patches {
			if err := ValidatePatch(patch, &patchCtx); err != nil {
				patchingErr = errors.Join(patchingErr, fmt.Errorf("resource %s (%d), patch %d: %w", *resource.Name, i, j, err))
				continue
			}
		}

		// TODO: handle env too
		if err := p.Renderer.Render(ctx, compositeRes, cd, resource, nil); err != nil {
			patchingErr = errors.Join(patchingErr, err)
			continue
		}
		composedResources[i] = cd
	}

	if patchingErr != nil {
		return patchingErr
	}

	var renderError error
	// RenderAndValidate Rendered Composed Resources from Composition
	for _, renderedComposed := range composedResources {
		crdV, ok := req.ManagedResourcesCRDs[renderedComposed.GetObjectKind().GroupVersionKind()]
		if !ok {
			if req.ValidationMode == v1.CompositionValidationModeStrict {
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
		return renderError
	}
	return nil
}
