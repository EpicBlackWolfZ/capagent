package contract_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/app"
	"github.com/EpicBlackWolfZ/capagent/internal/config"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
	"github.com/EpicBlackWolfZ/capagent/internal/requirement"
)

const liveRequirementMode = "live"

func TestLiveRequirementDocumentBoundary(t *testing.T) {
	t.Parallel()
	schema := compileSchema(t)
	for _, test := range []struct {
		name, document string
		code           int
	}{
		{"satisfied absence", `{"not":{"capability":"runtime.podman"}}`, app.ExitSatisfied},
		{"unsatisfied presence", `{"capability":"runtime.podman"}`, app.ExitUnsatisfied},
		{"unmeasured predicate", `{"capability":"runtime.podman.not_delivered"}`, app.ExitIndeterminate},
		{"malformed", `{"raw-secret"`, app.ExitUsage},
		{"unknown member", `{"raw-secret":true}`, app.ExitUsage},
		{"null document", `null`, app.ExitUsage},
		{"duplicate member", `{"all":[],"all":[]}`, app.ExitUsage},
		{"incomplete bounded read", `{"all":[]}` + strings.Repeat(" ", config.MaxRequirementBytes), app.ExitExecution},
		{"missing document", "", app.ExitExecution},
		{"in-root symlink", `{"not":{"capability":"runtime.podman"}}`, app.ExitSatisfied},
		{"escaping symlink", `{"all":[]}`, app.ExitExecution},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			directory := filepath.Join(root, "selected")
			if err := os.Mkdir(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			selected := filepath.Join(directory, "requirement.json")
			if test.name != "missing document" {
				if err := os.WriteFile(selected, []byte(test.document), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if test.name == "in-root symlink" || test.name == "escaping symlink" {
				link := filepath.Join(directory, "link.json")
				destination := "requirement.json"
				if test.name == "escaping symlink" {
					destination = "../outside.json"
					if err := os.WriteFile(filepath.Join(root, "outside.json"), []byte(test.document), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(destination, link); err != nil {
					t.Fatal(err)
				}
				selected = link
			}
			var stdout, stderr bytes.Buffer
			code := app.Execute(t.Context(), app.Options{Runtime: "podman", Requirement: selected,
				PodmanPath: filepath.Join(root, "absent-podman")}, &stdout, &stderr)
			if code != test.code {
				t.Fatalf("exit=%d want=%d: %s", code, test.code, &stderr)
			}
			if strings.Contains(stderr.String(), "raw-secret") {
				t.Fatal("raw requirement input leaked")
			}
			if test.code == app.ExitUsage || test.code == app.ExitExecution {
				if stdout.Len() != 0 {
					t.Fatal("failed document input produced an assessment")
				}
				return
			}
			if err := validateJSON(t, schema, stdout.Bytes()); err != nil {
				t.Fatal(err)
			}
			report, err := output.Unmarshal(stdout.Bytes())
			if err != nil || report.Evaluation.Mode != liveRequirementMode || report.Evaluation.Collection != "passive" {
				t.Fatal("requirement changed native collection policy", err)
			}
		})
	}
}

func TestPublishedLiveRequirementsUseDeliveredPredicates(t *testing.T) {
	t.Parallel()
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, fixtureRelativeRoot, "network-rootless", "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	report, err := output.Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"inspection", "rootless-quadlet", "rootful-quadlet", "rootless-quadlet-without-login"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join(root, "examples/requirements", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			node, err := config.ParseRequirement(data)
			if err != nil {
				t.Fatal(err)
			}
			ids := map[string]bool{}
			var visit func(*requirement.Node)
			visit = func(node *requirement.Node) {
				if node == nil {
					return
				}
				if node.Capability != "" {
					id := string(node.Capability)
					ids[id] = true
					if _, delivered := report.Capabilities[id]; !delivered {
						t.Error("example uses an undelivered capability", id)
					}
				}
				visit(node.Not)
				for _, children := range [][]*requirement.Node{node.All, node.Any} {
					for _, child := range children {
						visit(child)
					}
				}
			}
			visit(node)
			if name == "rootful-quadlet" && (ids["runtime.podman.quadlet.runtime_directory"] || ids["runtime.podman.quadlet.linger"]) {
				t.Fatal("rootful example requires a rootless session")
			}
			if ids["runtime.podman.quadlet.linger"] != (name == "rootless-quadlet-without-login") {
				t.Fatal("linger became an unconditional prerequisite")
			}
		})
	}
}
