package contract_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

var actionCommit = regexp.MustCompile(`^[^@]+@[a-f0-9]{40}$`)

func scanActionPins(value any) []string {
	var failures []string
	switch node := value.(type) {
	case map[string]any:
		for key, child := range node {
			if key == "uses" {
				ref, ok := child.(string)
				if !ok || (!strings.HasPrefix(ref, "./") && !actionCommit.MatchString(ref)) {
					failures = append(failures, fmt.Sprintf("unpinned action: %v", child))
				}
			}
			failures = append(failures, scanActionPins(child)...)
		}
	case []any:
		for _, child := range node {
			failures = append(failures, scanActionPins(child)...)
		}
	}
	return failures
}

func readWorkflow(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(findRepoRoot(t), path))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func TestWorkflow_ImmutableActions(t *testing.T) {
	t.Parallel()
	root := findRepoRoot(t)
	err := filepath.WalkDir(filepath.Join(root, ".github"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".yml") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		for _, failure := range scanActionPins(readWorkflow(t, rel)) {
			t.Errorf("%s: %s", rel, failure)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWorkflow_ActionPinScanner(t *testing.T) {
	t.Parallel()
	tests := []struct {
		ref   string
		valid bool
	}{
		{"actions/checkout@v7", false},
		{"actions/checkout@main", false},
		{"actions/checkout@" + strings.Repeat("a", 40), true},
		{"./.github/actions/setup-build", true},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			t.Parallel()
			got := scanActionPins(map[string]any{"steps": []any{map[string]any{"uses": tt.ref}}})
			if (len(got) == 0) != tt.valid {
				t.Fatalf("violations: %v", got)
			}
		})
	}
}

func TestWorkflow_ReadOnlyValidationAndRequiredChecks(t *testing.T) {
	t.Parallel()
	ci := readWorkflow(t, ".github/workflows/ci.yml")
	permissions := ci["permissions"].(map[string]any)
	if len(permissions) != 1 || permissions["contents"] != "read" {
		t.Fatalf("CI must default to contents: read: %v", permissions)
	}
	required := map[string]string{
		"pr-lint":   "Validate Conventional PR Title",
		"lint":      "Lint (golangci-lint)",
		"test":      "Unit Tests & Coverage Gate (>= 95%)",
		"vulncheck": "Vulnerability Scan (govulncheck)",
		"gitleaks":  "Secrets Detection (gitleaks)",
		"build":     "Build, GoReleaser Snapshot & Self-Bundling Verification",
	}
	jobs := ci["jobs"].(map[string]any)
	for id, name := range required {
		job, ok := jobs[id].(map[string]any)
		if !ok || job["name"] != name {
			t.Errorf("required check renamed or removed: %s", name)
		}
	}
	for id, raw := range jobs {
		job := raw.(map[string]any)
		if permissions, ok := job["permissions"].(map[string]any); ok {
			for key, value := range permissions {
				if value == "write" {
					t.Errorf("CI job %s grants %s write", id, key)
				}
			}
		}
	}
	build := jobs["build"].(map[string]any)
	if !strings.Contains(fmt.Sprint(build["steps"]), "scripts/gate.py stage --stage build") {
		t.Fatal("required build job does not run release rehearsal")
	}
}

func TestWorkflow_PublishingIsAnIsolatedTagOnlyHandoff(t *testing.T) {
	t.Parallel()
	release := readWorkflow(t, ".github/workflows/release.yml")
	permissions := release["permissions"].(map[string]any)
	if len(permissions) != 1 || permissions["contents"] != "read" {
		t.Fatal("release build must default to read-only")
	}
	jobs := release["jobs"].(map[string]any)
	publisher, ok := jobs["publish"].(map[string]any)
	if !ok {
		t.Fatal("missing isolated publisher")
	}
	condition := fmt.Sprint(publisher["if"])
	for _, guard := range []string{"github.event_name == 'push'", "refs/tags/v", "EpicBlackWolfZ/capagent"} {
		if !strings.Contains(condition, guard) {
			t.Errorf("publisher lacks guard %s", guard)
		}
	}
	steps := fmt.Sprint(publisher["steps"])
	for _, forbidden := range []string{"actions/checkout@", "goreleaser", "make ", "go build", "secrets: inherit"} {
		if strings.Contains(steps, forbidden) {
			t.Errorf("publisher must not check out or build code: %s", forbidden)
		}
	}
	for _, required := range []string{"artifact-ids", "needs.build.outputs.sha256", "cosign verify-blob", "--verify-tag"} {
		if !strings.Contains(steps, required) {
			t.Errorf("missing publisher handoff control: %s", required)
		}
	}
}

func TestWorkflow_ValidationHasNoCredentialEscalation(t *testing.T) {
	t.Parallel()
	for _, path := range []string{".github/workflows/ci.yml", ".github/workflows/release.yml"} {
		document := readWorkflow(t, path)
		triggers := document["on"].(map[string]any)
		for _, trigger := range []string{"pull_request_target", "workflow_run"} {
			if _, exists := triggers[trigger]; exists {
				t.Errorf("%s: unexpected privileged trigger %s", path, trigger)
			}
		}
		for id, raw := range document["jobs"].(map[string]any) {
			job := raw.(map[string]any)
			if id == "publish" && strings.HasSuffix(path, "release.yml") {
				continue
			}
			if permissions, ok := job["permissions"].(map[string]any); ok {
				for permission, level := range permissions {
					if level == "write" {
						t.Errorf("%s/%s: validation grants %s write", path, id, permission)
					}
				}
			}
			for _, rawStep := range job["steps"].([]any) {
				step := rawStep.(map[string]any)
				if strings.HasPrefix(fmt.Sprint(step["uses"]), "actions/checkout@") {
					inputs, ok := step["with"].(map[string]any)
					if !ok || inputs["persist-credentials"] != false {
						t.Errorf("%s/%s: checkout retains credentials", path, id)
					}
				}
			}
		}
	}
}
