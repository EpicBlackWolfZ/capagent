package app

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

func TestWorkerBootstrapUsesTransportedRequirement(t *testing.T) {
	t.Parallel()
	credentials, err := platform.CurrentCredentials()
	if err != nil {
		t.Fatal(err)
	}
	user := &model.UserIdentity{UID: credentials.EUID, GID: credentials.EGID, GroupsKnown: credentials.GroupsKnown,
		SupplementaryGroups: credentials.Groups}
	for _, scenario := range []string{
		"selected", "large valid document", "wide deep document", "missing document", "unexpected document", "invalid document",
	} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			scope := model.EvaluationScope{RunID: "worker-requirement", ContextID: "worker-request", Runtime: testPodmanRuntime, Endpoint: "local"}
			payload := targetPayload{Options: Options{Runtime: testPodmanRuntime,
				PodmanPath: filepath.Join(directory, "missing-podman"), Requirement: filepath.Join(directory, "caller-only.json")},
				Scope: scope, Launcher: user, Target: user, Requirement: []byte(`{"not":{"capability":"runtime.podman"}}`)}
			switch scenario {
			case "large valid document":
				document := `{"capability":"runtime.` + strings.Repeat("a", 63000) + `"}`
				for range 31 {
					document = `{"not":` + document + `}`
				}
				payload.Requirement = []byte(document)
			case "wide deep document":
				// 481 leaves + their all node + 30 not nodes reaches exactly
				// 512 nodes and depth 32, within the 64 KiB input envelope.
				leaves := make([]string, 481)
				for i := range leaves {
					leaves[i] = `{"capability":"runtime.` + strings.Repeat("a", 90) + strconv.Itoa(i) + `"}`
				}
				document := `{"all":[` + strings.Join(leaves, ",") + `]}`
				for range 30 {
					document = `{"not":` + document + `}`
				}
				payload.Requirement = []byte(document)
			case "missing document":
				payload.Requirement = nil
			case "unexpected document":
				payload.Options.Requirement = ""
			case "invalid document":
				payload.Requirement = []byte(`{"raw-secret":true}`)
			}
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			code := executeTargetWorker(t.Context(), &stdout, &stderr, func(context.Context) (platform.TargetRequest, error) {
				return platform.TargetRequest{RunID: scope.RunID, ContextID: scope.ContextID, Target: *user, Payload: data}, nil
			})
			if scenario != "selected" && scenario != "large valid document" && scenario != "wide deep document" {
				if code != ExitExecution || stdout.Len() != 0 || bytes.Contains(stderr.Bytes(), []byte("raw-secret")) {
					t.Fatal("worker accepted or exposed invalid requirement input", code)
				}
				return
			}
			if code != 0 {
				t.Fatal("worker tried to reread a caller-only pathname", code, stderr.String())
			}
			report, err := output.Unmarshal(stdout.Bytes())
			expected := "SATISFIED"
			if scenario == "large valid document" || scenario == "wide deep document" {
				expected = "INDETERMINATE"
			}
			if err != nil || report.Evaluation.Requirement.State != expected {
				t.Fatal("worker ignored the requested predicate", err)
			}
		})
	}
}
