package app

import (
	"bytes"
	"strings"
	"testing"
)

func TestContextFixtureReportsAllocations(t *testing.T) {
	t.Parallel()
	var out, diagnostics bytes.Buffer
	status := Execute(t.Context(), Options{Fixture: "../../testdata/fixtures/v1/context-rootless"}, &out, &diagnostics)
	if status != ExitIndeterminate {
		t.Fatal(status, diagnostics.String())
	}
	for _, want := range []string{`"subids":`, `"total":35`, `"total":12`, `"setuid":true`, `"executable":true`, `"usable":null`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %s", want)
		}
	}
}
