/*
Copyright 2024 The Crossplane Authors.

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

package validate

import (
	"context"
	"fmt"

	ext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	runtimeschema "k8s.io/apimachinery/pkg/runtime/schema"
	celconfig "k8s.io/apiserver/pkg/apis/cel"

	"github.com/crossplane/crossplane-runtime/pkg/errors"

	"github.com/crossplane/crossplane/internal/controller/apiextensions/composite"
)

func newValidatorsAndStructurals(crds []*extv1.CustomResourceDefinition) (map[runtimeschema.GroupVersionKind][]*validation.SchemaValidator, map[runtimeschema.GroupVersionKind]*schema.Structural, error) {
	validators := map[runtimeschema.GroupVersionKind][]*validation.SchemaValidator{}
	structurals := map[runtimeschema.GroupVersionKind]*schema.Structural{}

	for i := range crds {
		internal := &ext.CustomResourceDefinition{}
		if err := extv1.Convert_v1_CustomResourceDefinition_To_apiextensions_CustomResourceDefinition(crds[i], internal, nil); err != nil {
			return nil, nil, err
		}

		// Top-level and per-version schemas are mutually exclusive.
		for _, ver := range internal.Spec.Versions {
			gvk := runtimeschema.GroupVersionKind{
				Group:   internal.Spec.Group,
				Version: ver.Name,
				Kind:    internal.Spec.Names.Kind,
			}

			// Top level validation rules
			var s *ext.JSONSchemaProps
			switch {
			case internal.Spec.Validation != nil:
				s = internal.Spec.Validation.OpenAPIV3Schema
			case ver.Schema != nil && ver.Schema.OpenAPIV3Schema != nil:
				s = ver.Schema.OpenAPIV3Schema
			default:
				// TODO log a warning here, it should never happen
				continue
			}
			sv, _, err := validation.NewSchemaValidator(s)
			if err != nil {
				return nil, nil, err
			}

			structural, err := schema.NewStructural(s)
			if err != nil {
				return nil, nil, err
			}
			if err != nil {
				return nil, nil, err
			}
			validators[gvk] = append(validators[gvk], &sv)
			structurals[gvk] = structural
		}

	}

	return validators, structurals, nil
}

// SchemaValidation validates the resources against the given CRDs
func SchemaValidation(resources []*unstructured.Unstructured, crds []*extv1.CustomResourceDefinition, skipSuccessLogs bool) error {
	schemaValidators, structurals, err := newValidatorsAndStructurals(crds)
	if err != nil {
		return errors.Wrap(err, "cannot create schema validators")
	}

	failure, warning := 0, 0

	for i, r := range resources {
		gvk := r.GetObjectKind().GroupVersionKind()
		sv, ok := schemaValidators[gvk]
		if !ok {
			warning++
			fmt.Println("[!] could not find CRD/XRD for: " + r.GroupVersionKind().String())
			continue
		}

		rf := 0

		for _, v := range sv {
			re := validation.ValidateCustomResource(nil, r, *v)
			for _, e := range re {
				rf++
				fmt.Printf("[x] validation error %s, %s : %s\n", r.GroupVersionKind().String(), r.GetAnnotations()[composite.AnnotationKeyCompositionResourceName], e.Error())
			}
		}

		s := structurals[gvk]
		celValidator := cel.NewValidator(s, true, celconfig.RuntimeCELCostBudget)
		re, _ := celValidator.Validate(context.TODO(), nil, s, resources[i].Object, nil, celconfig.RuntimeCELCostBudget)
		for _, e := range re {
			rf++
			fmt.Printf("[x] CEL validation error %s, %s : %s\n", r.GroupVersionKind().String(), r.GetAnnotations()[composite.AnnotationKeyCompositionResourceName], e.Error())
		}

		if rf == 0 && !skipSuccessLogs {
			fmt.Printf("[✓] %s, %s validated successfully\n", r.GroupVersionKind().String(), r.GetAnnotations()[composite.AnnotationKeyCompositionResourceName])
		} else {
			failure++
		}
	}

	fmt.Printf("%d error, %d warning, %d success cases\n", failure, warning, len(resources)-failure-warning)

	if failure > 0 {
		return errors.New("could not validate all resources")
	}

	return nil
}
