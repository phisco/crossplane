package composition

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/pointer"

	v1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
	"github.com/crossplane/crossplane/pkg/validation/errors"
)

func TestValidateComposition(t *testing.T) {
	type args struct {
		comp      *v1.Composition
		gvkToCRDs map[schema.GroupVersionKind]apiextensions.CustomResourceDefinition
	}
	type want struct {
		errs field.ErrorList
	}
	tests := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"AcceptStrictNoCRDsNoPatches": {
			reason: "Should accept a Composition if no CRDs are available, but no patches are defined",
			want: want{
				errs: nil,
			},
			args: args{
				comp:      buildDefaultComposition(t, v1.CompositionValidationModeStrict, map[string]any{"someOtherField": "test"}),
				gvkToCRDs: nil,
			},
		},
		"RejectStrictNoCRDsWithPatches": {
			reason: "Should accept a Composition if no CRDs are available, but no patches are defined",
			want: want{
				errs: field.ErrorList{
					{
						Type:  field.ErrorTypeInternal,
						Field: "spec.resources[0]",
					},
				},
			},
			args: args{
				comp: buildDefaultComposition(t, v1.CompositionValidationModeStrict, map[string]any{"someOtherField": "test"},
					withPatches(0, v1.Patch{
						FromFieldPath: pointer.String("spec.someField"),
						ToFieldPath:   pointer.String("spec.someOtherField"),
					})),
				gvkToCRDs: nil,
			},
		},
		"AcceptStrictAllCRDs": {
			reason: "Should accept a valid Composition if all CRDs are available",
			want:   want{errs: nil},
			args: args{
				gvkToCRDs: defaultGVKToCRDs(),
				comp:      buildDefaultComposition(t, v1.CompositionValidationModeStrict, map[string]any{"someOtherField": "test"}),
			},
		},
		"AcceptStrictInvalid": {
			reason: "Should accept a Composition not defining a required field in a resource if all CRDs are available",
			// TODO(phisco): this should return an error once we implement rendered validation
			want: want{errs: nil},
			args: args{
				gvkToCRDs: defaultGVKToCRDs(),
				comp:      buildDefaultComposition(t, v1.CompositionValidationModeStrict, nil),
			},
		},
		"AcceptStrictRequiredFieldByPatch": {
			reason: "Should accept a Composition with a required field defined only by a patch if all CRDs are available",
			want:   want{errs: nil},
			args: args{
				gvkToCRDs: defaultGVKToCRDs(),
				comp: buildDefaultComposition(t, v1.CompositionValidationModeStrict, nil, withPatches(0, v1.Patch{
					Type:          v1.PatchTypeFromCompositeFieldPath,
					FromFieldPath: pointer.String("spec.someField"),
					ToFieldPath:   pointer.String("spec.someOtherField"),
				})),
			},
		},
		"RejectStrictInvalidFromFieldPath": {
			reason: "Should reject a Composition with a patch using a field not allowed by the the Composite resource, if all CRDs are found",
			want: want{
				errs: field.ErrorList{
					{
						Type:  field.ErrorTypeInvalid,
						Field: "spec.resources[0].patches[0].fromFieldPath",
					},
				},
			},
			args: args{
				gvkToCRDs: defaultGVKToCRDs(),
				comp: buildDefaultComposition(t, v1.CompositionValidationModeStrict, nil, withPatches(0, v1.Patch{
					Type:          v1.PatchTypeFromCompositeFieldPath,
					FromFieldPath: pointer.String("spec.someWrongField"),
					ToFieldPath:   pointer.String("spec.someOtherField"),
				})),
			},
		},
		"RejectStrictInvalidToFieldPath": {
			reason: "Should reject a Composition with a patch using a field not allowed by the schema of the Managed resource, if all CRDs are found",
			want: want{
				errs: field.ErrorList{
					{
						Type:  field.ErrorTypeInvalid,
						Field: "spec.resources[0].patches[0].toFieldPath",
					},
				},
			},
			args: args{
				gvkToCRDs: defaultGVKToCRDs(),
				comp: buildDefaultComposition(t, v1.CompositionValidationModeStrict, map[string]any{"someOtherField": "test"}, withPatches(0, v1.Patch{
					Type:          v1.PatchTypeFromCompositeFieldPath,
					FromFieldPath: pointer.String("spec.someField"),
					ToFieldPath:   pointer.String("spec.someOtherWrongField"),
				})),
			},
		},
		"RejectStrictPatchMismatchTypes": {
			reason: "Should reject a Composition with a patch between two different types, if all CRDs are found",
			want: want{
				errs: field.ErrorList{
					{
						Type:  field.ErrorTypeRequired,
						Field: "spec.resources[0].patches[0].transforms",
					},
				},
			},
			args: args{
				gvkToCRDs: buildGvkToCRDs(
					defaultCompositeCrdBuilder().withOption(func(crd *extv1.CustomResourceDefinition) {
						crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"].Properties["someField"] = extv1.JSONSchemaProps{
							Type: "integer",
						}
					}).build(),
					defaultManagedCrdBuilder().build(),
				),
				comp: buildDefaultComposition(t, v1.CompositionValidationModeStrict, nil, withPatches(0, v1.Patch{
					Type:          v1.PatchTypeFromCompositeFieldPath,
					FromFieldPath: pointer.String("spec.someField"),
					ToFieldPath:   pointer.String("spec.someOtherField"),
				})),
			},
		},
		"RejectStrictPatchMismatchTypeWithMathTransform": {
			reason: "Should reject a Composition with a math transformation resulting in the wrong final type, if validation mode is strict and all CRDs are found",
			want: want{
				errs: field.ErrorList{
					{
						Type:  field.ErrorTypeInvalid,
						Field: "spec.resources[0].patches[0].transforms[0]",
					},
				},
			},
			args: args{
				gvkToCRDs: defaultGVKToCRDs(),
				comp: buildDefaultComposition(t, v1.CompositionValidationModeLoose, nil, withPatches(0, v1.Patch{
					Type:          v1.PatchTypeFromCompositeFieldPath,
					FromFieldPath: pointer.String("spec.someField"),
					ToFieldPath:   pointer.String("spec.someOtherField"),
					Transforms: []v1.Transform{{
						Type: v1.TransformTypeMath,
						Math: &v1.MathTransform{
							Multiply: pointer.Int64(int64(2)),
						},
					}},
				})),
			},
		},
		"RejectStrictPatchMismatchTypeWithConvertTransform": {
			reason: "Should reject a Composition with a convert transformation resulting in the wrong final type, if all CRDs are found",
			want: want{
				errs: field.ErrorList{
					{
						Type:  field.ErrorTypeInvalid,
						Field: "spec.resources[0].patches[0].transforms",
					},
				},
			},
			args: args{
				gvkToCRDs: defaultGVKToCRDs(),
				comp: buildDefaultComposition(t, v1.CompositionValidationModeLoose, nil, withPatches(0, v1.Patch{
					Type:          v1.PatchTypeFromCompositeFieldPath,
					FromFieldPath: pointer.String("spec.someField"),
					ToFieldPath:   pointer.String("spec.someOtherField"),
					Transforms: []v1.Transform{{
						Type: v1.TransformTypeConvert,
						Convert: &v1.ConvertTransform{
							ToType: "int64",
						},
					}},
				})),
			},
		},
		"AcceptStrictPatchWithCombinePatch": {
			reason: "Should accept a Composition with a combine patch, if all CRDs are found",
			args: args{
				gvkToCRDs: buildGvkToCRDs(
					defaultCompositeCrdBuilder().withOption(func(crd *extv1.CustomResourceDefinition) {
						spec := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
						spec.Properties["someOtherOtherField"] = extv1.JSONSchemaProps{
							Type: "string",
						}

						spec.Required = append(spec.Required,
							"someOtherOtherField")
						crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"] = spec
					}).build(),
					defaultManagedCrdBuilder().build(),
				),
				comp: buildDefaultComposition(t, v1.CompositionValidationModeLoose, nil, withPatches(0, v1.Patch{
					Type: v1.PatchTypeCombineFromComposite,
					Combine: &v1.Combine{
						Variables: []v1.CombineVariable{
							{
								FromFieldPath: "spec.someField",
							},
							{
								FromFieldPath: "spec.someOtherOtherField",
							},
						},
						Strategy: v1.CombineStrategyString,
						String: &v1.StringCombine{
							Format: "%s-%s",
						},
					},
					ToFieldPath: pointer.String("spec.someOtherField"),
				})),
			},
		},
		"RejectStrictPatchWithCombinePatchMissingRequiredField": {
			reason: "Should reject a Composition with a combine patch with mismatched required fields, if all CRDs are found",
			want: want{
				errs: field.ErrorList{
					{
						Type:  field.ErrorTypeInvalid,
						Field: "spec.resources[0].patches[0].combine",
					},
				},
			},
			args: args{
				gvkToCRDs: buildGvkToCRDs(
					defaultCompositeCrdBuilder().withOption(func(crd *extv1.CustomResourceDefinition) {
						spec := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
						spec.Properties["someNonReqField"] = extv1.JSONSchemaProps{
							Type: "string",
						}
					}).build(),
					defaultManagedCrdBuilder().build(),
				),
				comp: buildDefaultComposition(t, v1.CompositionValidationModeLoose, nil, withPatches(0, v1.Patch{
					Type: v1.PatchTypeCombineFromComposite,
					Combine: &v1.Combine{
						Variables: []v1.CombineVariable{
							{
								FromFieldPath: "spec.someField",
							},
							{
								FromFieldPath: "spec.someNonReqField",
							},
						},
						Strategy: v1.CombineStrategyString,
						String: &v1.StringCombine{
							Format: "%s-%s",
						},
					},
					ToFieldPath: pointer.String("spec.someOtherField"),
				})),
			},
		},
		"RejectStrictPatchWithCombinePatchMissingField": {
			reason: "Should reject a Composition with a combine patch with missing fields, if validation mode is strict and all CRDs are found",
			want: want{
				errs: field.ErrorList{
					{
						Type:  field.ErrorTypeInvalid,
						Field: "spec.resources[0].patches[0].combine",
					},
				},
			},
			args: args{
				gvkToCRDs: defaultGVKToCRDs(),
				comp: buildDefaultComposition(t, v1.CompositionValidationModeStrict, nil, withPatches(0, v1.Patch{
					Type: v1.PatchTypeCombineFromComposite,
					Combine: &v1.Combine{
						Variables: []v1.CombineVariable{
							{
								FromFieldPath: "spec.someField",
							},
							{
								FromFieldPath: "spec.someNonDefinedField",
							},
						},
						Strategy: v1.CombineStrategyString,
						String: &v1.StringCombine{
							Format: "%s-%s",
						},
					},
					ToFieldPath: pointer.String("spec.someOtherField"),
				})),
			},
		},
		"AcceptEnvironmentConfigPatchUnsupported": {
			reason: "Should accept Composition using an EnvironmentConfig related PatchType, if all CRDs are found",
			want: want{
				errs: nil,
			},
			args: args{
				gvkToCRDs: defaultGVKToCRDs(),
				comp: buildDefaultComposition(t, v1.CompositionValidationModeLoose, nil, withPatches(0, v1.Patch{
					Type:          v1.PatchTypeFromEnvironmentFieldPath,
					FromFieldPath: pointer.String("spec.someField"),
					ToFieldPath:   pointer.String("spec.someOtherField"),
				})),
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			v, err := NewValidator(WithCRDGetterFromMap(tc.args.gvkToCRDs))
			if err != nil {
				t.Errorf("NewValidator(...) = %v", err)
				return
			}
			_, got := v.Validate(context.TODO(), tc.args.comp)
			if diff := cmp.Diff(tc.want.errs, got, errors.SortFieldErrors(), cmpopts.IgnoreFields(field.Error{}, "Detail", "BadValue")); diff != "" {
				t.Errorf("%s\nValidate(...) = -want, +got\n%s", tc.reason, diff)
			}
		})
	}
}

const testGroup = "resources.test.com"
const testGroupSingular = "resource.test.com"

func marshalJSON(t *testing.T, obj interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(obj)
	if err != nil {
		t.Errorf("Failed to marshal object: %v", err)
	}
	return b
}

func toPointer[T any](v T) *T {
	return &v
}

func defaultCompositeCrdBuilder() *crdBuilder {
	return newCRDBuilder("Composite", "v1").withOption(specSchemaOption("v1", extv1.JSONSchemaProps{
		Type: "object",
		Required: []string{
			"someField",
		},
		Properties: map[string]extv1.JSONSchemaProps{
			"someField": {
				Type: "string",
			},
			"someNonRequiredField": {
				Type: "string",
			},
		},
	}))
}

func defaultManagedCrdBuilder() *crdBuilder {
	return newCRDBuilder("Managed", "v1").withOption(specSchemaOption("v1", extv1.JSONSchemaProps{
		Type: "object",
		Required: []string{
			"someOtherField",
		},
		Properties: map[string]extv1.JSONSchemaProps{
			"someOtherField": {
				Type: "string",
			},
		},
	}))
}

func defaultGVKToCRDs() map[schema.GroupVersionKind]apiextensions.CustomResourceDefinition {
	crds := []apiextensions.CustomResourceDefinition{*defaultManagedCrdBuilder().build(), *defaultCompositeCrdBuilder().build()}
	m := make(map[schema.GroupVersionKind]apiextensions.CustomResourceDefinition, len(crds))
	for _, crd := range crds {
		crd := crd
		m[schema.GroupVersionKind{
			Group:   crd.Spec.Group,
			Version: crd.Spec.Versions[0].Name,
			Kind:    crd.Spec.Names.Kind,
		}] = crd
	}
	return m
}

func defaultCRDs() []runtime.Object {
	return []runtime.Object{defaultManagedCrdBuilder().buildExtV1(), defaultCompositeCrdBuilder().buildExtV1()}
}

type builderOption func(*extv1.CustomResourceDefinition)

type crdBuilder struct {
	kind, version string
	opts          []builderOption
}

func newCRDBuilder(kind, version string) *crdBuilder {
	return &crdBuilder{kind: kind, version: version}
}

func specSchemaOption(version string, schema extv1.JSONSchemaProps) builderOption {
	return func(crd *extv1.CustomResourceDefinition) {
		var storageFound bool
		for i, definitionVersion := range crd.Spec.Versions {
			storageFound = storageFound || definitionVersion.Storage
			if definitionVersion.Name == version {
				crd.Spec.Versions[i].Schema = &extv1.CustomResourceValidation{
					OpenAPIV3Schema: &extv1.JSONSchemaProps{
						Type: "object",
						Required: []string{
							"spec",
						},
						Properties: map[string]extv1.JSONSchemaProps{
							"spec": schema,
						},
					},
				}
				return
			}
		}
		crd.Spec.Versions = append(crd.Spec.Versions, extv1.CustomResourceDefinitionVersion{
			Name:    version,
			Served:  true,
			Storage: !storageFound,
			Schema: &extv1.CustomResourceValidation{
				OpenAPIV3Schema: &extv1.JSONSchemaProps{
					Type: "object",
					Required: []string{
						"spec",
					},
					Properties: map[string]extv1.JSONSchemaProps{
						"spec": schema,
					},
				},
			},
		})
	}
}

func (b *crdBuilder) withOption(f builderOption) *crdBuilder {
	b.opts = append(b.opts, f)
	return b
}

func (b *crdBuilder) build() *apiextensions.CustomResourceDefinition {
	internal := &apiextensions.CustomResourceDefinition{}
	_ = extv1.Convert_v1_CustomResourceDefinition_To_apiextensions_CustomResourceDefinition(b.buildExtV1(), internal, nil)
	return internal
}

func (b *crdBuilder) buildExtV1() *extv1.CustomResourceDefinition {
	crd := &extv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: strings.ToLower(b.kind) + "s." + testGroupSingular,
		},
		Spec: extv1.CustomResourceDefinitionSpec{
			Group: testGroup,
			Names: extv1.CustomResourceDefinitionNames{
				Kind: b.kind,
			},
			Versions: []extv1.CustomResourceDefinitionVersion{
				{
					Name:    b.version,
					Served:  true,
					Storage: true,
				},
			},
		},
	}
	for _, opt := range b.opts {
		opt(crd)
	}
	return crd
}

type compositionBuilderOption func(c *v1.Composition)

func withPatches(index int, patches ...v1.Patch) compositionBuilderOption {
	return func(c *v1.Composition) {
		c.Spec.Resources[index].Patches = patches
	}
}

func buildDefaultComposition(t *testing.T, validationMode v1.CompositionValidationMode, spec map[string]any, opts ...compositionBuilderOption) *v1.Composition {
	t.Helper()
	if spec == nil {
		spec = map[string]any{}
	}
	c := &v1.Composition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testComposition",
			Annotations: map[string]string{
				v1.CompositionValidationModeAnnotation: string(validationMode),
			},
		},
		Spec: v1.CompositionSpec{
			CompositeTypeRef: v1.TypeReference{
				APIVersion: testGroup + "/v1",
				Kind:       "Composite",
			},
			Resources: []v1.ComposedTemplate{
				{
					Name: toPointer("test"),
					Base: runtime.RawExtension{
						Raw: marshalJSON(t, map[string]any{
							"apiVersion": testGroup + "/v1",
							"kind":       "Managed",
							"metadata": map[string]any{
								"name":      "test",
								"namespace": "testns",
							},
							"spec": spec,
						}),
					},
				},
			},
		},
	}

	for _, opt := range opts {
		opt(c)
	}
	return c
}

func buildGvkToCRDs(crds ...*apiextensions.CustomResourceDefinition) map[schema.GroupVersionKind]apiextensions.CustomResourceDefinition {
	m := map[schema.GroupVersionKind]apiextensions.CustomResourceDefinition{}
	for _, crd := range crds {
		if crd == nil {
			continue
		}
		if len(crd.Spec.Versions) == 0 {
			m[schema.GroupVersionKind{
				Group:   crd.Spec.Group,
				Version: crd.Spec.Version,
				Kind:    crd.Spec.Names.Kind,
			}] = *crd
			continue
		}
		for _, version := range crd.Spec.Versions {
			m[schema.GroupVersionKind{
				Group:   crd.Spec.Group,
				Version: version.Name,
				Kind:    crd.Spec.Names.Kind,
			}] = *crd
		}
	}
	return m
}
