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
	"fmt"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	v1 "github.com/crossplane/crossplane/apis/apiextensions/v1"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
)

// ValidateConnectionDetails validates the connection details of a composition. It only checks the
// FromFieldPath as that is the only one we are able to validate with certainty.
func ValidateConnectionDetails(comp *v1.Composition, gvkToCRD map[schema.GroupVersionKind]apiextensions.CustomResourceDefinition) (errs field.ErrorList) {
	for i, resource := range comp.Spec.Resources {
		gvk, err := resource.GetObjectGVK()
		if err != nil {
			return append(errs, field.InternalError(field.NewPath("spec", "resource").Index(i).Child("base"), errors.Wrap(err, "cannot get object gvk")))
		}
		crd, ok := gvkToCRD[gvk]
		if !ok {
			return append(errs, field.InternalError(
				field.NewPath("spec", "resource").Index(i).Child("base"),
				fmt.Errorf("crd for gvk %q not found", gvk),
			))
		}
		for j, con := range resource.ConnectionDetails {
			if con.FromFieldPath == nil {
				continue
			}
			_, _, err = validateFieldPath(crd.Spec.Validation.OpenAPIV3Schema, *con.FromFieldPath)
			if err != nil {
				errs = append(errs, field.Invalid(field.NewPath("spec", "resource").Index(i).Child("base").Child("connectionDetails").Index(j).Child("fromFieldPath"), *con.FromFieldPath, err.Error()))
			}
		}
	}

	return errs
}
