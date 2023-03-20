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
	"testing"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apimachinery/pkg/runtime/schema"

	v1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
)

func withConnectionDetails(index int, cds ...v1.ConnectionDetail) compositionBuilderOption {
	return func(c *v1.Composition) {
		c.Spec.Resources[index].ConnectionDetails = cds
	}
}

func TestValidateConnectionDetails(t *testing.T) {
	type args struct {
		comp     *v1.Composition
		gvkToCRD map[schema.GroupVersionKind]apiextensions.CustomResourceDefinition
	}
	tests := []struct {
		name     string
		args     args
		wantErrs bool
	}{
		{
			name: "should accept empty connection details",
			args: args{
				comp:     buildDefaultComposition(t, v1.CompositionValidationModeLoose, nil),
				gvkToCRD: defaultGVKToCRDs(),
			},
			wantErrs: false,
		},
		{
			name: "should accept valid connection details",
			args: args{
				comp: buildDefaultComposition(
					t,
					v1.CompositionValidationModeLoose,
					nil,
					withConnectionDetails(0, v1.ConnectionDetail{FromFieldPath: toPointer("spec.someOtherField")}),
				),
				gvkToCRD: defaultGVKToCRDs(),
			},
			wantErrs: false,
		},
		{
			name: "should reject invalid connection details fromFieldPath",
			args: args{
				comp: buildDefaultComposition(
					t,
					v1.CompositionValidationModeLoose,
					nil,
					withConnectionDetails(0, v1.ConnectionDetail{FromFieldPath: toPointer("invalid")}),
				),
				gvkToCRD: defaultGVKToCRDs(),
			},
			wantErrs: true,
			//wantErrs: field.Invalid(field.NewPath("spec", "resources").Index(0).Child("base"), "invalid", fmt.Sprintf("unable to find field: invalid")),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotErrs := validateConnectionDetailsWithSchemas(tt.args.comp, tt.args.gvkToCRD); (len(gotErrs) != 0) != tt.wantErrs {
				t.Errorf("validateConnectionDetailsWithSchemas() = %v, want %v", gotErrs, tt.wantErrs)
			}
		})
	}
}
