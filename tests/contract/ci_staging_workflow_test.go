package contract_test

import (
	json "encoding/json/v2"
	"fmt"
	"slices"
	"strings"
	"testing"
)

const (
	stageLint      = "lint"
	stageTest      = "test"
	stageVulncheck = "vulncheck"
	stageGitleaks  = "gitleaks"
	stageBuild     = "build"
	stageFuzz      = "fuzz"
	condAlways     = "always()"
)

func normalizeNeeds(raw any) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	switch val := raw.(type) {
	case string:
		trimmed := strings.TrimSpace(val)
		if trimmed == "" {
			return nil, fmt.Errorf("empty string in needs")
		}
		return []string{trimmed}, nil
	case []any:
		seen := make(map[string]struct{}, len(val))
		result := make([]string, 0, len(val))
		for idx, item := range val {
			str, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("needs item at index %d is not a string: %T", idx, item)
			}
			trimmed := strings.TrimSpace(str)
			if trimmed == "" {
				return nil, fmt.Errorf("needs item at index %d is empty", idx)
			}
			if _, exists := seen[trimmed]; exists {
				return nil, fmt.Errorf("duplicate dependency in needs: %q", trimmed)
			}
			seen[trimmed] = struct{}{}
			result = append(result, trimmed)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported needs type: %T", raw)
	}
}

func TestWorkflow_NormalizeNeedsHelper(t *testing.T) {
	t.Parallel()

	const (
		depA = "dep-a"
		depB = "dep-b"
		depC = "dep-c"
	)

	tests := []struct {
		name      string
		input     any
		want      []string
		wantError bool
	}{
		{
			name:      "nil input returns nil",
			input:     nil,
			want:      nil,
			wantError: false,
		},
		{
			name:      "valid single string",
			input:     depA,
			want:      []string{depA},
			wantError: false,
		},
		{
			name:      "empty string fails",
			input:     "   ",
			want:      nil,
			wantError: true,
		},
		{
			name:      "valid slice of strings",
			input:     []any{depA, depB, depC},
			want:      []string{depA, depB, depC},
			wantError: false,
		},
		{
			name:      "duplicate item fails",
			input:     []any{depA, depB, depA},
			want:      nil,
			wantError: true,
		},
		{
			name:      "non-string item in slice fails",
			input:     []any{depA, 123},
			want:      nil,
			wantError: true,
		},
		{
			name:      "empty item in slice fails",
			input:     []any{depA, ""},
			want:      nil,
			wantError: true,
		},
		{
			name:      "unsupported map type fails",
			input:     map[string]any{"job": depA},
			want:      nil,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := normalizeNeeds(tt.input)
			if (err != nil) != tt.wantError {
				t.Fatalf("normalizeNeeds(%v) error = %v, wantError = %v", tt.input, err, tt.wantError)
			}
			if !tt.wantError && !slices.Equal(got, tt.want) {
				t.Fatalf("normalizeNeeds(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestWorkflow_CIStagingDependencies(t *testing.T) {
	t.Parallel()

	ci := readWorkflow(t, ".github/workflows/ci.yml")
	jobs, ok := ci["jobs"].(map[string]any)
	if !ok {
		t.Fatal("ci.yml missing jobs map")
	}

	expectedPrereqs := map[string][]string{
		"pr-lint":        nil,
		stageLint:        nil,
		stageVulncheck:   nil,
		stageGitleaks:    nil,
		stageTest:        {stageLint, stageVulncheck, stageGitleaks},
		stageBuild:       {stageLint, stageVulncheck, stageGitleaks},
		stageFuzz:        {stageLint, stageVulncheck, stageGitleaks},
		"hardening-gate": {stageLint, stageTest, stageVulncheck, stageGitleaks, stageBuild, stageFuzz},
	}

	for jobID, wantPrereqs := range expectedPrereqs {
		rawJob, exists := jobs[jobID]
		if !exists {
			t.Fatalf("required job %q does not exist in ci.yml", jobID)
		}
		jobMap, ok := rawJob.(map[string]any)
		if !ok {
			t.Fatalf("job %q is not a map", jobID)
		}

		gotNeeds, err := normalizeNeeds(jobMap["needs"])
		if err != nil {
			t.Fatalf("job %q has invalid needs: %v", jobID, err)
		}

		// Verify all referenced jobs in needs actually exist
		for _, dep := range gotNeeds {
			if _, exists := jobs[dep]; !exists {
				t.Errorf("job %q references non-existent job %q in needs", jobID, dep)
			}
		}

		// Verify no pr-lint dependency anywhere
		if slices.Contains(gotNeeds, "pr-lint") {
			t.Errorf("job %q must not depend on pr-lint", jobID)
		}

		// Sort both for unordered set comparison
		gotSorted := slices.Clone(gotNeeds)
		wantSorted := slices.Clone(wantPrereqs)
		slices.Sort(gotSorted)
		slices.Sort(wantSorted)

		if !slices.Equal(gotSorted, wantSorted) {
			t.Errorf("job %q needs = %v, want exact set %v", jobID, gotNeeds, wantPrereqs)
		}
	}

	// Verify heavy jobs have no dependencies on each other
	heavyJobs := []string{stageTest, stageBuild, stageFuzz}
	for _, id := range heavyJobs {
		jobMap := jobs[id].(map[string]any)
		needs, _ := normalizeNeeds(jobMap["needs"])
		for _, other := range heavyJobs {
			if slices.Contains(needs, other) {
				t.Errorf("heavy job %q must not depend on other heavy job %q", id, other)
			}
		}
	}

	// Verify none of the six verification jobs has a job-level condition
	verificationJobs := []string{stageLint, stageVulncheck, stageGitleaks, stageTest, stageBuild, stageFuzz}
	for _, id := range verificationJobs {
		jobMap := jobs[id].(map[string]any)
		if cond, exists := jobMap["if"]; exists && cond != nil {
			t.Errorf("verification job %q must not have job-level condition: %v", id, cond)
		}
	}
}

func TestWorkflow_CIGateIdentityAndOutcomes(t *testing.T) {
	t.Parallel()

	ci := readWorkflow(t, ".github/workflows/ci.yml")
	jobs, ok := ci["jobs"].(map[string]any)
	if !ok {
		t.Fatal("ci.yml missing jobs map")
	}

	gateRaw, exists := jobs["hardening-gate"]
	if !exists {
		t.Fatal("ci.yml missing hardening-gate job")
	}
	gate, ok := gateRaw.(map[string]any)
	if !ok {
		t.Fatal("hardening-gate job is not a map")
	}

	const expectedGateName = "CI Gate"
	if name, _ := gate["name"].(string); name != expectedGateName {
		t.Errorf("hardening-gate name = %q, want %q", name, expectedGateName)
	}

	if cond, _ := gate["if"].(string); cond != condAlways {
		t.Errorf("hardening-gate job-level if = %q, want %q", cond, condAlways)
	}

	steps, ok := gate["steps"].([]any)
	if !ok {
		t.Fatal("hardening-gate missing steps slice")
	}

	var reportStep map[string]any
	for _, stepRaw := range steps {
		step, ok := stepRaw.(map[string]any)
		if !ok {
			continue
		}
		if runCmd, _ := step["run"].(string); strings.Contains(runCmd, "scripts/gate.py report") {
			reportStep = step
			break
		}
	}

	if reportStep == nil {
		t.Fatal("hardening-gate missing step that runs scripts/gate.py report")
	}

	if cond, _ := reportStep["if"].(string); cond != condAlways {
		t.Errorf("report step if = %q, want %q", cond, condAlways)
	}

	envMap, ok := reportStep["env"].(map[string]any)
	if !ok {
		t.Fatal("report step missing env map")
	}

	resultsRaw, exists := envMap["CAPAGENT_JOB_RESULTS"]
	if !exists {
		t.Fatal("report step missing CAPAGENT_JOB_RESULTS environment variable")
	}

	resultsStr, ok := resultsRaw.(string)
	if !ok {
		t.Fatalf("CAPAGENT_JOB_RESULTS is not a string: %T", resultsRaw)
	}

	var outcomes map[string]string
	if err := json.Unmarshal([]byte(resultsStr), &outcomes); err != nil {
		t.Fatalf("failed to parse CAPAGENT_JOB_RESULTS as JSON: %v", err)
	}

	requiredStages := []string{stageLint, stageTest, stageVulncheck, stageGitleaks, stageBuild, stageFuzz}
	if len(outcomes) != len(requiredStages) {
		t.Errorf("CAPAGENT_JOB_RESULTS has %d entries, want %d", len(outcomes), len(requiredStages))
	}

	for _, stage := range requiredStages {
		val, exists := outcomes[stage]
		if !exists {
			t.Errorf("CAPAGENT_JOB_RESULTS missing stage %q", stage)
			continue
		}
		expectedExpr := fmt.Sprintf("${{ needs.%s.result }}", stage)
		if val != expectedExpr {
			t.Errorf("CAPAGENT_JOB_RESULTS[%q] = %q, want %q", stage, val, expectedExpr)
		}
	}
}

func TestWorkflow_CIStagingPreservesTriggers(t *testing.T) {
	t.Parallel()

	ci := readWorkflow(t, ".github/workflows/ci.yml")

	// Verify top-level triggers
	triggers, ok := ci["on"].(map[string]any)
	if !ok {
		t.Fatal("ci.yml missing on triggers")
	}

	pr, ok := triggers["pull_request"].(map[string]any)
	if !ok {
		t.Fatal("ci.yml missing pull_request trigger")
	}

	// PR path filters must remain absent
	if paths, exists := pr["paths"]; exists && paths != nil {
		t.Errorf("pull_request must not define paths: %v", paths)
	}
	if pathsIgnore, exists := pr["paths-ignore"]; exists && pathsIgnore != nil {
		t.Errorf("pull_request must not define paths-ignore: %v", pathsIgnore)
	}

	// PR activity types must remain opened, synchronize, reopened, edited
	typesRaw, ok := pr["types"].([]any)
	if !ok {
		t.Fatal("pull_request missing types slice")
	}
	var types []string
	for _, item := range typesRaw {
		if s, ok := item.(string); ok {
			types = append(types, s)
		}
	}
	expectedTypes := []string{"opened", "synchronize", "reopened", "edited"}
	slices.Sort(types)
	slices.Sort(expectedTypes)
	if !slices.Equal(types, expectedTypes) {
		t.Errorf("pull_request types = %v, want %v", types, expectedTypes)
	}

	// Push trigger must be preserved
	push, ok := triggers["push"].(map[string]any)
	if !ok {
		t.Fatal("ci.yml missing push trigger")
	}
	branchesRaw, _ := push["branches"].([]any)
	var branches []string
	for _, b := range branchesRaw {
		if s, ok := b.(string); ok {
			branches = append(branches, s)
		}
	}
	if !slices.Equal(branches, []string{"main"}) {
		t.Errorf("push branches = %v, want [main]", branches)
	}

	pathsIgnoreRaw, _ := push["paths-ignore"].([]any)
	var pathsIgnore []string
	for _, p := range pathsIgnoreRaw {
		if s, ok := p.(string); ok {
			pathsIgnore = append(pathsIgnore, s)
		}
	}
	expectedPathsIgnore := []string{"**.md", "docs/**", "LICENSE*", ".gitignore"}
	slices.Sort(pathsIgnore)
	slices.Sort(expectedPathsIgnore)
	if !slices.Equal(pathsIgnore, expectedPathsIgnore) {
		t.Errorf("push paths-ignore = %v, want %v", pathsIgnore, expectedPathsIgnore)
	}

	// workflow_dispatch must remain present
	if _, exists := triggers["workflow_dispatch"]; !exists {
		t.Error("ci.yml missing workflow_dispatch trigger")
	}

	// Concurrency must be preserved
	concurrency, ok := ci["concurrency"].(map[string]any)
	if !ok {
		t.Fatal("ci.yml missing concurrency")
	}
	if group, _ := concurrency["group"].(string); group != "${{ github.workflow }}-${{ github.ref }}" {
		t.Errorf("concurrency group = %q, want ${{ github.workflow }}-${{ github.ref }}", group)
	}
	if cancel, _ := concurrency["cancel-in-progress"].(bool); !cancel {
		t.Errorf("concurrency cancel-in-progress = %v, want true", cancel)
	}
}
