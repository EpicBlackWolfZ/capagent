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
	modulePrefix           = "github.com/EpicBlackWolfZ/capagent/"
	pkgInternalModel       = "internal/model"
	pkgInternalRequirement = "internal/requirement"
	pkgInternalCapability  = "internal/capability"
	pkgInternalProbe       = "internal/probe"
	pkgInternalPlatform    = "internal/platform"
	pkgInternalHost        = "internal/host"
	pkgInternalRuntime     = "internal/runtime"
	pkgInternalConfig      = "internal/config"
	pkgInternalKnowledge   = "internal/knowledge"
	pkgInternalDiagnostics = "internal/diagnostics"
	pkgCmdPrefix           = "cmd/"
	stretchrAssert         = "github.com/stretchr/testify/assert"
)

// Rule defines an architectural import restriction for a package path prefix.
type Rule struct {
	// SourcePrefix is the package directory prefix this rule applies to (e.g. "internal/model").
	SourcePrefix string
	// DisallowedPrefixes is a list of package prefixes that SourcePrefix MUST NOT import.
	DisallowedPrefixes []string
	// AllowedInternal if set means the package may only import from these specific internal packages and standard library.
	// Third-party packages are strictly forbidden unless AllowThirdParty is true.
	AllowedInternal []string
	// AllowThirdParty if true permits third-party external dependencies. Defaults to false.
	AllowThirdParty bool
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
		SourcePrefix: pkgInternalRequirement,
		AllowedInternal: []string{
			pkgInternalModel,
		},
		DisallowedPrefixes: []string{
			pkgInternalProbe,
			pkgInternalPlatform,
			pkgInternalHost,
			pkgInternalRuntime,
			pkgInternalCapability,
			pkgInternalConfig,
			pkgInternalKnowledge,
			pkgInternalDiagnostics,
			pkgCmdPrefix,
		},
		Rationale: "internal/requirement encapsulates 3-valued Boolean logic and may only depend on internal/model",
	},
	{
		SourcePrefix: pkgInternalCapability,
		AllowedInternal: []string{
			pkgInternalModel,
			pkgInternalRequirement,
		},
		DisallowedPrefixes: []string{
			pkgInternalPlatform,
			pkgInternalHost,
			pkgInternalRuntime,
			pkgInternalProbe,
			pkgInternalConfig,
			pkgInternalKnowledge,
			pkgInternalDiagnostics,
			pkgCmdPrefix,
		},
		Rationale: "internal/capability evaluates over evidence graphs only; no platform, runtime, probe, config, or CLI deps",
	},
	{
		SourcePrefix: pkgInternalPlatform,
		AllowedInternal: []string{
			pkgInternalModel,
		},
		DisallowedPrefixes: []string{
			pkgInternalProbe,
			pkgInternalCapability,
			pkgInternalRequirement,
			pkgInternalHost,
			pkgInternalRuntime,
			pkgInternalConfig,
			pkgInternalKnowledge,
			pkgInternalDiagnostics,
			pkgCmdPrefix,
		},
		AllowThirdParty: true,
		Rationale: "internal/platform is a low-level OS abstraction; depends on internal/model, standard library, " +
			"and golang.org/x/sys/unix for kernel-confined filesystem operations",
	},
	{
		SourcePrefix: pkgInternalProbe,
		AllowedInternal: []string{
			pkgInternalModel,
			pkgInternalPlatform,
		},
		DisallowedPrefixes: []string{
			pkgInternalCapability,
			pkgInternalRequirement,
			pkgInternalHost,
			pkgInternalRuntime,
			pkgInternalConfig,
			pkgInternalKnowledge,
			pkgInternalDiagnostics,
			pkgCmdPrefix,
		},
		Rationale: "internal/probe orchestrates probes using platform abstractions; must not depend on engine or CLI packages",
	},
	{
		SourcePrefix: pkgInternalRuntime,
		DisallowedPrefixes: []string{
			pkgCmdPrefix,
			pkgInternalDiagnostics,
		},
		Rationale: "runtime adapters must not depend on CLI or diagnostics packages",
	},
	{
		SourcePrefix: "internal/output",
		AllowedInternal: []string{
			pkgInternalModel,
			"schema/v1",
		},
		DisallowedPrefixes: []string{
			pkgInternalProbe,
			pkgInternalPlatform,
			pkgInternalHost,
			pkgInternalRuntime,
			pkgInternalCapability,
			pkgInternalConfig,
			pkgInternalKnowledge,
			pkgInternalDiagnostics,
			pkgCmdPrefix,
		},
		Rationale: "internal/output serializes Schema v1 reports and may only directly import internal/model, schema/v1, and standard library",
	},
}

// hasPackagePrefix checks if pkg matches prefix or is a subpackage under prefix.
// It ensures matches occur on path component boundaries, preventing false-positive
// matches such as "internal/modelicious" matching "internal/model".
func hasPackagePrefix(pkg, prefix string) bool {
	cleanPrefix := strings.TrimSuffix(prefix, "/")
	cleanPkg := strings.TrimSuffix(pkg, "/")
	return cleanPkg == cleanPrefix || strings.HasPrefix(cleanPkg, cleanPrefix+"/")
}

// PackageImports maps package relative paths (e.g. "internal/model") to their imported packages.
type PackageImports map[string][]string

// CheckArchitecture inspects a PackageImports map against ArchitectureRules and returns all violations.
func CheckArchitecture(pkgs PackageImports, rules []Rule) []string {
	var violations []string

	for pkgPath, imports := range pkgs {
		for _, rule := range rules {
			if !hasPackagePrefix(pkgPath, rule.SourcePrefix) {
				continue
			}

			for _, imp := range imports {
				// Normalize module import path to relative repo path if within capagent module
				relImp := strings.TrimPrefix(imp, modulePrefix)
				isInternal := strings.HasPrefix(imp, modulePrefix) || hasPackagePrefix(imp, "internal")

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

				// Check AllowedInternal and third-party restrictions
				if len(rule.AllowedInternal) > 0 {
					if isInternal {
						allowed := false
						for _, okPkg := range rule.AllowedInternal {
							if hasPackagePrefix(relImp, okPkg) {
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
					} else if !rule.AllowThirdParty && strings.Contains(imp, ".") {
						violations = append(violations, fmt.Sprintf(
							"rule violation: %s imports third-party package %s but is only permitted to import %v and standard library (%s)",
							pkgPath, imp, rule.AllowedInternal, rule.Rationale,
						))
					}
				}

				// Check DisallowedPrefixes
				for _, disallowed := range rule.DisallowedPrefixes {
					if hasPackagePrefix(relImp, disallowed) {
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
		if !hasPackagePrefix(relPath, "internal") && !hasPackagePrefix(relPath, "cmd") {
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
					stretchrAssert,
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
				pkgInternalRequirement: {
					"github.com/EpicBlackWolfZ/capagent/internal/probe",
				},
			},
			wantViolation: true,
		},
		{
			name: "internal/requirement importing internal/model is permitted",
			imports: PackageImports{
				pkgInternalRequirement: {
					"github.com/EpicBlackWolfZ/capagent/" + pkgInternalModel,
				},
			},
			wantViolation: false,
		},
		{
			name: "internal/requirement importing third-party package is rejected",
			imports: PackageImports{
				pkgInternalRequirement: {
					stretchrAssert,
				},
			},
			wantViolation: true,
		},
		{
			name: "internal/capability importing internal/host is rejected",
			imports: PackageImports{
				pkgInternalCapability: {
					"github.com/EpicBlackWolfZ/capagent/internal/host",
				},
			},
			wantViolation: true,
		},
		{
			name: "internal/capability importing internal/runtime is rejected",
			imports: PackageImports{
				pkgInternalCapability: {
					"github.com/EpicBlackWolfZ/capagent/internal/runtime",
				},
			},
			wantViolation: true,
		},
		{
			name: "internal/capability importing internal/platform is rejected",
			imports: PackageImports{
				pkgInternalCapability: {
					"github.com/EpicBlackWolfZ/capagent/internal/platform",
				},
			},
			wantViolation: true,
		},
		{
			name: "internal/capability importing internal/knowledge is rejected",
			imports: PackageImports{
				pkgInternalCapability: {
					"github.com/EpicBlackWolfZ/capagent/internal/knowledge",
				},
			},
			wantViolation: true,
		},
		{
			name: "internal/capability importing internal/config is rejected",
			imports: PackageImports{
				pkgInternalCapability: {
					"github.com/EpicBlackWolfZ/capagent/internal/config",
				},
			},
			wantViolation: true,
		},
		{
			name: "internal/capability importing internal/diagnostics is rejected",
			imports: PackageImports{
				pkgInternalCapability: {
					"github.com/EpicBlackWolfZ/capagent/internal/diagnostics",
				},
			},
			wantViolation: true,
		},
		{
			name: "internal/capability importing internal/model is permitted",
			imports: PackageImports{
				pkgInternalCapability: {
					"github.com/EpicBlackWolfZ/capagent/" + pkgInternalModel,
					"github.com/EpicBlackWolfZ/capagent/internal/requirement",
				},
			},
			wantViolation: false,
		},
		{
			name: "internal/capability importing third-party package is rejected",
			imports: PackageImports{
				pkgInternalCapability: {
					stretchrAssert,
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
		{
			name: "internal/requirement importing internal/modelicious is rejected",
			imports: PackageImports{
				pkgInternalRequirement: {
					"github.com/EpicBlackWolfZ/capagent/internal/modelicious",
				},
			},
			wantViolation: true,
		},
		{
			name: "internal/requirement importing internal/model subpackage is permitted",
			imports: PackageImports{
				pkgInternalRequirement: {
					"github.com/EpicBlackWolfZ/capagent/internal/model/subpkg",
				},
			},
			wantViolation: false,
		},
		{
			name: "internal/requirement_backup is not matched by internal/requirement rule",
			imports: PackageImports{
				"internal/requirement_backup": {
					"github.com/EpicBlackWolfZ/capagent/internal/probe",
				},
			},
			wantViolation: false,
		},
		{
			name: "internal/runtime importing cmdline is not rejected by cmd rule",
			imports: PackageImports{
				"internal/runtime/podman": {
					"github.com/EpicBlackWolfZ/capagent/cmdline",
				},
			},
			wantViolation: false,
		},
		{
			name: "internal/output importing internal/probe is rejected",
			imports: PackageImports{
				"internal/output": {
					"github.com/EpicBlackWolfZ/capagent/internal/probe",
				},
			},
			wantViolation: true,
		},
		{
			name: "internal/output importing internal/model and schema/v1 is permitted",
			imports: PackageImports{
				"internal/output": {
					"github.com/EpicBlackWolfZ/capagent/" + pkgInternalModel,
					"github.com/EpicBlackWolfZ/capagent/schema/v1",
				},
			},
			wantViolation: false,
		},
		{
			name: "internal/output importing third-party package is rejected",
			imports: PackageImports{
				"internal/output": {
					stretchrAssert,
				},
			},
			wantViolation: true,
		},
		{
			name: "internal/platform importing internal/probe is rejected",
			imports: PackageImports{
				pkgInternalPlatform: {
					"github.com/EpicBlackWolfZ/capagent/internal/probe",
				},
			},
			wantViolation: true,
		},
		{
			name: "internal/platform importing internal/capability is rejected",
			imports: PackageImports{
				pkgInternalPlatform: {
					"github.com/EpicBlackWolfZ/capagent/internal/capability",
				},
			},
			wantViolation: true,
		},
		{
			name: "internal/platform importing internal/runtime is rejected",
			imports: PackageImports{
				"internal/platform/runtime": {
					"github.com/EpicBlackWolfZ/capagent/internal/platform",
				},
			},
			wantViolation: true,
		},
		{
			name: "internal/platform importing golang.org/x/sys/unix is permitted (kernel-confined filesystem boundary)",
			imports: PackageImports{
				pkgInternalPlatform: {
					"golang.org/x/sys/unix",
				},
			},
			wantViolation: false,
		},
		{
			name: "internal/platform importing arbitrary third-party package is permitted (scoped to kernel interface)",
			imports: PackageImports{
				pkgInternalPlatform: {
					stretchrAssert,
				},
			},
			wantViolation: false,
		},
		{
			name: "internal/platform importing internal/model is permitted",
			imports: PackageImports{
				pkgInternalPlatform: {
					"github.com/EpicBlackWolfZ/capagent/" + pkgInternalModel,
				},
			},
			wantViolation: false,
		},
		{
			name: "internal/platform importing standard library is permitted",
			imports: PackageImports{
				pkgInternalPlatform: {
					"os",
					"context",
					"syscall",
					"sync",
				},
			},
			wantViolation: false,
		},
		{
			name: "internal/probe importing internal/capability is rejected",
			imports: PackageImports{
				pkgInternalProbe: {
					"github.com/EpicBlackWolfZ/capagent/internal/capability",
				},
			},
			wantViolation: true,
		},
		{
			name: "internal/probe importing internal/runtime is rejected",
			imports: PackageImports{
				pkgInternalProbe: {
					"github.com/EpicBlackWolfZ/capagent/internal/runtime",
				},
			},
			wantViolation: true,
		},
		{
			name: "internal/probe importing cmd/capagent is rejected",
			imports: PackageImports{
				pkgInternalProbe: {
					"github.com/EpicBlackWolfZ/capagent/cmd/capagent",
				},
			},
			wantViolation: true,
		},
		{
			name: "internal/probe importing third-party package is rejected",
			imports: PackageImports{
				pkgInternalProbe: {
					stretchrAssert,
				},
			},
			wantViolation: true,
		},
		{
			name: "internal/probe importing internal/model and internal/platform is permitted",
			imports: PackageImports{
				pkgInternalProbe: {
					"github.com/EpicBlackWolfZ/capagent/" + pkgInternalModel,
					"github.com/EpicBlackWolfZ/capagent/internal/platform",
				},
			},
			wantViolation: false,
		},
		{
			name: "internal/probe importing standard library is permitted",
			imports: PackageImports{
				pkgInternalProbe: {
					"context",
					"sync",
					"time",
				},
			},
			wantViolation: false,
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

func TestHasPackagePrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pkg    string
		prefix string
		want   bool
	}{
		{pkg: pkgInternalModel, prefix: pkgInternalModel, want: true},
		{pkg: pkgInternalModel + "/sub", prefix: pkgInternalModel, want: true},
		{pkg: pkgInternalModel + "/", prefix: pkgInternalModel, want: true},
		{pkg: pkgInternalModel, prefix: pkgInternalModel + "/", want: true},
		{pkg: "internal/modelicious", prefix: pkgInternalModel, want: false},
		{pkg: "internal/requirement_backup", prefix: pkgInternalRequirement, want: false},
		{pkg: "cmd/capagent", prefix: pkgCmdPrefix, want: true},
		{pkg: "cmd", prefix: pkgCmdPrefix, want: true},
		{pkg: "cmdline", prefix: pkgCmdPrefix, want: false},
		{pkg: pkgInternalProbe, prefix: pkgInternalModel, want: false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_matches_%s", tt.pkg, tt.prefix), func(t *testing.T) {
			t.Parallel()
			got := hasPackagePrefix(tt.pkg, tt.prefix)
			if got != tt.want {
				t.Errorf("hasPackagePrefix(%q, %q) = %v, want %v", tt.pkg, tt.prefix, got, tt.want)
			}
		})
	}
}

// TestArchitecture_ForbidLegacyEncodingJSON mechanically enforces that no Go file across the entire repository
// imports legacy "encoding/json". "encoding/json/v2" is strictly required for all JSON processing.
func TestArchitecture_ForbidLegacyEncodingJSON(t *testing.T) {
	t.Parallel()

	rootDir := findRepoRoot(t)
	fset := token.NewFileSet()
	var violations []string

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			base := info.Name()
			if strings.HasPrefix(base, ".") || base == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		node, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", path, err)
		}

		relPath, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}

		for _, imp := range node.Imports {
			impPath := strings.Trim(imp.Path.Value, `"`)
			if impPath == "encoding/json" {
				violations = append(violations, fmt.Sprintf(
					"%s imports legacy 'encoding/json'; encoding/json/v2 must always be used instead",
					relPath,
				))
			}
		}

		return nil
	})

	if err != nil {
		t.Fatalf("failed to scan repository: %v", err)
	}

	if len(violations) > 0 {
		t.Errorf("Found forbidden legacy encoding/json imports:\n%s", strings.Join(violations, "\n"))
	}
}
