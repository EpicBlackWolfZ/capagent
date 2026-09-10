package platform_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

func TestCommandPolicy_FakeFullSpecification(t *testing.T) {
	t.Parallel()
	policy, err := platform.NewEnvPolicy(nil, map[string]string{"Z": "last", "A": "first"})
	if err != nil {
		t.Fatal(err)
	}
	policy2, err := platform.NewEnvPolicy(nil, map[string]string{"A": "first", "Z": "last"})
	if err != nil {
		t.Fatal(err)
	}
	spec := platform.CommandSpec{Path: fakeEchoCommand, Args: []string{"a", ""}, Env: policy}
	fake := platform.NewFakeCommandRunner()
	if err := fake.Register(spec, platform.ExecResult{Stdout: []byte("matched")}); err != nil {
		t.Fatal(err)
	}
	equivalent := platform.CommandSpec{Path: fakeEchoCommand, Args: []string{"a", ""}, Env: policy2, Dir: "/"}
	if _, err := fake.Run(t.Context(), equivalent); err != nil {
		t.Fatal("equivalent snapshot/default directory did not match", err)
	}
	tests := []struct {
		name   string
		change func(*platform.CommandSpec)
	}{
		{"path", func(s *platform.CommandSpec) { s.Path += "2" }},
		{"arg count", func(s *platform.CommandSpec) { s.Args = []string{"a"} }},
		{"arg order", func(s *platform.CommandSpec) { s.Args = []string{"", "a"} }},
		{"environment", func(s *platform.CommandSpec) { s.Env = platform.EnvPolicy{} }},
		{"directory", func(s *platform.CommandSpec) { s.Dir = "/tmp" }},
		{"timeout", func(s *platform.CommandSpec) { s.Timeout = time.Second }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			changed := spec
			tt.change(&changed)
			if _, err := fake.Run(t.Context(), changed); !errors.Is(err, platform.ErrUnmockedCommand()) {
				t.Fatal(err)
			}
		})
	}
}

func TestCommandPolicy_FakeSnapshotOwnership(t *testing.T) {
	t.Parallel()
	policy, err := platform.NewEnvPolicy(nil, map[string]string{"TOKEN": policySecret})
	if err != nil {
		t.Fatal(err)
	}
	variables := policy.Variables()
	variables[0] = "MUTATED=yes"
	if reflect.DeepEqual(variables, policy.Variables()) {
		t.Fatal("environment aliases escaped")
	}
	defaults := (platform.EnvPolicy{}).Variables()
	defaults[0] = "MUTATED=yes"
	if reflect.DeepEqual(defaults, (platform.EnvPolicy{}).Variables()) {
		t.Fatal("default environment aliases escaped")
	}
	args := []string{"original-argument"}
	spec := platform.CommandSpec{Path: fakeEchoCommand, Args: args, Env: policy}
	fake := platform.NewFakeCommandRunner()
	if err := fake.RegisterWithError(spec, platform.ExecResult{}, errors.New(policySecret)); err != nil {
		t.Fatal(err)
	}
	args[0] = "mutated"
	spec.Args = []string{"original-argument"}
	_, err = fake.Run(t.Context(), spec)
	assertPolicyRedacted(t, err)
	spec.Args[0] = "mutated after run"
	calls := fake.Calls()
	assertPolicyRedacted(t, calls[0])
	if calls[0].Spec.Args[0] != "original-argument" {
		t.Fatal("call aliases input")
	}
	calls[0].Spec.Args[0] = "mutated call"
	if fake.Calls()[0].Spec.Args[0] != "original-argument" {
		t.Fatal("call aliases output")
	}
	// Nil contexts retain the historical background-context behavior in both runners.
	var nilCtx context.Context //nolint:staticcheck // SA1012: intentional nil-context contract test.
	if _, err := fake.Run(nilCtx, platform.CommandSpec{Path: fakeEchoCommand}); !errors.Is(err, platform.ErrUnmockedCommand()) {
		t.Fatal(err)
	}
}

func TestCommandPolicy_ExplicitDefaultOverrides(t *testing.T) {
	t.Parallel()
	policy, err := platform.NewEnvPolicy(nil, map[string]string{"PATH": "/explicit", "LC_ALL": "POSIX", "_KEY9": ""})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(policy.Variables(), []string{"LC_ALL=POSIX", "PATH=/explicit", "_KEY9="}) {
		t.Fatal(policy.Variables())
	}
	// A longer per-command timeout must override a short runner default too.
	result, err := platform.NewOSCommandRunner(time.Nanosecond).Run(t.Context(), platform.CommandSpec{
		Path: echoCommand, Args: []string{"ok"}, Env: policy, Timeout: time.Second,
	})
	if err != nil || result.TimedOut || string(result.Stdout) != "ok\n" {
		t.Fatalf("override failed: %+v %v", result, err)
	}
}
