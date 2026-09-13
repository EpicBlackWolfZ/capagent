package app

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTargetOptionsAndNoFallback(t *testing.T) {
	t.Parallel()
	for _, selector := range []string{"user:root", "uid:0", "uid:1000"} {
		if !validOptions(Options{Context: selector}) {
			t.Fatalf("selector rejected: %s", selector)
		}
	}
	mem := platform.NewMemPlatformReader()
	mem.AddDir("/etc", 0o755)
	mem.AddFile("/etc/passwd", []byte("alice:x:1000:1000::/home/alice:/bin/sh\n"), 0o644)
	mem.AddFile("/etc/group", []byte("alice:x:1000:\n"), 0o644)
	files := platform.NewScopedMemReader("/", mem)
	defer files.Close()
	creds := model.CurrentCredentials{UID: 1000, EUID: 1000, GID: 1000, EGID: 1000, GroupsKnown: true}
	for _, selector := range []string{"user:absent", "uid:0"} {
		report, err := evaluateCurrent(t.Context(), Options{Context: selector}, files, creds, nil, func() time.Time { return time.Unix(1, 0) })
		if err != nil {
			t.Fatal(err)
		}
		if report.Context.UID != nil || report.Evaluation.Target != nil ||
			report.Evaluation.Execution != nil || hostExit(report) != ExitIndeterminate {
			t.Fatalf("unresolved target fell back to caller: %+v", report.Context)
		}
	}
}

func targetServices(t *testing.T, uid uint32) currentServices {
	t.Helper()
	mem := platform.NewMemPlatformReader()
	for _, dir := range []string{"/etc", "/proc", "/proc/self", "/proc/self/ns"} {
		mem.AddDir(dir, 0o755)
	}
	mem.AddFile("/etc/passwd", []byte("root:x:0:0::/root:/bin/sh\nalice:x:1000:1000::/home/alice:/bin/sh\n"), 0o644)
	mem.AddFile("/etc/group", []byte("root:x:0:\nalice:x:1000:\n"), 0o644)
	for _, kind := range []string{"user", "pid", "net", "mnt", "ipc", "uts", "cgroup"} {
		mem.AddSymlink("/proc/self/ns/"+kind, kind+":[1]")
	}
	files := platform.NewScopedMemReader("/", mem)
	t.Cleanup(func() {
		if err := files.Close(); err != nil {
			t.Error(err)
		}
	})
	return currentServices{files: files, credentials: model.CurrentCredentials{UID: uid, EUID: uid, GID: uid, EGID: uid, GroupsKnown: true,
		Groups: []uint32{uid}}, now: func() time.Time { return time.Unix(1, 0) }}
}

func TestDelegatedReportBinding(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"bound response", "run error", "exit", "truncated", "invalid", "nil", "scope", "execution", "launcher",
		"schema", "selection", "namespace", "namespace count", "missing UID"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			parent := targetServices(t, 0)
			parent.target = func(ctx context.Context, r platform.TargetRequest) (platform.ExecResult, error) {
				var payload targetPayload
				if err := json.Unmarshal(r.Payload, &payload); err != nil {
					t.Fatal(err)
				}
				worker := targetServices(t, 1000)
				worker.worker = &payload
				report, err := evaluateCurrentServices(ctx, payload.Options, worker)
				if err != nil {
					t.Fatal(err)
				}
				if name == "scope" {
					report.Evaluation.Scope.RunID = "different"
				}
				if name == "execution" {
					report.Evaluation.Execution = nil
				}
				if name == "launcher" {
					report.Evaluation.Current.UID = 10
				}
				if name == "selection" {
					report.Evaluation.Selection = "uid:2"
				}
				if name == "namespace" {
					report.Evaluation.Namespaces[0].ID = "user:[999]"
				}
				if name == "namespace count" {
					report.Evaluation.Namespaces = nil
				}
				if name == "schema" {
					report.SchemaVersion = 999
				}
				data, err := json.Marshal(report)
				if err != nil {
					t.Fatal(err)
				}
				result := platform.ExecResult{Stdout: data}
				switch name {
				case "run error":
					return result, errors.New("failure")
				case "exit":
					result.ExitCode = 70
				case "truncated":
					result.StdoutTruncated = true
				case "invalid":
					result.Stdout = []byte("{")
				case "missing UID":
					result.Stdout = bytes.ReplaceAll(data, []byte(`"uid":1000,`), nil)
				case "nil":
					result.Stdout = []byte("null")
				}
				return result, nil
			}
			report, err := evaluateCurrentServices(t.Context(), Options{Context: "uid:1000"}, parent)
			if (err == nil) != (name == "bound response") {
				t.Fatal(err)
			}
			if err == nil && (report.Evaluation.Current.UID != 0 || report.Evaluation.Target.UID != 1000 ||
				report.Evaluation.Execution.UID != 1000) {
				t.Fatal("lost identity separation")
			}
		})
	}
}

func TestSameUserTargetAndDeniedDelegation(t *testing.T) {
	t.Parallel()
	services := targetServices(t, 1000)
	for _, selector := range []string{"uid:1000", "user:alice", "uid:0"} {
		report, err := evaluateCurrentServices(t.Context(), Options{Context: selector}, services)
		if err != nil {
			t.Fatal(err)
		}
		if selector == "uid:0" {
			if report.Evaluation.Target.UID != 0 || report.Evaluation.Execution != nil {
				t.Fatal("delegated without authority")
			}
		} else {
			if report.Evaluation.Execution == nil || report.Evaluation.Execution.UID != 1000 {
				t.Fatal("lost matching target")
			}
		}
	}
	policy, err := targetEnvironment(model.UserIdentity{UID: 1000, HomeDir: "/home/alice"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(policy.Variables(), []string{"HOME=/home/alice", "LC_ALL=C",
		"PATH=/usr/bin:/bin", "XDG_RUNTIME_DIR=/run/user/1000"}) {
		t.Fatal("ambient environment leaked")
	}
	if _, err := targetEnvironment(model.UserIdentity{}); err == nil {
		t.Fatal("missing home accepted")
	}
	if sameOutputIdentity(nil, &model.UserIdentity{}) {
		t.Fatal("missing execution accepted")
	}
}

func TestWorkerApplicationBoundary(t *testing.T) {
	t.Parallel()
	c, err := platform.CurrentCredentials()
	if err != nil {
		t.Fatal(err)
	}
	user := &model.UserIdentity{UID: c.EUID, GID: c.EGID, GroupsKnown: c.GroupsKnown, SupplementaryGroups: c.Groups}
	scope := model.EvaluationScope{RunID: "worker-test", ContextID: "worker-current"}
	payload := targetPayload{Scope: scope, Launcher: user, Target: user}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"valid", "bootstrap", "payload", "mismatch", "write"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			receive := func(context.Context) (platform.TargetRequest, error) {
				r := platform.TargetRequest{RunID: scope.RunID, ContextID: scope.ContextID, Target: *user, Payload: data}
				if name == "bootstrap" {
					return r, errors.New("secret failure")
				}
				if name == "payload" {
					r.Payload = []byte("{")
				}
				if name == "mismatch" {
					r.RunID = "different"
				}
				return r, nil
			}
			var code int
			if name == "write" {
				code = executeTargetWorker(t.Context(), failingWriter{}, &stderr, receive)
			} else {
				code = executeTargetWorker(t.Context(), &stdout, &stderr, receive)
			}
			if (code == 0) != (name == "valid") {
				t.Fatalf("%d: %s", code, stderr.String())
			}
			if strings.Contains(stderr.String(), "secret") {
				t.Fatal("raw error leaked")
			}
			if name == "valid" {
				if _, err := output.Unmarshal(stdout.Bytes()); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
