package v1_test

import (
	"bytes"
	"testing"

	v1 "github.com/EpicBlackWolfZ/capagent/schema/v1"
)

func TestGetSchema(t *testing.T) {
	t.Parallel()

	original := append([]byte(nil), v1.SchemaBytes...)
	returned := v1.GetSchema()
	if len(returned) == 0 {
		t.Fatal("expected non-empty schema bytes, got empty")
	}

	// Verify mutating returned slice does not corrupt package variable contents
	returned[0] ^= 0xFF
	if !bytes.Equal(v1.SchemaBytes, original) {
		t.Errorf("mutating returned GetSchema() slice corrupted package variable v1.SchemaBytes")
	}
}
