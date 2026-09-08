package contract_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	modulePrefix     = "github.com/EpicBlackWolfZ/capagent/"
	pkgInternalModel = "internal/model"
)

// Rule defines an architectural import restriction for a package path prefix.
type Rule struct {
	// SourcePrefix is the package directory prefix this rule applies to (e.g. "internal/model").
	SourcePrefix string
	// DisallowedPrefixes is a list of package prefixes that SourcePrefix MUST NOT import.
	DisallowedPrefixes []string
	// AllowInternalOnly if set means the package may only import from these specific internal packages.
	AllowedInternal []string
	// StandardLibraryOnly if true means the package must have ZERO internal or third-party dependencies.
	StandardLibraryOnly bool
	// Rationale documents why the invariant exists.
	Rationale string
}

// ArchitectureRules defines the canonical package boundary rules from AGENTS.md and docs/architecture.md.
var ArchitectureRules = []Rule{
	{
		SourcePrefix:        pkgInternalModel,
		StandardLibraryOnly: true,
		Rationale:           "internal/model must consist of pure domain primitives with zero dependencies on other packages",
	},
	{
		SourcePrefix: "internal/requirement",
		AllowedInternal: []string{
			pkgInternalModel,
		},
		DisallowedPrefixes: []string{
			"internal/probe",
			"internal/platform",
			"internal/host",
			"internal/runtime",
			"internal/capability",
			"internal/config",
			"internal/knowledge",
			"internal/diagnostics",
			"cmd/",
		},
		Rationale: "internal/requirement encapsulates 3-valued Boolean logic and may only depend on internal/model",
	},
	{
		SourcePrefix: "internal/capability",
		DisallowedPrefixes: []string{
			"internal/host",
			"internal/runtime",
			"internal/probe",
			"cmd/",
		},
		Rationale: "internal/capability must evaluate purely over evidence graphs and never execute probes or runtime commands",
	},
	{
		SourcePrefix: "internal/runtime",
		DisallowedPrefixes: []string{
			"cmd/",
			"internal/diagnostics",
		},
		Rationale: "runtime adapters must not depend on CLI or diagnostics packages",
	},
}

// PackageImports maps package relative paths (e.g. "internal/model") to their imported packages.
type PackageImports map[string][]string

// CheckArchitecture inspects a PackageImports map against ArchitectureRules and returns all violations.
func CheckArchitecture(pkgs PackageImports, rules []Rule) []string {
	var violations []string

	for pkgPath, imports := range pkgs {
		for _, rule := range rules {
			if !strings.HasPrefix(pkgPath, rule.SourcePrefix) {
				continue
			}

			for _, imp := range imports {
				// Normalize module import path to relative repo path if within capagent module
				relImp := strings.TrimPrefix(imp, modulePrefix)
				isInternal := strings.HasPrefix(imp, modulePrefix) || strings.HasPrefix(imp, "internal/")

				// Check StandardLibraryOnly
				if rule.StandardLibraryOnly {
					if isInternal || strings.Contains(imp, ".") {
						violations = append(violations, fmt.Sprintf(
							"rule violation: %s imports %s (%s)",
							pkgPath, imp, rule.Rationale,
						))
					}
					continue
				}

				// Check AllowedInternal
				if len(rule.AllowedInternal) > 0 && isInternal {
					allowed := false
					for _, okPkg := range rule.AllowedInternal {
						if strings.HasPrefix(relImp, okPkg) {
							allowed = true
							break
						}
					}
					if !allowed {
						violations = append(violations, fmt.Sprintf(
							"rule violation: %s imports %s but is only permitted to import %v (%s)",
							pkgPath, imp, rule.AllowedInternal, rule.Rationale,
						))
					}
				}

				// Check DisallowedPrefixes
				for _, disallowed := range rule.DisallowedPrefixes {
					if strings.HasPrefix(relImp, disallowed) {
						violations = append(violations, fmt.Sprintf(
							"rule violation: %s imports %s which matches disallowed prefix %s (%s)",
							pkgPath, imp, disallowed, rule.Rationale,
						))
					}
				}
			}
		}
	}

	return violations
}

// collectPackageImports scans the filesystem starting from rootDir and extracts imports from non-test Go files.
func collectPackageImports(rootDir string) (PackageImports, error) {
	pkgImports := make(PackageImports)
	fset := token.NewFileSet()

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories, test files, and hidden/vendor/testdata paths
		if info.IsDir() {
			base := info.Name()
			if strings.HasPrefix(base, ".") || base == "vendor" || base == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		relPath, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}

		// Only inspect internal and cmd packages
		if !strings.HasPrefix(relPath, "internal") && !strings.HasPrefix(relPath, "cmd") {
			return nil
		}

		node, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", path, err)
		}

		pkgDir := filepath.Dir(relPath)
		for _, imp := range node.Imports {
			impPath := strings.Trim(imp.Path.Value, `"`)
			pkgImports[pkgDir] = append(pkgImports[pkgDir], impPath)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return pkgImports, nil
}

func findRepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root containing go.mod")
		}
		dir = parent
	}
}

// TestArchitecture_PackageBoundaries verifies that all non-test packages in the codebase
// strictly adhere to the unidirectional architectural boundaries.
func TestArchitecture_PackageBoundaries(t *testing.T) {
	t.Parallel()

	rootDir := findRepoRoot(t)
	pkgs, err := collectPackageImports(rootDir)
	if err != nil {
		t.Fatalf("failed to collect package imports: %v", err)
	}

	if len(pkgs) == 0 {
		t.Fatal("no packages found to check; ensure internal/ contains Go code")
	}

	violations := CheckArchitecture(pkgs, ArchitectureRules)
	if len(violations) > 0 {
		t.Errorf("Architecture boundary contract violations found:\n%s", strings.Join(violations, "\n"))
	}
}

// TestArchitecture_RuleEnforcement simulates forbidden imports and verifies that CheckArchitecture
// correctly detects each violation.
func TestArchitecture_RuleEnforcement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		imports       PackageImports
		wantViolation bool
	}{
		{
			name: "internal/model importing internal/requirement is rejected",
			imports: PackageImports{
				pkgInternalModel: {
					"github.com/EpicBlackWolfZ/capagent/internal/requirement",
				},
			},
			wantViolation: true,
		},
		{
			name: "internal/model importing third-party package is rejected",
			imports: PackageImports{
				pkgInternalModel: {
					"github.com/stretchr/testify/assert",
				},
			},
			wantViolation: true,
		},
		{
			name: "internal/model importing standard library is permitted",
			imports: PackageImports{
				pkgInternalModel: {
					"fmt",
					"time",
					"strings",
				},
			},
			wantViolation: false,
		},
		{
			name: "internal/requirement importing internal/probe is rejected",
			imports: PackageImports{
				"internal/requirement": {
					"github.com/EpicBlackWolfZ/capagent/internal/probe",
				},
			},
			wantViolation: true,
		},
		{
			name: "internal/requirement importing internal/model is permitted",
			imports: PackageImports{
				"internal/requirement": {
					"github.com/EpicBlackWolfZ/capagent/" + pkgInternalModel,
				},
			},
			wantViolation: false,
		},
		{
			name: "internal/capability importing internal/host is rejected",
			imports: PackageImports{
				"internal/capability": {
					"github.com/EpicBlackWolfZ/capagent/internal/host",
				},
			},
			wantViolation: true,
		},
		{
			name: "internal/capability importing internal/runtime is rejected",
			imports: PackageImports{
				"internal/capability": {
					"github.com/EpicBlackWolfZ/capagent/internal/runtime",
				},
			},
			wantViolation: true,
		},
		{
			name: "internal/runtime importing cmd/capagent is rejected",
			imports: PackageImports{
				"internal/runtime/podman": {
					"github.com/EpicBlackWolfZ/capagent/cmd/capagent",
				},
			},
			wantViolation: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			violations := CheckArchitecture(tt.imports, ArchitectureRules)
			if tt.wantViolation && len(violations) == 0 {
				t.Errorf("expected architecture violation for %s, but got none", tt.name)
			}
			if !tt.wantViolation && len(violations) > 0 {
				t.Errorf("unexpected architecture violation for %s: %v", tt.name, violations)
			}
		})
	}
}
