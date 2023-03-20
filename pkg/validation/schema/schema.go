// Package schema defines helpers for working with JSON schema.
// As defined by https://datatracker.ietf.org/doc/html/draft-zyp-json-schema-04
package schema

// KnownJSONType is all the known JSON types.
// See https://datatracker.ietf.org/doc/html/draft-zyp-json-schema-04#section-3.5
type KnownJSONType string

const (
	// ArrayKnownJSONType is the JSON type for arrays.
	ArrayKnownJSONType KnownJSONType = "array"
	// BooleanKnownJSONType is the JSON type for booleans.
	BooleanKnownJSONType KnownJSONType = "boolean"
	// IntegerKnownJSONType is the JSON type for integers.
	IntegerKnownJSONType KnownJSONType = "integer"
	// NullKnownJSONType is the JSON type for null.
	NullKnownJSONType KnownJSONType = "null"
	// NumberKnownJSONType is the JSON type for numbers.
	NumberKnownJSONType KnownJSONType = "number"
	// ObjectKnownJSONType is the JSON type for objects.
	ObjectKnownJSONType KnownJSONType = "object"
	// StringKnownJSONType is the JSON type for strings.
	StringKnownJSONType KnownJSONType = "string"
)

// IsEquivalent returns true if the two supplied types are equal, or if the first
// type is an integer and the second is a number. This is because the JSON
// schema spec allows integers to be used in place of numbers.
func (t KnownJSONType) IsEquivalent(t2 KnownJSONType) bool {
	return t == t2 || (t == IntegerKnownJSONType && t2 == NumberKnownJSONType)
}

// IsKnownJSONType returns true if the supplied string is a known JSON type.
func IsKnownJSONType(t string) bool {
	switch KnownJSONType(t) {
	case ArrayKnownJSONType, BooleanKnownJSONType, IntegerKnownJSONType, NullKnownJSONType, NumberKnownJSONType, ObjectKnownJSONType, StringKnownJSONType:
		return true
	default:
		return false
	}
}
