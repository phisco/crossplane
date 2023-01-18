package v1

import (
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"k8s.io/utils/pointer"
	"testing"
)

func TestRejectMixedTemplates(t *testing.T) {
	cases := map[string]struct {
		comp *Composition
		want error
	}{
		"Mixed": {
			comp: &Composition{
				Spec: CompositionSpec{
					Resources: []ComposedTemplate{
						{
							// Unnamed.
						},
						{
							Name: pointer.StringPtr("cool"),
						},
					},
				},
			},
			want: errors.New(errMixed),
		},
		"Anonymous": {
			comp: &Composition{
				Spec: CompositionSpec{
					Resources: []ComposedTemplate{
						{
							// Unnamed.
						},
						{
							// Unnamed.
						},
					},
				},
			},
			want: nil,
		},
		"Named": {
			comp: &Composition{
				Spec: CompositionSpec{
					Resources: []ComposedTemplate{
						{
							Name: pointer.StringPtr("cool"),
						},
						{
							Name: pointer.StringPtr("cooler"),
						},
					},
				},
			},
			want: nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := RejectMixedTemplates(tc.comp)
			if diff := cmp.Diff(tc.want, got, test.EquateErrors()); diff != "" {
				t.Errorf("\nRejectMixedTemplates(...): -want, +got:\n%s", diff)
			}
		})
	}
}

func TestRejectDuplicateNames(t *testing.T) {
	cases := map[string]struct {
		comp *Composition
		want error
	}{
		"Unique": {
			comp: &Composition{
				Spec: CompositionSpec{
					Resources: []ComposedTemplate{
						{
							Name: pointer.StringPtr("cool"),
						},
						{
							Name: pointer.StringPtr("cooler"),
						},
					},
				},
			},
			want: nil,
		},
		"Anonymous": {
			comp: &Composition{
				Spec: CompositionSpec{
					Resources: []ComposedTemplate{
						{
							// Unnamed.
						},
						{
							// Unnamed.
						},
					},
				},
			},
			want: nil,
		},
		"Duplicates": {
			comp: &Composition{
				Spec: CompositionSpec{
					Resources: []ComposedTemplate{
						{
							Name: pointer.StringPtr("cool"),
						},
						{
							Name: pointer.StringPtr("cool"),
						},
					},
				},
			},
			want: errors.New(errDuplicate),
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := RejectDuplicateNames(tc.comp)
			if diff := cmp.Diff(tc.want, got, test.EquateErrors()); diff != "" {
				t.Errorf("\nRejectDuplicateNames(...): -want, +got:\n%s", diff)
			}
		})
	}
}
