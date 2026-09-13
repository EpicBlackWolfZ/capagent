package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/config"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

func TestExplanationPreservesJSONAndIdentifiesMissingEvidence(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		fixture   string
		code      int
		fragments []string
	}{
		{"assessment-rootful", ExitSatisfied, []string{"system manager: running=true", "cgroup mode=v2"}},
		{"network-helper-missing", ExitUnsatisfied, []string{
			"UNSATISFIED", "Target uid=1000", "runtime.podman.aardvark_dns.executable: misconfigured",
			"/usr/libexec/podman/aardvark-dns", "present=false", "selected evidence"}},
		{"network-helper-denied", ExitIndeterminate, []string{
			"INDETERMINATE", "runtime.podman.aardvark_dns.executable: unknown", "configured_candidate_access_unknown"}},
		{"network-source-denied", ExitIndeterminate, []string{
			"configuration source", "/etc/containers/containers.conf", "status=denied", "excluded evidence"}},
		{"engine-invalid-value", ExitUnsatisfied, []string{"cgroup_manager", "invalid selected value", "redacted"}},
		{"engine-runtime-override", ExitUnsatisfied, []string{"cgroupfs", "systemd", "superseded evidence"}},
		{"network-runtime-conflict", ExitUnsatisfied, []string{"backend=cni", "slirp4netns", "superseded evidence"}},
	} {
		t.Run(test.fixture, func(t *testing.T) {
			t.Parallel()
			opts := Options{Fixture: filepath.Join("../../testdata/fixtures/v1", test.fixture), Pretty: true}
			var original, originalErrors, explained, explanation bytes.Buffer
			if code := Execute(t.Context(), opts, &original, &originalErrors); code != test.code {
				t.Fatal("unexpected fixture baseline", code)
			}
			opts.Explain = true
			if code := Execute(t.Context(), opts, &explained, &explanation); code != test.code {
				t.Fatal("explanation changed requirement exit", code)
			}
			if !bytes.Equal(original.Bytes(), explained.Bytes()) {
				t.Fatal("explanation changed machine-readable JSON")
			}
			for _, fragment := range test.fragments {
				if !strings.Contains(explanation.String(), fragment) {
					t.Fatalf("missing useful explanation %q", fragment)
				}
			}
			if strings.Contains(explanation.String(), "synthetic-secret") {
				t.Fatal("explanation exposed redacted configuration values")
			}
			if test.fixture == "network-helper-missing" &&
				strings.Count(explanation.String(), "observation podman.configuration.network:") != 1 {
				t.Fatal("shared configuration observation repeated instead of referenced")
			}
		})
	}
}

func TestExplanationBoundsAndTerminalSanitization(t *testing.T) {
	t.Parallel()
	const id = "runtime.podman.example"
	r := output.NewReport()
	r.Evaluation = output.NewEvaluationTrace(model.EvaluationScope{RunID: "explanation", ContextID: "explanation-current",
		Runtime: testPodmanRuntime, Endpoint: "local"}, time.Unix(1, 0), "synthetic")
	r.Evaluation.Target = &output.Identity{UID: 1000}
	r.Context.TargetUser = "alice\x1b[31m"
	r.Capabilities[id] = output.CapabilityReport{State: "unknown", Confidence: "unknown", Evidence: []string{}}
	r.Evaluation.Observations = []output.ObservationRecord{{ID: "observed", Diagnostics: []output.Diagnostic{
		{Code: "measurement\ncode", Message: "PRIVATE_CREDENTIAL"}}}}
	const evidenceCount = 2000
	for i := range evidenceCount {
		r.Evaluation.Evidence = append(r.Evaluation.Evidence, output.EvidenceRecord{ID: "evidence-" + strconv.Itoa(i),
			Claim: id, State: "unknown", Completeness: "partial", Observations: []string{"observed"}})
	}
	node, err := config.ParseRequirement([]byte(`{"capability":"runtime.podman.example"}`))
	if err != nil {
		t.Fatal(err)
	}
	var text bytes.Buffer
	if err := writeExplanation(r, Options{requirementNode: node}, &text); err != nil {
		t.Fatal(err)
	}
	if text.Len() > maxExplanationBytes || !strings.HasSuffix(text.String(), explanationTruncated) {
		t.Fatal("explanation size bound or explicit truncation marker lost")
	}
	if strings.ContainsAny(text.String(), "\x1b\r") || strings.Contains(text.String(), "PRIVATE_CREDENTIAL") ||
		!strings.Contains(text.String(), `alice\x1b[31m`) || !strings.Contains(text.String(), `measurement\ncode`) {
		t.Fatal("terminal escaping or diagnostic redaction failed")
	}
	if err := writeExplanation(r, Options{requirementNode: node}, explanationShortWriter{}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal("short explanation write was accepted")
	}
}

type explanationShortWriter struct{}

func (explanationShortWriter) Write(data []byte) (int, error) { return len(data) - 1, nil }

func TestExplanationHostAndWriterFailure(t *testing.T) {
	t.Parallel()
	var report, text bytes.Buffer
	opts := Options{Fixture: "../../testdata/fixtures/v1/host-basic", Explain: true}
	if code := Execute(t.Context(), opts, &report, &text); code != ExitSatisfied || !strings.Contains(text.String(), "Host collection:") {
		t.Fatal("host explanation failed", code)
	}
	report.Reset()
	if code := Execute(t.Context(), opts, &report, failingWriter{}); code != ExitExecution || report.Len() != 0 {
		t.Fatal("failed explanation emitted a successful report", code)
	}
}

func TestInspectionExplanationRetainsOtherConfigurationProblems(t *testing.T) {
	t.Parallel()
	services, _ := activeServices(t)
	services.files = explanationConfigReader{ScopedReader: services.files}
	opts := Options{Runtime: testPodmanRuntime, Active: true, Explain: true}
	report, err := evaluateCurrentServices(t.Context(), opts, services)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := writeReport(report, opts, &stdout, &stderr); code != ExitSatisfied {
		t.Fatal("configuration finding changed the inspection requirement", code)
	}
	for _, fragment := range []string{"Additional finding outside this requirement", "cgroup_manager", "invalid selected value"} {
		if !strings.Contains(stderr.String(), fragment) {
			t.Fatal("inspection explanation omitted configuration problem", fragment)
		}
	}
}

type explanationConfigReader struct{ platform.ScopedReader }

func (r explanationConfigReader) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if name == "etc/containers/containers.conf" {
		return []byte("[engine]\ncgroup_manager='synthetic-secret'"), nil
	}
	return r.ScopedReader.ReadFile(ctx, name)
}
