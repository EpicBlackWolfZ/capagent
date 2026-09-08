package v1_test

import (
	"testing"

	v1 "github.com/EpicBlackWolfZ/capagent/schema/v1"
)

func TestGetSchema(t *testing.T) {
	t.Parallel()

	bytes := v1.GetSchema()
	if len(bytes) == 0 {
		t.Fatal("expected non-empty schema bytes, got empty")
	}

	// Verify mutating returned slice does not corrupt package variable
	originalLen := len(v1.SchemaBytes)
	bytes[0] = '{'
	if len(v1.SchemaBytes) != originalLen {
		t.Errorf("expected package SchemaBytes length %d, got %d", originalLen, len(v1.SchemaBytes))
	}
}
