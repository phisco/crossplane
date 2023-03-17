// Package schema defines helpers for working with JSON schema.
// As defined by https://datatracker.ietf.org/doc/html/draft-zyp-json-schema-04
package schema

// KnownJSONTypes is all the known JSON types.
// See https://datatracker.ietf.org/doc/html/draft-zyp-json-schema-04#section-3.5
type KnownJSONTypes string

const (
	// ArrayKnownJSONType is the JSON type for arrays.
	ArrayKnownJSONType KnownJSONTypes = "array"
	// BooleanKnownJSONType is the JSON type for booleans.
	BooleanKnownJSONType KnownJSONTypes = "boolean"
	// IntegerKnownJSONType is the JSON type for integers.
	IntegerKnownJSONType KnownJSONTypes = "integer"
	// NullKnownJSONType is the JSON type for null.
	NullKnownJSONType KnownJSONTypes = "null"
	// NumberKnownJSONType is the JSON type for numbers.
	NumberKnownJSONType KnownJSONTypes = "number"
	// ObjectKnownJSONType is the JSON type for objects.
	ObjectKnownJSONType KnownJSONTypes = "object"
	// StringKnownJSONType is the JSON type for strings.
	StringKnownJSONType KnownJSONTypes = "string"
)
