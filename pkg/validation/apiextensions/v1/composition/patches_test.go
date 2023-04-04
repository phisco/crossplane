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
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/utils/pointer"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/pkg/test"

	v1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
	"github.com/crossplane/crossplane/pkg/validation/schema"

	_ "embed"
)

var (
	// got running `kubectl get crds -o json openidconnectproviders.iam.aws.crossplane.io  | jq '.spec.versions[0].schema.openAPIV3Schema |del(.. | .description?)'`
	// from provider: xpkg.upbound.io/crossplane-contrib/provider-aws:v0.38.0
	//go:embed fixtures/complex_schema_openidconnectproviders_v1beta1.json
	complexSchemaOpenIDConnectProvidersV1beta1      []byte
	complexSchemaOpenIDConnectProvidersV1beta1Props = toJSONSchemaProps(complexSchemaOpenIDConnectProvidersV1beta1)
)

func toJSONSchemaProps(in []byte) *apiextensions.JSONSchemaProps {
	p := extv1.JSONSchemaProps{}
	err := json.Unmarshal(in, &p)
	if err != nil {
		panic(err)
	}
	out := apiextensions.JSONSchemaProps{}
	if err := extv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(&p, &out, nil); err != nil {
		panic(err)
	}
	return &out
}

func Test_validateTransforms(t *testing.T) {
	type args struct {
		transforms       []v1.Transform
		fromType, toType schema.KnownJSONType
	}
	type want struct {
		err bool
	}
	tests := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"AcceptEmptyTransformsSameType": {
			reason: "Should accept empty transforms to the same type successfully",
			args: args{
				transforms: []v1.Transform{},
				fromType:   "string",
				toType:     "string",
			},
		},
		"AcceptNilTransformsSameType": {
			reason: "Should accept if no transforms are provided and the types are the same",
			want:   want{err: false},
			args: args{
				transforms: nil,
				fromType:   "string",
				toType:     "string",
			},
		},
		"RejectEmptyTransformsWrongTypes": {
			reason: "Should reject empty transforms to a different type",
			want:   want{err: true},
			args: args{
				transforms: []v1.Transform{},
				fromType:   "string",
				toType:     "integer",
			},
		},
		"RejectNilTransformsWrongTypes": {
			reason: "Should reject if no transforms are provided and the types are not the same",
			want:   want{err: true},
			args: args{
				transforms: nil,
				fromType:   "string",
				toType:     "integer",
			},
		},
		"AcceptEmptyTransformsCompatibleTypes": {
			reason: "Should accept empty transforms to a different type when its integer to number",
			want:   want{err: false},
			args: args{
				transforms: []v1.Transform{},
				fromType:   "integer",
				toType:     "number",
			},
		},
		"AcceptNilTransformsCompatibleTypes": {
			reason: "Should accept if no transforms are provided and the types are not the same but the types are integer and number",
			want:   want{err: false},
			args: args{
				transforms: nil,
				fromType:   "integer",
				toType:     "number",
			},
		},
		"AcceptConvertTransforms": {
			reason: "Should accept convert transforms successfully",
			args: args{
				transforms: []v1.Transform{
					{
						Type: v1.TransformTypeConvert,
						Convert: &v1.ConvertTransform{
							ToType: "int64",
						},
					},
				},
				fromType: "string",
				toType:   "integer",
			},
		},
		"AcceptConvertTransformsMultiple": {
			reason: "Should accept convert integer to number transforms successfully",
			args: args{
				transforms: []v1.Transform{
					{
						Type: v1.TransformTypeConvert,
						Convert: &v1.ConvertTransform{
							ToType: "float64",
						},
					},
					{
						Type: v1.TransformTypeConvert,
						Convert: &v1.ConvertTransform{
							ToType: "int64",
						},
					},
				},
				fromType: "string",
				toType:   "number",
			},
		},
		"RejectConvertTransformsNumberToInteger": {
			reason: "Should reject convert number to integer transforms",
			want:   want{err: true},
			args: args{
				transforms: []v1.Transform{
					{
						Type: v1.TransformTypeConvert,
						Convert: &v1.ConvertTransform{
							ToType: "int64",
						},
					},
					{
						Type: v1.TransformTypeConvert,
						Convert: &v1.ConvertTransform{
							ToType: "float64",
						},
					},
				},
				fromType: "string",
				toType:   "integer",
			},
		},
		"AcceptValidChainedConvertTransforms": {
			reason: "Should accept valid chained convert transforms",
			args: args{
				transforms: []v1.Transform{
					{
						Type: v1.TransformTypeConvert,
						Convert: &v1.ConvertTransform{
							ToType: "int64",
						},
					},
					{
						Type: v1.TransformTypeConvert,
						Convert: &v1.ConvertTransform{
							ToType: "string",
						},
					},
				},
				fromType: "string",
				toType:   "string",
			},
		},
		"RejectInvalidTransformType": {
			reason: "Should reject invalid transform types",
			want:   want{err: true},
			args: args{
				transforms: []v1.Transform{
					{
						Type: v1.TransformType("doesnotexist"),
					},
				},
				fromType: "string",
				toType:   "string",
			},
		},
		"AcceptNilTransformsNoFromType": {
			reason: "Should accept if there is no type spec for input and no transforms are provided",
			want:   want{err: false},
			args: args{
				transforms: nil,
				fromType:   "",
				toType:     "string",
			},
		},
		"AcceptNilTransformsNoToType": {
			reason: "Should accept if there is no type spec for output and no transforms are provided",
			want:   want{err: false},
			args: args{
				transforms: nil,
				fromType:   "string",
				toType:     "",
			},
		},
		"AcceptNoInputOutputNoTransforms": {
			reason: "Should accept if there are no type spec for input and output and no transforms are provided",
			want:   want{err: false},
			args: args{
				transforms: nil,
				fromType:   "",
				toType:     "",
			},
		},
		"AcceptNoInputOutputWithTransforms": {
			reason: "Should accept if there are no type spec for input and output and transforms are provided",
			want:   want{err: false},
			args: args{
				transforms: []v1.Transform{
					{
						Type: v1.TransformTypeConvert,
						Convert: &v1.ConvertTransform{
							ToType: "int64",
						},
					},
				},
				fromType: "",
				toType:   "",
			},
		},
		"RejectNoToTypeInvalidInputType": {
			reason: "Should reject if there is no type spec for the output, but input is specified and transforms are wrong",
			want:   want{err: true},
			args: args{
				transforms: []v1.Transform{
					{
						Type: v1.TransformTypeMath,
						Math: &v1.MathTransform{
							Multiply: pointer.Int64(2),
						},
					},
				},
				fromType: "string",
				toType:   "",
			},
		},
		"RejectNoInputTypeWrongOutputTypeForTransforms": {
			reason: "Should return an error if there is no type spec for the input, but output is specified and transforms are wrong",
			want:   want{err: true},
			args: args{
				transforms: []v1.Transform{
					{
						Type: v1.TransformTypeMath,
						Math: &v1.MathTransform{
							Multiply: pointer.Int64(2),
						},
					},
				},
				fromType: "",
				toType:   "string",
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := validateIOTypesWithTransforms(tc.args.transforms, tc.args.fromType, tc.args.toType)
			if diff := cmp.Diff(tc.want.err, err != nil); diff != "" {
				t.Errorf("\n%s\nvalidateIOTypesWithTransforms(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

func Test_validateFieldPath(t *testing.T) {
	type args struct {
		schema    *apiextensions.JSONSchemaProps
		fieldPath string
	}
	type want struct {
		err       bool
		fieldType schema.KnownJSONType
		required  bool
	}
	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"AcceptValidFieldPath": {
			reason: "Should validate a valid field path",
			want:   want{err: false, fieldType: "string", required: false},
			args: args{
				fieldPath: "spec.forProvider.foo",
				schema: &apiextensions.JSONSchemaProps{
					Properties: map[string]apiextensions.JSONSchemaProps{
						"spec": {
							Properties: map[string]apiextensions.JSONSchemaProps{
								"forProvider": {
									Properties: map[string]apiextensions.JSONSchemaProps{
										"foo": {Type: "string"}}}}}}}},
		},
		"AcceptValidFieldPathWithRequiredChain": {
			reason: "Should validate a valid field path with a field required the whole chain",
			want:   want{err: false, fieldType: "string", required: true},
			args: args{
				fieldPath: "spec.forProvider.foo",
				schema: &apiextensions.JSONSchemaProps{
					Required: []string{"spec"},
					Properties: map[string]apiextensions.JSONSchemaProps{
						"spec": {
							Required: []string{"forProvider"},
							Properties: map[string]apiextensions.JSONSchemaProps{
								"forProvider": {
									Required: []string{"foo"},
									Properties: map[string]apiextensions.JSONSchemaProps{
										"foo": {Type: "string"}}}}}}}},
		},
		"AcceptValidFieldPathWithRequiredFieldNotWholeChain": {
			reason: "Should not return that a field is required if it is not the whole chain",
			want:   want{err: false, fieldType: "string", required: false},
			args: args{
				fieldPath: "spec.forProvider.foo",
				schema: &apiextensions.JSONSchemaProps{
					Required: []string{"spec"},
					Properties: map[string]apiextensions.JSONSchemaProps{
						"spec": {
							Required: []string{"forProvider"},
							Properties: map[string]apiextensions.JSONSchemaProps{
								"forProvider": {
									Properties: map[string]apiextensions.JSONSchemaProps{
										"foo": {Type: "string"}}}}}}}},
		},
		"RejectInvalidFieldPath": {
			reason: "Should return an error for an invalid field path",
			want:   want{err: true},
			args: args{
				fieldPath: "spec.forProvider.wrong",
				schema: &apiextensions.JSONSchemaProps{
					Properties: map[string]apiextensions.JSONSchemaProps{
						"spec": {
							Properties: map[string]apiextensions.JSONSchemaProps{
								"forProvider": {
									Properties: map[string]apiextensions.JSONSchemaProps{
										"foo": {Type: "string"}}}}}}}},
		},
		"AcceptFieldPathXPreserveUnkownFields": {
			reason: "Should not return an error for an undefined but accepted field path",
			want:   want{err: false, fieldType: "", required: false},
			args: args{
				fieldPath: "spec.forProvider.wrong",
				schema: &apiextensions.JSONSchemaProps{
					Properties: map[string]apiextensions.JSONSchemaProps{
						"spec": {
							Properties: map[string]apiextensions.JSONSchemaProps{
								"forProvider": {
									Properties: map[string]apiextensions.JSONSchemaProps{
										"foo": {Type: "string"}},
									XPreserveUnknownFields: &[]bool{true}[0],
								}}}}}},
		},
		"AcceptValidArray": {
			reason: "Should validate arrays properly",
			want:   want{err: false, fieldType: "string", required: false},
			args: args{
				fieldPath: "spec.forProvider.foo[0].bar",
				schema: &apiextensions.JSONSchemaProps{
					Properties: map[string]apiextensions.JSONSchemaProps{
						"spec": {
							Properties: map[string]apiextensions.JSONSchemaProps{
								"forProvider": {
									Properties: map[string]apiextensions.JSONSchemaProps{
										"foo": {
											Type: "array",
											Items: &apiextensions.JSONSchemaPropsOrArray{
												Schema: &apiextensions.JSONSchemaProps{
													Properties: map[string]apiextensions.JSONSchemaProps{
														"bar": {Type: "string"}}}}}}}}}}}},
		},
		"AcceptMinItems1NotRequired": {
			reason: "Should validate arrays properly with a field not required the whole chain, minimum length 1",
			want:   want{err: false, fieldType: "string", required: false},
			args: args{
				fieldPath: "spec.forProvider.foo[1].bar",
				schema: &apiextensions.JSONSchemaProps{
					Properties: map[string]apiextensions.JSONSchemaProps{
						"spec": {
							Properties: map[string]apiextensions.JSONSchemaProps{
								"forProvider": {
									Properties: map[string]apiextensions.JSONSchemaProps{
										"foo": {
											Type:     "array",
											MinItems: &[]int64{1}[0],
											Items: &apiextensions.JSONSchemaPropsOrArray{
												Schema: &apiextensions.JSONSchemaProps{
													Required: []string{"bar"},
													Properties: map[string]apiextensions.JSONSchemaProps{
														"bar": {Type: "string"}}}}}}}}}}}},
		},
		"AcceptRequiredInMinItemsRange": {
			reason: "Should validate arrays properly with a field required the whole chain, accessing in min items range",
			want:   want{err: false, fieldType: "string", required: true},
			args: args{
				fieldPath: "spec.forProvider.foo[1].bar",
				schema: &apiextensions.JSONSchemaProps{
					Required: []string{"spec"},
					Properties: map[string]apiextensions.JSONSchemaProps{
						"spec": {
							Required: []string{"forProvider"},
							Properties: map[string]apiextensions.JSONSchemaProps{
								"forProvider": {
									Required: []string{"foo"},
									Properties: map[string]apiextensions.JSONSchemaProps{
										"foo": {
											Type:     "array",
											MinItems: &[]int64{2}[0],
											Items: &apiextensions.JSONSchemaPropsOrArray{
												Schema: &apiextensions.JSONSchemaProps{
													Required: []string{"bar"},
													Properties: map[string]apiextensions.JSONSchemaProps{
														"bar": {Type: "string"}}}}}}}}}}}},
		},
		"AcceptRequiredAboveMinItemsRange": {
			reason: "Should validate arrays properly with a field required the whole chain, accessing above min items range",
			want:   want{err: false, fieldType: "string", required: false},
			args: args{
				fieldPath: "spec.forProvider.foo[10].bar",
				schema: &apiextensions.JSONSchemaProps{
					Required: []string{"spec"},
					Properties: map[string]apiextensions.JSONSchemaProps{
						"spec": {
							Required: []string{"forProvider"},
							Properties: map[string]apiextensions.JSONSchemaProps{
								"forProvider": {
									Required: []string{"foo"},
									Properties: map[string]apiextensions.JSONSchemaProps{
										"foo": {
											Type:     "array",
											MinItems: &[]int64{2}[0],
											Items: &apiextensions.JSONSchemaPropsOrArray{
												Schema: &apiextensions.JSONSchemaProps{
													Required: []string{"bar"},
													Properties: map[string]apiextensions.JSONSchemaProps{
														"bar": {Type: "string"}}}}}}}}}}}},
		},
		"AcceptComplexSchema": {
			reason: "Should validate properly with complex schema",
			want:   want{err: false, fieldType: "string", required: false},
			args: args{
				fieldPath: "spec.forProvider.clientIDList[0]",
				// parse the schema from json
				schema: complexSchemaOpenIDConnectProvidersV1beta1Props,
			},
		},
		"RejectComplexAboveMaxItems": {
			reason: "Should error if above max items",
			want:   want{err: true},
			args: args{
				fieldPath: "spec.forProvider.clientIDList[101]",
				// parse the schema from json
				schema: complexSchemaOpenIDConnectProvidersV1beta1Props,
			},
		},
		"AcceptBelowMinItemsRequiredChain": {
			reason: "Should accept if below min items, and mark as required if the whole chain is required",
			want:   want{err: false, fieldType: "string", required: true},
			args: args{
				fieldPath: "spec.forProvider.thumbprintList[0]",
				// parse the schema from json
				schema: complexSchemaOpenIDConnectProvidersV1beta1Props,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			gotFieldType, gotRequired, err := validateFieldPath(tc.args.schema, tc.args.fieldPath)
			if diff := cmp.Diff(tc.want.err, err != nil, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nvalidateFieldPath(...): -want error, +got error: %s\n", tc.reason, diff)
				return
			}
			if diff := cmp.Diff(tc.want.fieldType, gotFieldType); diff != "" {
				t.Errorf("\n%s\nvalidateFieldPath(...): -want, +got: %s\n", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.required, gotRequired); diff != "" {
				t.Errorf("\n%s\nvalidateFieldPath(...): -want, +got: %s\n", tc.reason, diff)
			}
		})
	}
}

func Test_validateFieldPathSegmentIndex(t *testing.T) {
	type args struct {
		parent  *apiextensions.JSONSchemaProps
		segment fieldpath.Segment
	}
	type want struct {
		err      bool
		required bool
	}
	cases := map[string]struct {
		name string
		args args
		want want
	}{
		"RejectParentNotArray": {
			name: "Should return an error if the parent is not an array",
			args: args{
				parent: &apiextensions.JSONSchemaProps{
					Type: "string",
				},
				segment: fieldpath.Segment{
					Type:  fieldpath.SegmentIndex,
					Index: 1,
				},
			},
			want: want{err: true, required: false},
		},
		"AcceptParentArray": {
			name: "Should return no error if the parent is an array",
			args: args{
				parent: &apiextensions.JSONSchemaProps{
					Type: "array",
					Items: &apiextensions.JSONSchemaPropsOrArray{
						Schema: &apiextensions.JSONSchemaProps{
							Type: "string",
						},
					},
				},
				segment: fieldpath.Segment{
					Type:  fieldpath.SegmentIndex,
					Index: 1,
				},
			},
			want: want{err: false, required: false},
		},
		"AcceptMinSizeArrayBelowRequired": {
			name: "Should return no error and required if the parent is an array, accessing element below min size",
			args: args{
				parent: &apiextensions.JSONSchemaProps{
					Type:     "array",
					MinItems: &[]int64{2}[0],
					Items: &apiextensions.JSONSchemaPropsOrArray{
						Schema: &apiextensions.JSONSchemaProps{
							Type: "string",
						},
					},
				},
				segment: fieldpath.Segment{
					Type:  fieldpath.SegmentIndex,
					Index: 1,
				},
			},
			want: want{err: false, required: true},
		},
		"AcceptMinSizeArrayAboveNotRequired": {
			name: "Should return no error and not required if the parent is an array, accessing element above min size",
			args: args{
				parent: &apiextensions.JSONSchemaProps{
					Type:     "array",
					MinItems: &[]int64{2}[0],
					Items: &apiextensions.JSONSchemaPropsOrArray{
						Schema: &apiextensions.JSONSchemaProps{
							Type: "string",
						},
					},
				},
				segment: fieldpath.Segment{
					Type:  fieldpath.SegmentIndex,
					Index: 3,
				},
			},
			want: want{err: false, required: false},
		},
		"AcceptIndex0MinSize1": {
			name: "Should return no error and required if the parent is an array with min size 1 and the index is 0",
			args: args{
				parent: &apiextensions.JSONSchemaProps{
					Type:     "array",
					MinItems: &[]int64{1}[0],
					Items: &apiextensions.JSONSchemaPropsOrArray{
						Schema: &apiextensions.JSONSchemaProps{
							Type: "string",
						},
					},
				},
				segment: fieldpath.Segment{
					Type:  fieldpath.SegmentIndex,
					Index: 0,
				},
			},
			want: want{err: false, required: true},
		},
		"RejectAboveMaxIndex": {
			name: "Should return an error if accessing an index that is above the max items",
			args: args{
				parent: &apiextensions.JSONSchemaProps{
					Type:     "array",
					MaxItems: &[]int64{1}[0],
					Items: &apiextensions.JSONSchemaPropsOrArray{
						Schema: &apiextensions.JSONSchemaProps{
							Type: "string",
						},
					},
				},
				segment: fieldpath.Segment{
					Type:  fieldpath.SegmentIndex,
					Index: 1,
				},
			},
			want: want{err: true, required: false},
		},
		"AcceptBelowMaxIndex": {
			name: "Should return no error if accessing an index that is below the max items",
			args: args{
				parent: &apiextensions.JSONSchemaProps{
					Type:     "array",
					MaxItems: &[]int64{10}[0],
					Items: &apiextensions.JSONSchemaPropsOrArray{
						Schema: &apiextensions.JSONSchemaProps{
							Type: "string",
						},
					},
				},
				segment: fieldpath.Segment{
					Type:  fieldpath.SegmentIndex,
					Index: 1,
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, required, err := validateFieldPathSegmentIndex(tc.args.parent, tc.args.segment)
			if diff := cmp.Diff(tc.want.err, err != nil); diff != "" {
				t.Errorf("\n%s\nvalidateFieldPathSegmentIndex(...): -want, +got: %s\n", tc.name, diff)
			}
			if diff := cmp.Diff(tc.want.required, required); diff != "" {
				t.Errorf("\n%s\nvalidateFieldPathSegmentIndex(...): -want, +got: %s\n", tc.name, diff)
			}
		})
	}
}

func Test_validateFieldPathSegmentField(t *testing.T) {
	type args struct {
		parent  *apiextensions.JSONSchemaProps
		segment fieldpath.Segment
	}
	type want struct {
		err      bool
		required bool
	}
	cases := map[string]struct {
		name string
		args args
		want want
	}{
		"RejectParentNotObject": {
			name: "Should return an error if the parent is not an object",
			args: args{
				parent: &apiextensions.JSONSchemaProps{
					Type: "string",
				},
				segment: fieldpath.Segment{
					Type:  fieldpath.SegmentField,
					Field: "foo",
				},
			},
			want: want{err: true, required: false},
		},
		"AcceptFieldNotPresent": {
			name: "Should return no error if the parent is an object and the field is present",
			args: args{
				parent: &apiextensions.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensions.JSONSchemaProps{
						"foo": {
							Type: "string",
						},
					},
				},
				segment: fieldpath.Segment{
					Type:  fieldpath.SegmentField,
					Field: "foo",
				},
			},
			want: want{err: false, required: false},
		},
		"AcceptFieldNotPresentRequired": {
			name: "Should return no error if the parent is an object and the field is present and required",
			args: args{
				parent: &apiextensions.JSONSchemaProps{
					Type:     "object",
					Required: []string{"foo"},
					Properties: map[string]apiextensions.JSONSchemaProps{
						"foo": {
							Type: "string",
						},
					},
				},
				segment: fieldpath.Segment{
					Type:  fieldpath.SegmentField,
					Field: "foo",
				},
			},
			want: want{err: false, required: true},
		},
		"AcceptFieldNotPresentWithXPreserveUnknownFields": {
			name: "Should return no error with XPreserveUnknownFields accessing a missing field",
			args: args{
				parent: &apiextensions.JSONSchemaProps{
					Type:                   "object",
					XPreserveUnknownFields: &[]bool{true}[0],
					Properties: map[string]apiextensions.JSONSchemaProps{
						"foo": {
							Type: "string",
						},
					},
				},
				segment: fieldpath.Segment{
					Type:  fieldpath.SegmentField,
					Field: "bar",
				},
			},
			want: want{err: false, required: false},
		},
		"AcceptFieldPresentWithXPreserveUnknownFieldsRequired": {
			name: "Should return no error with XPreserveUnknownFields, but required if a known required field is accessed",
			args: args{
				parent: &apiextensions.JSONSchemaProps{
					Type:                   "object",
					XPreserveUnknownFields: &[]bool{true}[0],
					Required:               []string{"foo"},
					Properties: map[string]apiextensions.JSONSchemaProps{
						"foo": {
							Type: "string",
						},
					},
				},
				segment: fieldpath.Segment{
					Type:  fieldpath.SegmentField,
					Field: "foo",
				},
			},
			want: want{err: false, required: true},
		},
		"AcceptFieldNotPresentWithAdditionalProperties": {
			name: "Should return no error with AdditionalProperties accessing a missing field",
			args: args{
				parent: &apiextensions.JSONSchemaProps{
					Type:                 "object",
					AdditionalProperties: &apiextensions.JSONSchemaPropsOrBool{Allows: true},
					Properties: map[string]apiextensions.JSONSchemaProps{
						"foo": {
							Type: "string",
						},
					},
				},
				segment: fieldpath.Segment{
					Type:  fieldpath.SegmentField,
					Field: "bar",
				},
			},
			want: want{err: false, required: false},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, required, err := validateFieldPathSegmentField(tt.args.parent, tt.args.segment)
			if diff := cmp.Diff(tt.want.err, err != nil); diff != "" {
				t.Errorf("\n%s\nvalidateFieldPathSegmentField(...): -want, +got: %s\n", tt.name, diff)
			}
			if diff := cmp.Diff(tt.want.required, required); diff != "" {
				t.Errorf("\n%s\nvalidateFieldPathSegmentField(...): -want, +got: %s\n", tt.name, diff)
			}
		})
	}
}
