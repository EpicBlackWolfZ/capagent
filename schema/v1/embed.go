package v1

import (
	_ "embed"
)

// SchemaBytes contains the raw embedded Schema v1 JSON specification.
//
//go:embed schema.json
var SchemaBytes []byte

// GetSchema returns a copy of the embedded Schema v1 JSON specification bytes.
func GetSchema() []byte {
	out := make([]byte, len(SchemaBytes))
	copy(out, SchemaBytes)
	return out
}
