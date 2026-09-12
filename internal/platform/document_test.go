package platform_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

func TestReadDocument(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "fixture.json"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	const budget = 8
	got, err := platform.ReadDocument(t.Context(), root, "fixture.json", budget)
	if err != nil || string(got) != "fixture" {
		t.Fatal(string(got), err)
	}
	for _, name := range []string{"../fixture.json", "missing"} {
		if _, err := platform.ReadDocument(t.Context(), root, name, budget); err == nil {
			t.Fatal("invalid document accepted")
		}
	}
	if _, err := platform.ReadDocument(t.Context(), root, "fixture.json", 1); err == nil {
		t.Fatal("oversized document accepted")
	}
	if _, err := platform.ReadDocument(t.Context(), root+"/missing", "fixture.json", budget); err == nil {
		t.Fatal("missing root accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := platform.ReadDocument(ctx, root, "fixture.json", budget); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
