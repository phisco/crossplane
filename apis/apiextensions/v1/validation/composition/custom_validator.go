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
	"encoding/json"
	"errors"
	"fmt"
	jsonpatch "github.com/evanphx/json-patch"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	validation2 "k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	unstructured2 "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	xperrors "github.com/crossplane/crossplane-runtime/pkg/errors"
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
	reader client.Reader
	scheme *runtime.Scheme
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

	c.scheme = mgr.GetScheme()
	c.reader = unstructured.NewClient(mgr.GetClient())

	return ctrl.NewWebhookManagedBy(mgr).
		WithValidator(c).
		For(&v1.Composition{}).
		Complete()
}

type cacheClient struct {
	cache map[schema.GroupVersionKind]map[types.NamespacedName]client.Object
}

func (c *cacheClient) Get(_ context.Context, key client.ObjectKey, out client.Object, opts ...client.GetOption) error {
	if c.cache == nil {
		return nil
	}
	if gvk, ok := c.cache[out.GetObjectKind().GroupVersionKind()]; ok {
		if o, ok := gvk[key]; ok {
			// We have a cache hit, let's copy the object into the provided one
			// Copied from controller-runtime CacheReader implementation
			err := deepCopyInto(out, o)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func deepCopyInto(out client.Object, o client.Object) error {
	outVal := reflect.ValueOf(out)
	objVal := reflect.ValueOf(o)
	if !objVal.Type().AssignableTo(outVal.Type()) {
		return fmt.Errorf("cache had type %s, but %s was asked for", objVal.Type(), outVal.Type())
	}
	reflect.Indirect(outVal).Set(reflect.Indirect(objVal))
	return nil
}

func (c *cacheClient) List(_ context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if c.cache == nil {
		return nil
	}
	gvk, ok := c.cache[list.GetObjectKind().GroupVersionKind()]
	if !ok {
		return nil
	}
	opt := &client.ListOptions{}
	opt.ApplyOptions(opts)
	objs := make([]runtime.Object, 0, len(gvk))
	for _, o := range gvk {
		if opt.Namespace != "" && o.GetNamespace() != opt.Namespace {
			continue
		}
		if opt.LabelSelector != nil && !opt.LabelSelector.Matches(labels.Set(o.GetLabels())) {
			continue
		}
		// TODO: handle rest of the options
		objs = append(objs, o)
	}
	return apimeta.SetList(list, objs)
}

func (c *cacheClient) Create(_ context.Context, obj client.Object, opts ...client.CreateOption) error {
	if c.cache == nil {
		c.cache = make(map[schema.GroupVersionKind]map[types.NamespacedName]client.Object)
	}
	gvk := obj.GetObjectKind().GroupVersionKind()
	if _, ok := c.cache[gvk]; !ok {
		c.cache[gvk] = make(map[types.NamespacedName]client.Object)
	}
	c.cache[gvk][types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}] = obj
	return nil
}

func (c *cacheClient) Delete(_ context.Context, obj client.Object, opts ...client.DeleteOption) error {
	if c.cache == nil {
		return nil
	}
	gvk := obj.GetObjectKind().GroupVersionKind()
	if _, ok := c.cache[gvk]; !ok {
		return nil
	}
	delete(c.cache[gvk], types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()})
	return nil
}

func (c *cacheClient) Update(_ context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if c.cache == nil {
		return nil
	}
	gvk := obj.GetObjectKind().GroupVersionKind()
	if _, ok := c.cache[gvk]; !ok {
		return nil
	}
	c.cache[gvk][types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}] = obj
	return nil
}

func (c *cacheClient) Patch(_ context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	if c.cache == nil {
		return nil
	}
	gvk := obj.GetObjectKind().GroupVersionKind()
	if _, ok := c.cache[gvk]; !ok {
		return nil
	}
	switch patch.Type() {
	case types.JSONPatchType:
		patchBytes, err := patch.Data(obj)
		if err != nil {
			return err
		}
		patchObj := &jsonpatch.Patch{}
		if err := json.Unmarshal(patchBytes, patchObj); err != nil {
			return err
		}
		originalBytes, err := json.Marshal(obj)
		if err != nil {
			return err
		}
		modifiedBytes, err := patchObj.Apply(originalBytes)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(modifiedBytes, obj); err != nil {
			return err
		}
		c.cache[gvk][types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}] = obj
	}
	// TODO: handle other patch types
	return nil
}

func (c *cacheClient) DeleteAllOf(_ context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	if c.cache == nil {
		return nil
	}
	gvk := obj.GetObjectKind().GroupVersionKind()
	if _, ok := c.cache[gvk]; !ok {
		return nil
	}
	opt := &client.DeleteAllOfOptions{}
	opt.ApplyOptions(opts)
	for k, o := range c.cache[gvk] {
		if opt.Namespace != "" && o.GetNamespace() != opt.Namespace {
			continue
		}
		if opt.LabelSelector != nil && !opt.LabelSelector.Matches(labels.Set(o.GetLabels())) {
			continue
		}
		delete(c.cache[gvk], k)
	}
	return nil
}

func (c *cacheClient) Status() client.SubResourceWriter {
	//TODO implement me
	panic("implement me")
}

func (c *cacheClient) SubResource(_ string) client.SubResourceClient {
	return &nopSubResourceClient{}
}

func (c *cacheClient) Scheme() *runtime.Scheme {
	return nil
}

func (c *cacheClient) RESTMapper() meta.RESTMapper {
	return nil
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
func (c *CustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) error {
	comp, ok := obj.(*v1.Composition)
	if !ok {
		return xperrors.New("not a v1 Composition")
	}

	// Get the composition validation mode from annotation
	validationMode, err := comp.GetValidationMode()
	if err != nil {
		return xperrors.Wrap(err, "cannot get validation mode")
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
			return xperrors.Errorf("there were some errors while getting the needed CRDs: %v", errs)
		}
		// If any of the needed CRDs is not found, error out if strict mode is enabled, otherwise continue
		if validationMode == v1.CompositionValidationModeStrict {
			return xperrors.Wrap(err, "cannot get needed CRDs and strict mode is enabled")
		}
		if validationMode == v1.CompositionValidationModeLoose {
			looseModeSkip = true
		}
	}

	// Given that some requirement is missing, and we are in loose mode, skip the rest of the validation
	if looseModeSkip && validationMode == v1.CompositionValidationModeLoose {
		// TODO: emit a warning here
		return nil
	}

	// From here on we should refactor the code to allow using it from linters/Lsp
	if err := ValidateComposition(ctx, comp, gvkToCRDs, NewClientWithFallbackReader(&cacheClient{}, c.reader)); err != nil {
		return apierrors.NewBadRequest(err.Error())
	}
	return nil
}

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
		return xperrors.Errorf("there were some errors while validating the rendered resources:\n%s", errors.Join(validationErrs...))
	}
	if len(validationWarns) != 0 {
		fmt.Printf("there were some warnings while validating the rendered resources:\n%s", errors.Join(validationWarns...))
	}

	return nil
}
