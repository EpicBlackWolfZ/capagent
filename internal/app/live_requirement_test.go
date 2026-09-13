package app

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/config"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

const testCallerRequirementPath = "/caller/only.json"

func TestLiveExplicitRequirementOutcomes(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		document string
		code     int
	}{
		{`{"capability":"runtime.podman.network.backend.netavark"}`, ExitSatisfied},
		{`{"capability":"runtime.podman.network.rootless.pasta"}`, ExitUnsatisfied},
		{`{"capability":"runtime.podman.not_delivered"}`, ExitIndeterminate},
	} {
		t.Run(test.document, func(t *testing.T) {
			t.Parallel()
			node, err := config.ParseRequirement([]byte(test.document))
			if err != nil {
				t.Fatal(err)
			}
			opts := Options{Runtime: testPodmanRuntime, Active: true, Requirement: "caller/requirements.json",
				requirementNode: node, requirementJSON: []byte(test.document)}
			services, runner := activeServices(t)
			report, err := evaluateCurrentServices(t.Context(), opts, services)
			if err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			if code := writeReport(report, opts, &stdout, &stderr); code != test.code {
				t.Fatalf("requirement exit=%d want=%d", code, test.code)
			}
			if len(runner.Calls()) != 2 || report.Evaluation.Target.UID != 0 || report.Evaluation.Scope.Runtime != testPodmanRuntime {
				t.Fatal("requirement changed inspection authority or target/runtime scope")
			}
		})
	}
}

func TestTargetRequirementPresenceBoundsAndOwnership(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, path, document string
		valid                bool
	}{
		{"default", "", "", true},
		{"selected", testCallerRequirementPath, `{"all":[]}`, true},
		{"missing payload", testCallerRequirementPath, "", false},
		{"unexpected payload", "", `{"all":[]}`, false},
		{"invalid payload", testCallerRequirementPath, `{"secret":true}`, false},
		{"null payload", testCallerRequirementPath, `null`, false},
		{"oversized payload", testCallerRequirementPath, `{"all":[]}` + strings.Repeat(" ", config.MaxRequirementBytes), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			payload := targetPayload{Options: Options{Requirement: test.path}, Requirement: []byte(test.document)}
			err := bindTargetRequirement(&payload)
			if (err == nil) != test.valid {
				t.Fatal("unexpected worker requirement validation result")
			}
			if test.name == "selected" {
				payload.Requirement[0] = '!'
				if payload.Options.requirementJSON[0] != '{' || payload.Options.requirementNode == nil {
					t.Fatal("worker borrowed transport buffer or discarded parsed request")
				}
			}
		})
	}
}

func TestDelegatedRequirementRetainsCallerDocument(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		document string
		code     int
	}{
		{`{"not":{"capability":"runtime.podman.helper.crun.installed"}}`, ExitSatisfied},
		{`{"capability":"runtime.podman.helper.crun.installed"}`, ExitUnsatisfied},
		{`{"capability":"runtime.podman.not_delivered"}`, ExitIndeterminate},
	} {
		t.Run(test.document, func(t *testing.T) {
			t.Parallel()
			parent := targetServices(t, 0)
			node, err := config.ParseRequirement([]byte(test.document))
			if err != nil {
				t.Fatal(err)
			}
			opts := Options{Runtime: testPodmanRuntime, Context: "user:alice", Requirement: "/caller-only/requirements.json",
				requirementNode: node, requirementJSON: []byte(test.document)}
			parent.target = func(ctx context.Context, request platform.TargetRequest) (platform.ExecResult, error) {
				var payload targetPayload
				if err := json.Unmarshal(request.Payload, &payload); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(payload.Requirement, opts.requirementJSON) {
					t.Fatal("caller document did not travel with target request")
				}
				if err := bindTargetRequirement(&payload); err != nil {
					t.Fatal(err)
				}
				worker := targetServices(t, 1000)
				worker.worker = &payload
				report, err := evaluateCurrentServices(ctx, payload.Options, worker)
				if err != nil {
					t.Fatal(err)
				}
				data, err := json.Marshal(report)
				return platform.ExecResult{Stdout: data}, err
			}
			report, err := evaluateCurrentServices(t.Context(), opts, parent)
			if err != nil {
				t.Fatal(err)
			}
			if exitCode(report.Evaluation.Requirement.State) != test.code || report.Evaluation.Target.UID != 1000 ||
				report.Evaluation.Current.UID != 0 || report.Evaluation.Execution.UID != 1000 {
				t.Fatal("delegated requirement lost result or identity binding")
			}
		})
	}
}

func TestLiveRequirementDoesNotOverrideFixtureOrHostMode(t *testing.T) {
	t.Parallel()
	for _, opts := range []Options{
		{Requirement: "requirements.json"},
		{Fixture: "fixture", Requirement: "requirements.json"},
	} {
		if validOptions(opts) {
			t.Fatal("requirement accepted outside selected live Podman scope")
		}
	}
}

func TestCustomPassiveRequirementRetainsKnownDeferredPredicate(t *testing.T) {
	t.Parallel()
	node, err := config.ParseRequirement([]byte(`{"capability":"runtime.podman.info"}`))
	if err != nil {
		t.Fatal(err)
	}
	services, _ := activeServices(t)
	services.runner = func() platform.CommandRunner {
		t.Fatal("custom requirement granted active command authority")
		return nil
	}
	report, err := evaluateCurrentServices(t.Context(), Options{Runtime: testPodmanRuntime,
		Requirement: testCallerRequirementPath, requirementNode: node}, services)
	if err != nil {
		t.Fatal(err)
	}
	if c, known := report.Capabilities["runtime.podman.info"]; !known || c.State != "unknown" ||
		report.Evaluation.Requirement.State != testIndeterminate {
		t.Fatal("delivered deferred predicate became an unregistered capability or definite result")
	}
}
