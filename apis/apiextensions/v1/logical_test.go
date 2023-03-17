/*
Copyright 2022 The Crossplane Authors.

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

package v1

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/utils/pointer"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/test"
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
							Name: pointer.String("cool"),
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
							Name: pointer.String("cool"),
						},
						{
							Name: pointer.String("cooler"),
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
			for _, e := range got {
				if diff := cmp.Diff(tc.want, errors.New(e.Detail), test.EquateErrors()); diff != "" {
					t.Errorf("\nRejectFunctionsWithoutRequiredConfig(...): -want, +got:\n%s", diff)
				}
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
							Name: pointer.String("cool"),
						},
						{
							Name: pointer.String("cooler"),
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
							Name: pointer.String("cool"),
						},
						{
							Name: pointer.String("cool"),
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
			for _, e := range got {
				if diff := cmp.Diff(tc.want, errors.New(e.Detail), test.EquateErrors()); diff != "" {
					t.Errorf("\nRejectDuplicateNames(...): -want, +got:\n%s", diff)
				}
			}
		})
	}
}

func TestRejectAnonymousTemplatesWithFunctions(t *testing.T) {
	cases := map[string]struct {
		comp *Composition
		want error
	}{
		"AnonymousAndCompFnsNotInUse": {
			comp: &Composition{
				Spec: CompositionSpec{
					Resources: []ComposedTemplate{
						{
							// Anonymous
						},
						{
							// Anonymous
						},
					},
					// Functions array is empty.
				},
			},
			want: nil,
		},
		"AnonymousAndCompFnsInUse": {
			comp: &Composition{
				Spec: CompositionSpec{
					Resources: []ComposedTemplate{
						{
							// Anonymous
						},
						{
							// Anonymous
						},
					},
					Functions: []Function{{
						Name: "cool-fn",
					}},
				},
			},
			want: errors.New(errFnsRequireNames),
		},
		"NamedAndCompFnsInUse": {
			comp: &Composition{
				Spec: CompositionSpec{
					Resources: []ComposedTemplate{
						{
							Name: pointer.String("cool"),
						},
						{
							Name: pointer.String("cooler"),
						},
					},
					Functions: []Function{{
						Name: "cool-fn",
					}},
				},
			},
			want: nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := RejectAnonymousTemplatesWithFunctions(tc.comp)
			for _, e := range got {
				if diff := cmp.Diff(tc.want, errors.New(e.Detail), test.EquateErrors()); diff != "" {
					t.Errorf("\nRejectAnonymousTemplatesWithFunctions(...): -want, +got:\n%s", diff)
				}
			}
		})
	}
}

func TestRejectFunctionsWithoutRequiredConfig(t *testing.T) {
	cases := map[string]struct {
		comp *Composition
		want error
	}{
		"UnknownType": {
			comp: &Composition{
				Spec: CompositionSpec{
					Functions: []Function{{
						Type: "wat",
					}},
				},
			},
			want: errors.Errorf(ErrFmtUnknownFnType, "wat"),
		},
		"MissingContainerConfig": {
			comp: &Composition{
				Spec: CompositionSpec{
					Functions: []Function{{
						Type: FunctionTypeContainer,
					}},
				},
			},
			want: errors.New(ErrFnMissingContainerConfig),
		},
		"HasContainerConfig": {
			comp: &Composition{
				Spec: CompositionSpec{
					Functions: []Function{{
						Type: FunctionTypeContainer,
						Container: &ContainerFunction{
							Image: "example.org/coolimg",
						},
					}},
				},
			},
			want: nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := RejectFunctionsWithoutRequiredConfig(tc.comp)
			for _, e := range got {
				if diff := cmp.Diff(tc.want, errors.New(e.Detail), test.EquateErrors()); diff != "" {
					t.Errorf("\nRejectFunctionsWithoutRequiredConfig(...): -want, +got:\n%s", diff)
				}
			}
		})
	}
}
