/*
Copyright 202333he Crossplane Authors.

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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	validation2 "k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	unstructured2 "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured"
	xprcomposite "github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/composite"
	xprvalidation "github.com/crossplane/crossplane-runtime/pkg/validation"
	v1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
	"github.com/crossplane/crossplane/internal/controller/apiextensions/composite"
	"github.com/crossplane/crossplane/internal/controller/apiextensions/composition/validation"
	"github.com/crossplane/crossplane/internal/xcrd"
)

// CustomValidator gathers required information using the provided client.Reader and then use them to render and
// validated a Composition.
type CustomValidator struct {
	clientBuilder *clientWithFallbackReaderBuilder
}

// SetupWithManager sets up the CustomValidator with the provided manager, setting up all the required indexes it requires.
func (c *CustomValidator) SetupWithManager(mgr ctrl.Manager) error {
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

	c.clientBuilder = newClientWithFallbackReaderBuilder(mgr)

	return ctrl.NewWebhookManagedBy(mgr).
		WithValidator(c).
		For(&v1.Composition{}).
		Complete()
}

type clientWithFallbackReaderBuilder struct {
	builder *fake.ClientBuilder
	reader  client.Reader
}

func newClientWithFallbackReaderBuilder(mgr manager.Manager) *clientWithFallbackReaderBuilder {
	return &clientWithFallbackReaderBuilder{
		builder: fake.NewClientBuilder().WithScheme(mgr.GetScheme()),
		reader:  unstructured.NewClient(mgr.GetClient()),
	}
}

func (b *clientWithFallbackReaderBuilder) withObjects(objs ...client.Object) *clientWithFallbackReaderBuilder {
	return &clientWithFallbackReaderBuilder{builder: b.builder.WithObjects(objs...), reader: b.reader}
}

// Build returns a new ClientWithFallbackReader.
func (b *clientWithFallbackReaderBuilder) build() *ClientWithFallbackReader {
	return NewClientWithFallbackReader(b.builder.Build(), b.reader)
}

// ValidateUpdate is a no-op for now.
func (c *CustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) error {
	return c.ValidateCreate(ctx, newObj)
}

// ValidateDelete is a no-op for now.
func (c *CustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) error {
	return nil
}

// ValidateCreate validates the Composition by rendering it and then validating the rendered resources.
//
//nolint:gocyclo // TODO(phisco): Refactor this function.
func (c *CustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) error {
	comp, ok := obj.(*v1.Composition)
	if !ok {
		return errors.New("not a v1 Composition")
	}

	// Get the composition validation mode from annotation
	validationMode, err := comp.GetValidationMode()
	if err != nil {
		return errors.Wrap(err, "cannot get validation mode")
	}

	// Get all the needed CRDs, Composite Resource, Managed resources ... ? Error out if missing in strict mode
	gvkToCRDs, errs := c.getNeededCRDs(ctx, comp)
	var looseModeSkip bool
	for _, err := range errs {
		if err == nil {
			continue
		}
		// If any of the errors is not a NotFound error, error out
		if !apierrors.IsNotFound(err) {
			return errors.Errorf("there were some errors while getting the needed CRDs: %v", errs)
		}
		// If any of the needed CRDs is not found, error out if strict mode is enabled, otherwise continue
		if validationMode == v1.CompositionValidationModeStrict {
			return errors.Wrap(err, "cannot get needed CRDs and strict mode is enabled")
		}
		if validationMode == v1.CompositionValidationModeLoose {
			looseModeSkip = true
		}
	}

	// From here on we should refactor the code to allow using it from linters/Lsp

	// Perform logical checks
	if err := validation.GetLogicalChecks().Validate(comp); err != nil {
		return errors.Wrap(err, "invalid composition")
	}

	// Given that some requirement is missing, and we are in loose mode, skip the rest of the validation
	if looseModeSkip && validationMode == v1.CompositionValidationModeLoose {
		// TODO: emit a warning here
		return nil
	}

	// Validate patches given the above CRDs, skip if any of the required CRDs is not available
	if errs := validation.ValidatePatches(comp, gvkToCRDs); len(errs) != 0 {
		return errors.Errorf("there were some errors while validating the patches: %v", errs)
	}

	// Return if using unsupported/non-deterministic features, e.g. Transforms...
	if err := comp.IsUsingNonDeterministicTransforms(); err != nil {
		return nil //nolint:nilerr // we can not check anything else
	}

	// Mock any required input given their CRDs => crossplane-runtime
	compositeResGVK := schema.FromAPIVersionAndKind(comp.Spec.CompositeTypeRef.APIVersion,
		comp.Spec.CompositeTypeRef.Kind)
	compositeRes := xprcomposite.New(xprcomposite.WithGroupVersionKind(compositeResGVK))
	compositeRes.SetName("fake")
	compositeRes.SetNamespace("test")
	compositeRes.SetCompositionReference(&corev1.ObjectReference{Name: comp.GetName()})
	if err := xprvalidation.MockRequiredFields(compositeRes, gvkToCRDs[compositeResGVK].Spec.Validation.OpenAPIV3Schema); err != nil {
		return errors.Wrap(err, "cannot mock required fields")
	}

	mockClient := c.clientBuilder.withObjects(
		// mocked Composite resource
		compositeRes,
		comp,
	).build()
	// Render resources => reuse existing logic
	r := composite.NewReconcilerFromClient(mockClient, resource.CompositeKind(schema.FromAPIVersionAndKind(comp.Spec.CompositeTypeRef.APIVersion,
		comp.Spec.CompositeTypeRef.Kind)))
	if _, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: "fake", Namespace: "test"}}); err != nil {
		return errors.Wrap(err, "cannot render resources")
	}

	fakeClient := mockClient.GetClient()

	// Validate resources given their CRDs => crossplane-runtime
	var validationErrs []error
	var validationWarns []error
	for gvk, crd := range gvkToCRDs {
		if gvk == compositeResGVK {
			continue
		}
		composedRes := &unstructured2.UnstructuredList{}
		composedRes.SetGroupVersionKind(gvk)
		err = fakeClient.List(ctx, composedRes, client.MatchingLabels{xcrd.LabelKeyNamePrefixForComposed: "fake"})
		if err != nil {
			return errors.Wrap(err, "cannot list composed resources")
		}
		for _, cd := range composedRes.Items {
			vs, _, err := validation2.NewSchemaValidator(crd.Spec.Validation)
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
		return errors.Errorf("there were some errors while validating the rendered resources: %v", validationErrs)
	}
	if len(validationWarns) != 0 {
		fmt.Printf("there were some warnings while validating the rendered resources: %v\n", validationWarns)
	}

	return nil
}
