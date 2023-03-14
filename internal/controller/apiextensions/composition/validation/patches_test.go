package validation

import (
	"testing"

	v1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
)

func Test_validateTransforms(t *testing.T) {
	type args struct {
		transforms []v1.Transform
		fromType   string
		toType     string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "Should validate empty transforms to the same type successfully",
			args: args{
				transforms: []v1.Transform{},
				fromType:   "string",
				toType:     "string",
			},
		},
		{
			name:    "Should reject empty transforms to a different type",
			wantErr: true,
			args: args{
				transforms: []v1.Transform{},
				fromType:   "string",
				toType:     "integer",
			},
		},
		{
			name: "Should accept empty transforms to a different type when its integer to number",
			args: args{
				transforms: []v1.Transform{},
				fromType:   "integer",
				toType:     "number",
			},
		},
		{
			name: "Should validate convert transforms successfully",
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
		{
			name: "Should validate convert integer to number transforms successfully",
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
		{
			name:    "Should reject convert number to integer transforms successfully",
			wantErr: true,
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
		{
			name: "Should validate multiple convert transforms successfully",
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
		{
			name:    "Should reject invalid transform types",
			wantErr: true,
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateTransforms(tt.args.transforms, tt.args.fromType, tt.args.toType); (err != nil) != tt.wantErr {
				t.Errorf("validateTransforms() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
