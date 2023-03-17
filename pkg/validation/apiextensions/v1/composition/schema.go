package composition

import (
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"

	"github.com/crossplane/crossplane/pkg/validation/schema"
)

var (
	metadataSchema = apiextensions.JSONSchemaProps{
		Type: string(schema.ObjectKnownJSONType),
		AdditionalProperties: &apiextensions.JSONSchemaPropsOrBool{
			Allows: true,
		},
		Properties: map[string]apiextensions.JSONSchemaProps{
			"name": {
				Type: string(schema.StringKnownJSONType),
			},
			"namespace": {
				Type: string(schema.StringKnownJSONType),
			},
			"labels": {
				Type: string(schema.ObjectKnownJSONType),
				AdditionalProperties: &apiextensions.JSONSchemaPropsOrBool{
					Schema: &apiextensions.JSONSchemaProps{
						Type: string(schema.StringKnownJSONType),
					},
				},
			},
			"annotations": {
				Type: string(schema.ObjectKnownJSONType),
				AdditionalProperties: &apiextensions.JSONSchemaPropsOrBool{
					Schema: &apiextensions.JSONSchemaProps{
						Type: string(schema.StringKnownJSONType),
					},
				},
			},
			"uid": {
				Type: string(schema.StringKnownJSONType),
			},
		},
	}
)
