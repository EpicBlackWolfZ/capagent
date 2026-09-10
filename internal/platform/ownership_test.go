package platform_test

import (
	"context"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"reflect"
	"testing"
)

func TestFakeEvidenceOwnership(t *testing.T) {
	t.Parallel()
	t.Run("file", func(t *testing.T) {
		t.Parallel()
		mem := platform.NewMemPlatformReader()
		input := []byte("original")
		mem.AddFile("/file", input, 0o600)
		input[0] = 'X'
		first, err := mem.ReadFile(t.Context(), "/file")
		if err != nil {
			t.Fatal(err)
		}
		if string(first) != "original" {
			t.Errorf("input alias: %q", first)
		}
		first[0] = 'Y'
		second, err := mem.ReadFile(t.Context(), "/file")
		if err != nil {
			t.Fatal(err)
		}
		if string(second) != "original" {
			t.Errorf("output alias: %q", second)
		}
	})
	t.Run("command", func(t *testing.T) {
		t.Parallel()
		runner := platform.NewFakeCommandRunner()
		out, errs := []byte("output"), []byte("errors")
		if err :=
			runner.Register(platform.CommandSpec{Path: "/fixtures/command", Args: nil},
				platform.ExecResult{Stdout: out, Stderr: errs}); err != nil {
			t.Fatal(err)
		}
		out[0] = 'X'
		errs[0] = 'X'
		first, err := runner.Run(context.Background(), platform.CommandSpec{Path: "/fixtures/command"})
		if err != nil {
			t.Fatal(err)
		}
		if string(first.Stdout) != "output" || string(first.Stderr) != "errors" {
			t.Errorf("input alias: %+v", first)
		}
		first.Stdout[0] = 'Y'
		first.Stderr[0] = 'Y'
		second, err := runner.Run(context.Background(), platform.CommandSpec{Path: "/fixtures/command"})
		if err != nil {
			t.Fatal(err)
		}
		if string(second.Stdout) != "output" || string(second.Stderr) != "errors" {
			t.Errorf("output alias: %+v", second)
		}
	})
}

// The probe-facing API must not expose setup or teardown authority.
func TestEnvironment_ReadOnlySurface(t *testing.T) {
	t.Parallel()
	env := platform.NewTestEnvironment(nil, platform.NewFakeCommandRunner())
	if fields := writableEnvironmentFields(reflect.TypeOf(env)); len(fields) != 0 {
		t.Errorf("writable fields: %v", fields)
	}
	// Negative fixture proves the same surface check rejects a writable service.
	type writableEnvironment struct{ Reader platform.PlatformReader }
	if fields := writableEnvironmentFields(reflect.TypeFor[writableEnvironment]()); len(fields) != 1 {
		t.Fatal("mutation fixture escaped detection")
	}
	for _, view := range []any{env.Reader(), env.Runner(), env.Procfs(), env.Sysfs()} {
		if _, ok := view.(interface{ Close() error }); ok {
			t.Error("probe can close owner resource")
		}
		if _, ok := view.(*platform.MemPlatformReader); ok {
			t.Error("probe can mutate fake reader")
		}
		if _, ok := view.(*platform.FakeCommandRunner); ok {
			t.Error("probe can mutate fake runner")
		}
		if _, ok := view.(*platform.ProcfsReader); ok {
			t.Error("probe can overwrite shared procfs wrapper")
		}
		if _, ok := view.(*platform.SysfsReader); ok {
			t.Error("probe can overwrite shared sysfs wrapper")
		}
	}
}

// Inspect the real API and negative fixtures with exactly the same rule.
func writableEnvironmentFields(typ reflect.Type) []string {
	var fields []string
	for i := 0; i < typ.NumField(); i++ {
		if typ.Field(i).IsExported() {
			fields = append(fields, typ.Field(i).Name)
		}
	}
	return fields
}
