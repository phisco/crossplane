package composition

import (
	"context"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/validation"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/crossplane/crossplane/apis"
	v1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
)

func TestValidateComposition(t *testing.T) {
	type args struct {
		comp      *v1.Composition
		gvkToCRDs map[schema.GroupVersionKind]apiextensions.CustomResourceDefinition
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name:    "Should reject a Composition if no CRDs are available",
			wantErr: true,
			args: args{
				comp:      buildDefaultComposition(t, v1.CompositionValidationModeStrict, map[string]any{"someOtherField": "test"}),
				gvkToCRDs: nil,
			},
		}, {
			name:    "Should accept a valid Composition if all CRDs are available",
			wantErr: false,
			args: args{
				gvkToCRDs: defaultGVKToCRDs(),
				comp:      buildDefaultComposition(t, v1.CompositionValidationModeStrict, map[string]any{"someOtherField": "test"}),
			},
		}, {
			name:    "Should reject a Composition not defining a required field in a resource if all CRDs are available",
			wantErr: true,
			args: args{
				gvkToCRDs: defaultGVKToCRDs(),
				comp:      buildDefaultComposition(t, v1.CompositionValidationModeStrict, nil),
			},
		}, {
			name:    "Should accept a Composition with a required field defined only by a patch if all CRDs are available",
			wantErr: false,
			args: args{
				gvkToCRDs: defaultGVKToCRDs(),
				comp: buildDefaultComposition(t, v1.CompositionValidationModeStrict, nil, v1.Patch{
					Type:          v1.PatchTypeFromCompositeFieldPath,
					FromFieldPath: toPointer("spec.someField"),
					ToFieldPath:   toPointer("spec.someOtherField"),
				}),
			},
		}, {
			name:    "Should reject a Composition with a patch using a field not allowed by the the Composite resource, if all CRDs are found",
			wantErr: true,
			args: args{
				gvkToCRDs: defaultGVKToCRDs(),
				comp: buildDefaultComposition(t, v1.CompositionValidationModeStrict, nil, v1.Patch{
					Type:          v1.PatchTypeFromCompositeFieldPath,
					FromFieldPath: toPointer("spec.someWrongField"),
					ToFieldPath:   toPointer("spec.someOtherField"),
				}),
			},
		}, {
			name:    "Should reject a Composition with a patch using a field not allowed by the schema of the Managed resource, if all CRDs are found",
			wantErr: true,
			args: args{
				gvkToCRDs: defaultGVKToCRDs(),
				comp: buildDefaultComposition(t, v1.CompositionValidationModeStrict, map[string]any{"someOtherField": "test"}, v1.Patch{
					Type:          v1.PatchTypeFromCompositeFieldPath,
					FromFieldPath: toPointer("spec.someField"),
					ToFieldPath:   toPointer("spec.someOtherWrongField"),
				}),
			},
		}, {
			name:    "Should reject a Composition with a patch between two different types, if all CRDs are found",
			wantErr: true,
			args: args{
				gvkToCRDs: buildGvkToCRDs(
					defaultCompositeCrdBuilder().withOption(func(crd *extv1.CustomResourceDefinition) {
						crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"].Properties["someField"] = extv1.JSONSchemaProps{
							Type: "integer",
						}
					}).build(),
					defaultManagedCrdBuilder().build(),
				),
				comp: buildDefaultComposition(t, v1.CompositionValidationModeStrict, nil, v1.Patch{
					Type:          v1.PatchTypeFromCompositeFieldPath,
					FromFieldPath: toPointer("spec.someField"),
					ToFieldPath:   toPointer("spec.someOtherField"),
				}),
			},
		}, {
			name:    "Should reject a Composition with a math transformation resulting in the wrong final type, if validation mode is strict and all CRDs are found",
			wantErr: true,
			args: args{
				gvkToCRDs: defaultGVKToCRDs(),
				comp: buildDefaultComposition(t, v1.CompositionValidationModeLoose, nil, v1.Patch{
					Type:          v1.PatchTypeFromCompositeFieldPath,
					FromFieldPath: toPointer("spec.someField"),
					ToFieldPath:   toPointer("spec.someOtherField"),
					Transforms: []v1.Transform{{
						Type: v1.TransformTypeMath,
						Math: &v1.MathTransform{
							Multiply: toPointer(int64(2)),
						},
					}},
				}),
			},
		},
		{
			name:    "Should reject a Composition with a convert transformation resulting in the wrong final type, if all CRDs are found",
			wantErr: true,
			args: args{
				gvkToCRDs: defaultGVKToCRDs(),
				comp: buildDefaultComposition(t, v1.CompositionValidationModeLoose, nil, v1.Patch{
					Type:          v1.PatchTypeFromCompositeFieldPath,
					FromFieldPath: toPointer("spec.someField"),
					ToFieldPath:   toPointer("spec.someOtherField"),
					Transforms: []v1.Transform{{
						Type: v1.TransformTypeConvert,
						Convert: &v1.ConvertTransform{
							ToType: "int64",
						},
					}},
				}),
			},
		},
		{
			name: "Should accept a Composition with a combine patch, if all CRDs are found",
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
				comp: buildDefaultComposition(t, v1.CompositionValidationModeLoose, nil, v1.Patch{
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
					ToFieldPath: toPointer("spec.someOtherField"),
				}),
			},
		},
		{
			name:    "Should reject a Composition with a combine patch with mismatched required fields, if all CRDs are found",
			wantErr: true,
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
				comp: buildDefaultComposition(t, v1.CompositionValidationModeLoose, nil, v1.Patch{
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
					ToFieldPath: toPointer("spec.someOtherField"),
				}),
			},
		},
		{
			name:    "Should reject a Composition with a combine patch with missing fields, if validation mode is strict and all CRDs are found",
			wantErr: true,
			args: args{
				gvkToCRDs: buildGvkToCRDs(
					defaultCompositeCrdBuilder().build(),
					defaultManagedCrdBuilder().build(),
				),
				comp: buildDefaultComposition(t, v1.CompositionValidationModeLoose, nil, v1.Patch{
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
					ToFieldPath: toPointer("spec.someOtherField"),
				}),
			},
		},
	}
	commonSetup := func() *fake.ClientBuilder {
		s := runtime.NewScheme()
		_ = apis.AddToScheme(s)
		return fake.NewClientBuilder().
			WithScheme(s)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientWithFallbackReader := validation.NewClientWithFallbackReader(commonSetup().Build(), commonSetup().Build())
			if err := ValidateComposition(context.TODO(), tt.args.comp, tt.args.gvkToCRDs, clientWithFallbackReader); (err != nil) != tt.wantErr {
				t.Errorf("ValidateComposition() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
