package v1

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/pointer"

	"github.com/crossplane/crossplane-runtime/pkg/test"
)

func TestComposition_validateResourceName(t *testing.T) {
	type fields struct {
		Spec CompositionSpec
	}
	tests := []struct {
		name     string
		fields   fields
		wantErrs field.ErrorList
	}{
		{
			name: "Valid: all named",
			fields: fields{
				Spec: CompositionSpec{
					Resources: []ComposedTemplate{
						{
							Name: pointer.String("foo"),
						},
						{
							Name: pointer.String("bar"),
						},
					},
				},
			},
		},
		{
			name: "Valid: all anonymous",
			fields: fields{
				Spec: CompositionSpec{
					Resources: []ComposedTemplate{
						{},
						{},
					},
				},
			},
		},
		{
			name: "Invalid: mixed names expecting anonymous",
			fields: fields{
				Spec: CompositionSpec{
					Resources: []ComposedTemplate{
						{},
						{Name: pointer.String("bar")},
					},
				},
			},
			wantErrs: field.ErrorList{
				{
					Type:     field.ErrorTypeInvalid,
					Field:    "spec.resources[1].name",
					BadValue: "bar",
				},
			},
		},
		{
			name: "Invalid: mixed names expecting named",
			fields: fields{
				Spec: CompositionSpec{
					Resources: []ComposedTemplate{
						{Name: pointer.String("bar")},
						{},
					},
				},
			},
			wantErrs: field.ErrorList{
				{
					Type:     field.ErrorTypeInvalid,
					Field:    "spec.resources[1].name",
					BadValue: "",
				},
			},
		},
		{
			name: "Valid: named with functions",
			fields: fields{
				Spec: CompositionSpec{
					Resources: []ComposedTemplate{
						{Name: pointer.String("foo")},
						{Name: pointer.String("bar")},
					},
					Functions: []Function{
						{
							Name: "baz",
						},
					},
				},
			},
		},
		{
			name: "Invalid: anonymous with functions",
			fields: fields{
				Spec: CompositionSpec{
					Resources: []ComposedTemplate{
						{},
					},
					Functions: []Function{
						{
							Name: "foo",
						},
					},
				},
			},
			wantErrs: field.ErrorList{
				{
					Type:     field.ErrorTypeInvalid,
					Field:    "spec.resources[0].name",
					BadValue: "",
				},
			},
		},
		{
			name: "Invalid: duplicate names",
			fields: fields{
				Spec: CompositionSpec{
					Resources: []ComposedTemplate{
						{Name: pointer.String("foo")},
						{Name: pointer.String("bar")},
						{Name: pointer.String("foo")},
					},
				},
			},
			wantErrs: field.ErrorList{
				{
					Type:     field.ErrorTypeDuplicate,
					Field:    "spec.resources[2].name",
					BadValue: "foo",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Composition{
				Spec: tt.fields.Spec,
			}
			gotErrs := c.validateResourceNames()
			for _, err := range gotErrs {
				err.Detail = ""
			}
			if diff := cmp.Diff(tt.wantErrs, gotErrs, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nvalidateResourceName(...): -want error, +got error: \n%s", tt.name, diff)
			}
		})
	}
}
