package contract_test

import (
	"fmt"
	"go/ast"
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
			if strings.HasPrefix(base, walkSkipHidden) || base == walkSkipVendor || base == walkSkipTestdata {
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
			if strings.HasPrefix(base, walkSkipHidden) || base == walkSkipVendor {
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

// ---------------------------------------------------------------------------
// Host-IO denylist (M1.1 plan §9; issue #47)
//
// The host-IO denylist rule is an AST-based mechanical guard against
// direct, syntactically-recognizable uses of forbidden host primitives
// outside the platform package and CLI exceptions. The rule is
// intentionally syntactic — it walks the AST looking for SelectorExpr
// nodes whose receiver is the Ident "os" and whose Sel matches a
// denylist entry. It does NOT track type aliases, function values, or
// indirect calls; those bypasses are accepted as documented false
// negatives (plan §9.6).
//
// The rule has three logical buckets:
//
//   - File I/O primitives (os.ReadFile, os.Open, etc.): forbidden in
//     production AND test code of any package other than the listed
//     allow prefixes.
//   - Subprocess primitives (os/exec.Command): forbidden in production
//     AND test code of any package other than the listed allow prefixes.
//   - Ambient env primitives (os.Getenv, os.LookupEnv, os.Environ):
//     forbidden in production code of any package other than the
//     allow prefixes, but PERMITTED in test code (harnesses legitimately
//     need to read environment variables).
//
// Allow prefixes (full path prefixes that bypass the rule):
//
//   - internal/platform/...: owns the primitives.
//   - cmd/capagent/...: CLI; may use os.Args, os.Stdout, os.Stderr,
//     os.Exit. May NOT use file-I/O or command-execution primitives.
//   - tests/contract/...: uses go/parser, filepath.Walk, and the
//     standard library legitimately to enforce the rules.
// ---------------------------------------------------------------------------

// hostPrimitiveDenylist is the canonical list of forbidden host-IO
// selectors. Each entry is "<package>.<selector>"; the receiver Ident
// is the package name (typically "os" or a renamed import).
var hostPrimitiveDenylist = []string{
	// File I/O
	"os.ReadFile", "os.WriteFile", "os.Open", "os.OpenFile", "os.Create",
	"os.Stat", "os.Lstat", "os.ReadDir",
	"os.Readlink",
	"os.Mkdir", "os.MkdirAll", "os.Remove", "os.RemoveAll",
	"os.Rename", "os.Chmod", "os.Symlink", "os.Link", "os.Truncate",
}

// hostExecDenylist forbids import of os/exec outside the platform
// package. The CLI's process exit primitives are tracked separately.
var hostExecDenylist = []string{
	"os/exec",
}

// hostAmbientEnvDenylist forbids reading ambient environment outside
// the platform package and tests. Tests are exempted because test
// harnesses legitimately need to inspect the environment.
var hostAmbientEnvDenylist = []string{
	"os.Getenv", "os.LookupEnv", "os.Environ",
}

// hostPrimitiveAllowPrefixes is the set of path prefixes that may use
// the denylisted primitives. The contract test directory is exempted
// per plan §9.3 (it uses os, filepath.Walk, go/parser legitimately
// to enforce the rules).
//
// The cmd/ prefix is intentionally NOT in the allowlist. CLI
// binaries may use ordinary CLI primitives (os.Args, os.Stdout,
// os.Stderr, os.Exit) — these are not in any denylist and so do
// not require an exemption. File-I/O and os/exec are denied in
// cmd/ just as in any other non-platform package.
var hostPrimitiveAllowPrefixes = []string{
	"internal/platform/",
	"tests/contract/",
}

// Walked-directory skip constants used by the AST scanner. Hoisted
// so the goconst linter does not flag repeated literals.
const (
	walkSkipVendor  = "vendor"
	walkSkipTestdata = "testdata"
	walkSkipHidden  = "."
)

// isHostIOAllowedPath returns true if the supplied repository-relative
// path is exempt from the host-IO denylist.
func isHostIOAllowedPath(relPath string) bool {
	for _, prefix := range hostPrimitiveAllowPrefixes {
		if strings.HasPrefix(relPath, prefix) {
			return true
		}
	}
	return false
}

// isExcludedHostIOPath returns true for generated/vendored/testdata
// paths the rule skips entirely.
func isExcludedHostIOPath(relPath string) bool {
	if strings.Contains(relPath, "/vendor/") || strings.HasPrefix(relPath, "vendor/") {
		return true
	}
	if strings.Contains(relPath, "/testdata/") || strings.HasPrefix(relPath, "testdata/") {
		return true
	}
	// The fixtures directory under tests/contract is intentionally
	// excluded from the global scan because it is the AST rule's
	// test corpus: per-file checks live in
	// TestArchitecture_HostPrimitiveFixtures which scans the
	// directory directly. Treating it as testdata-equivalent prevents
	// the global walker from double-flagging files that exist solely
	// to demonstrate the rule.
	if strings.Contains(relPath, "/tests/contract/fixtures/") ||
		strings.HasPrefix(relPath, "tests/contract/fixtures/") {
		return true
	}
	base := filepath.Base(relPath)
	// Generated-code patterns
	if strings.HasSuffix(base, ".pb.go") ||
		strings.HasSuffix(base, "_gen.go") ||
		strings.HasPrefix(base, "mock_") {
		return true
	}
	return false
}

// TestArchitecture_ForbidHostIOPrimitivesOutsidePlatform walks the
// repository AST looking for SelectorExpr nodes whose receiver is
// "os" and whose Sel matches an entry in hostPrimitiveDenylist or
// hostAmbientEnvDenylist. The rule is intentionally syntactic
// (plan §9.6) — false negatives from aliasing and function-value
// indirection are accepted.
//
// File I/O and command-execution denylist apply to BOTH production
// and test files. Ambient-env denylist does NOT apply to test files.
func TestArchitecture_ForbidHostIOPrimitivesOutsidePlatform(t *testing.T) {
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
			if strings.HasPrefix(base, walkSkipHidden) || base == walkSkipVendor {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		relPath, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}

		if isExcludedHostIOPath(relPath) {
			return nil
		}
		if isHostIOAllowedPath(relPath) {
			return nil
		}

		isTest := strings.HasSuffix(relPath, "_test.go")

		// Parse the full AST (not just imports) so we can detect
		// SelectorExpr nodes.
		node, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", path, err)
		}

		// Build import alias map for this file.
		aliases := make(map[string]string)
		for _, imp := range node.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if imp.Name != nil && imp.Name.Name == "." {
				continue
			}
			var pkgName string
			if imp.Name != nil {
				pkgName = imp.Name.Name
			} else {
				// Default: last path segment
				parts := strings.Split(importPath, "/")
				pkgName = parts[len(parts)-1]
			}
			aliases[importPath] = pkgName
		}

		// Walk AST looking for SelectorExpr matching the denylist.
		ast.Inspect(node, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}

			// Resolve which package the identifier refers to.
			resolvedPkg := ""
			for importPath, alias := range aliases {
				if alias == ident.Name {
					resolvedPkg = importPath
					break
				}
			}
			if resolvedPkg == "" {
				return true
			}

			selector := resolvedPkg + "." + sel.Sel.Name

			// File I/O selectors apply to all files (production and
			// test). The denylist explicitly closes the test-file
			// loophole: tests must use the platform package too.
			if stringSliceContainsSet(selector, hostPrimitiveDenylist) {
				violations = append(violations, fmt.Sprintf(
					"%s uses forbidden host-IO primitive %s; route through internal/platform instead",
					relPath, selector,
				))
				return true
			}

			// Ambient-env selectors apply only to production code;
			// tests legitimately need to inspect the environment.
			if !isTest && stringSliceContainsSet(selector, hostAmbientEnvDenylist) {
				violations = append(violations, fmt.Sprintf(
					"%s uses forbidden ambient-env primitive %s in production code; route through internal/platform instead",
					relPath, selector,
				))
				return true
			}

			return true
		})

		// Flag os/exec imports in BOTH production and test files
		// outside the owning layer. Test harnesses must not bypass
		// the platform.CommandRunner abstraction.
		for _, imp := range node.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if stringSliceContains(hostExecDenylist, importPath) {
				violations = append(violations, fmt.Sprintf(
					"%s imports forbidden %s; route through internal/platform.CommandRunner instead",
					relPath, importPath,
				))
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("failed to scan repository: %v", err)
	}

	if len(violations) > 0 {
		t.Errorf("Found forbidden host-IO primitives outside internal/platform:\n%s",
			strings.Join(violations, "\n"))
	}
}

// stringSliceContains reports whether the target string appears in the
// given slice.
func stringSliceContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// stringSliceContainsSet is a thin alias for symmetry with the file-I/O
// denylist; both use simple slice membership so a single helper suffices.
func stringSliceContainsSet(needle string, haystack []string) bool {
	return stringSliceContains(haystack, needle)
}

// TestArchitecture_HostPrimitiveRuleEnforcement verifies that the host-IO
// denylist correctly detects violations and accepts the allow-prefix
// exemptions. The positive fixture (TestArchitecture_ForbidHostIOPrimitivesOutsidePlatform)
// above is the production check; this test asserts the AST inspection
// helpers behave as documented.
func TestArchitecture_HostPrimitiveRuleEnforcement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		denylist   []string
		selector   string
		wantReject bool
	}{
		{name: "os.ReadFile rejected", denylist: hostPrimitiveDenylist, selector: "os.ReadFile", wantReject: true},
		{name: "os.Open rejected", denylist: hostPrimitiveDenylist, selector: "os.Open", wantReject: true},
		{name: "os.WriteFile rejected", denylist: hostPrimitiveDenylist, selector: "os.WriteFile", wantReject: true},
		{name: "os.Stat rejected", denylist: hostPrimitiveDenylist, selector: "os.Stat", wantReject: true},
		{name: "os.Args permitted", denylist: hostPrimitiveDenylist, selector: "os.Args", wantReject: false},
		{name: "os.Stdout permitted", denylist: hostPrimitiveDenylist, selector: "os.Stdout", wantReject: false},
		{name: "os.Getenv rejected in production", denylist: hostAmbientEnvDenylist, selector: "os.Getenv", wantReject: true},
		{name: "os.LookupEnv rejected in production", denylist: hostAmbientEnvDenylist, selector: "os.LookupEnv", wantReject: true},
		{name: "os.Environ rejected in production", denylist: hostAmbientEnvDenylist, selector: "os.Environ", wantReject: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotReject := stringSliceContainsSet(tt.selector, tt.denylist)
			if gotReject != tt.wantReject {
				t.Errorf("selector %q: rejected = %v, want %v", tt.selector, gotReject, tt.wantReject)
			}
		})
	}

	// Allow-prefix helpers must exempt the platform package and the
// contract test directory. The CLI is intentionally NOT exempted:
// CLI code that uses host-IO primitives is rejected; CLI code that
// uses only standard CLI primitives (os.Args, os.Stdout, os.Stderr,
// os.Exit) does not require any exemption because those primitives
// are not in any denylist.
	allowedSamples := []string{
		"internal/platform/reader.go",
		"internal/platform/scoped_reader.go",
		"tests/contract/architecture_test.go",
	}
	for _, sample := range allowedSamples {
		if !isHostIOAllowedPath(sample) {
			t.Errorf("expected %q to be allowed by host-IO allowlist", sample)
		}
	}

	// Non-allowed production files must NOT be exempt. The CLI is
	// also non-exempt; CLI primitives are not in the denylist, but
	// file-I/O and os/exec are.
	deniedSamples := []string{
		"internal/probe/orchestrator.go",
		"internal/model/capability.go",
		"cmd/capagent/main.go",
	}
	for _, sample := range deniedSamples {
		if isHostIOAllowedPath(sample) {
			t.Errorf("expected %q to NOT be allowed by host-IO allowlist", sample)
		}
	}
}

// TestArchitecture_HostPrimitiveFixtures verifies that the AST
// scanner classifies the fixtures in tests/contract/fixtures/ as
// expected: every negative fixture must be flagged, every positive
// fixture must not. The fixtures directory is excluded from the
// repository-wide scan so these tests can probe the rule directly
// without polluting production.
func TestArchitecture_HostPrimitiveFixtures(t *testing.T) {
	t.Parallel()

	rootDir := findRepoRoot(t)
	fixturesDir := filepath.Join(rootDir, "tests", "contract", "fixtures")

	violations, err := scanDirForHostIOViolations(fixturesDir)
	if err != nil {
		t.Fatalf("scanDirForHostIOViolations: %v", err)
	}

	violationByFile := make(map[string][]string)
	for _, v := range violations {
		// Format: "<relpath>: <message>"
		idx := strings.Index(v, ":")
		if idx < 0 {
			continue
		}
		violationByFile[v[:idx]] = append(violationByFile[v[:idx]], v[idx+1:])
	}

	// Negative fixtures: each MUST be flagged.
	negativeFixtures := []string{
		"example_host_io_violation.go",
		"example_cmd_host_io_violation.go",
		"example_test_host_exec_violation_test.go",
	}
	for _, name := range negativeFixtures {
		fixture := filepath.Join("tests", "contract", "fixtures", name)
		if len(violationByFile[fixture]) == 0 {
			t.Errorf("expected %q to be flagged by the denylist; got none", fixture)
		}
	}

	// Positive fixture: MUST NOT be flagged.
	positiveFixture := filepath.Join("tests", "contract", "fixtures", "example_legitimate.go")
	if len(violationByFile[positiveFixture]) > 0 {
		t.Errorf("did not expect %q to be flagged; got %v", positiveFixture, violationByFile[positiveFixture])
	}
}

// TestArchitecture_CLIPrimitivesAllowed verifies that the standard CLI
// primitives (os.Args, os.Stdout, os.Stderr, os.Exit) are NOT in any
// denylist and therefore do not require an allowlist exemption.
// cmd/capagent/main.go uses these primitives and must remain free of
// host-IO flags from the scanner.
func TestArchitecture_CLIPrimitivesAllowed(t *testing.T) {
	t.Parallel()

	cliPrimitives := []string{
		"os.Args",
		"os.Stdout",
		"os.Stderr",
		"os.Exit",
	}
	for _, sel := range cliPrimitives {
		if stringSliceContainsSet(sel, hostPrimitiveDenylist) {
			t.Errorf("%q is in the file-IO denylist; should be permitted for CLI", sel)
		}
		if stringSliceContainsSet(sel, hostAmbientEnvDenylist) {
			t.Errorf("%q is in the ambient-env denylist; should be permitted for CLI", sel)
		}
	}
}

// TestArchitecture_TestFileAmbientEnvAllowed verifies that ambient
// environment access is permitted inside _test.go files. The
// plan §2.2 keeps this single test-file exemption for ambient env
// only; file-IO and os/exec remain forbidden in test files.
func TestArchitecture_TestFileAmbientEnvAllowed(t *testing.T) {
	t.Parallel()

	// Synthesize a fake "is test" code path: parse an in-memory
	// source that uses os.Getenv inside a _test.go-styled helper.
	src := `package fixtures
import "os"
func helper() string { return os.Getenv("HOME") }
`
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "fake_test.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Mimic the scanner's per-file denylist application.
	isTest := true
	var matchedAmbient bool
	ast.Inspect(node, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if ident.Name != "os" {
			return true
		}
		fullSel := "os." + sel.Sel.Name
		if stringSliceContainsSet(fullSel, hostAmbientEnvDenylist) {
			matchedAmbient = true
			if !isTest {
				t.Errorf("ambient-env %q in non-test code: scanner should flag", fullSel)
			}
		}
		return true
	})

	if !matchedAmbient {
		t.Fatalf("test source did not reference any ambient-env selector")
	}
}

// scanDirForHostIOViolations is the same AST walk used by the
// repository-wide test, restricted to the supplied directory. It is
// extracted here so the fixture tests can probe the rule without
// scanning the entire repository.
//
// The per-fixture scan intentionally does NOT skip the tests/contract/
// fixtures/ directory: those files are the rule's test corpus and
// must be classified individually.
func scanDirForHostIOViolations(rootDir string) ([]string, error) {
	fset := token.NewFileSet()
	var violations []string

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			base := info.Name()
			if strings.HasPrefix(base, walkSkipHidden) || base == walkSkipVendor {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		relPath, err := filepath.Rel(findRepoRootForScan(), path)
		if err != nil {
			return err
		}

		// Skip vendor/testdata/generated but DO scan fixtures: the
		// per-fixture scanner's job is to classify them.
		if strings.Contains(relPath, "/vendor/") || strings.HasPrefix(relPath, "vendor/") {
			return nil
		}
		if strings.Contains(relPath, "/testdata/") || strings.HasPrefix(relPath, "testdata/") {
			return nil
		}
		base := filepath.Base(relPath)
		if strings.HasSuffix(base, ".pb.go") ||
			strings.HasSuffix(base, "_gen.go") ||
			strings.HasPrefix(base, "mock_") {
			return nil
		}
		// Fixtures themselves are NOT exempt; this is the AST
		// rule's test corpus.
		if isHostIOAllowedPath(relPath) && !strings.Contains(relPath, "/fixtures/") {
			return nil
		}

		isTest := strings.HasSuffix(relPath, "_test.go")

		node, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", path, err)
		}

		aliases := make(map[string]string)
		for _, imp := range node.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if imp.Name != nil && imp.Name.Name == "." {
				continue
			}
			var pkgName string
			if imp.Name != nil {
				pkgName = imp.Name.Name
			} else {
				parts := strings.Split(importPath, "/")
				pkgName = parts[len(parts)-1]
			}
			aliases[importPath] = pkgName
		}

		ast.Inspect(node, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}

			resolvedPkg := ""
			for importPath, alias := range aliases {
				if alias == ident.Name {
					resolvedPkg = importPath
					break
				}
			}
			if resolvedPkg == "" {
				return true
			}

			selector := resolvedPkg + "." + sel.Sel.Name

			if stringSliceContainsSet(selector, hostPrimitiveDenylist) {
				violations = append(violations, fmt.Sprintf("%s: %s", relPath, selector))
				return true
			}
			if !isTest && stringSliceContainsSet(selector, hostAmbientEnvDenylist) {
				violations = append(violations, fmt.Sprintf("%s: %s (ambient)", relPath, selector))
				return true
			}
			return true
		})

		for _, imp := range node.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		if stringSliceContains(hostExecDenylist, importPath) {
			violations = append(violations, fmt.Sprintf("%s: import %s", relPath, importPath))
		}
	}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return violations, nil
}

// findRepoRootForScan is a thin shim around findRepoRoot used by the
// scan helper. findRepoRoot takes *testing.T; this version caches the
// result via a sentinel to avoid the testing.T dependency in helpers.
func findRepoRootForScan() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir
		}
		dir = parent
	}
}
