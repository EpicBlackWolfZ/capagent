package contract_test

import (
	"debug/elf"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestArchitecture_NoCGOImports verifies that no Go files in the repository import "C".
func TestArchitecture_NoCGOImports(t *testing.T) {
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
			if strings.HasPrefix(base, ".") || base == "vendor" || base == "testdata" {
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

		node, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}

		for _, imp := range node.Imports {
			if imp.Path.Value == `"C"` {
				violations = append(violations, relPath)
			}
		}

		return nil
	})

	if err != nil {
		t.Fatalf("failed to scan repository files: %v", err)
	}

	if len(violations) > 0 {
		t.Errorf("Found forbidden CGO imports in Go files:\n%s", strings.Join(violations, "\n"))
	}
}

// TestBinary_StaticLinking verifies that binaries compiled with CGO_ENABLED=0 have zero
// dynamic library dependencies or PT_INTERP headers.
//
// By default, it builds a fresh, deterministic test binary in an isolated temporary directory
// to prevent testing stale local files. When CAPAGENT_BINARY_PATH is set (e.g. in CI or release verification),
// it inspects that exact specified artifact.
func TestBinary_StaticLinking(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("ELF binary static verification only applicable on Linux")
	}

	rootDir := findRepoRoot(t)
	binPath := os.Getenv("CAPAGENT_BINARY_PATH")

	if binPath != "" {
		if !filepath.IsAbs(binPath) {
			if _, err := os.Stat(binPath); err != nil {
				candidate := filepath.Join(rootDir, binPath)
				if _, err := os.Stat(candidate); err == nil {
					binPath = candidate
				}
			}
		}
		if _, err := os.Stat(binPath); err != nil {
			t.Fatalf("CAPAGENT_BINARY_PATH was specified but file not found: %s", binPath)
		}
	} else {
		// Build fresh, deterministic test binary in an isolated temp directory
		tmpDir := t.TempDir()
		binPath = filepath.Join(tmpDir, "capagent")

		cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", binPath, "./cmd/capagent")
		cmd.Dir = rootDir
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("failed to build fresh test binary with CGO_ENABLED=0: %v\nOutput: %s", err, string(out))
		}
	}

	f, err := elf.Open(binPath)
	if err != nil {
		t.Fatalf("failed to open binary %s with debug/elf: %v", binPath, err)
	}
	defer func() {
		_ = f.Close()
	}()

	// 1. Verify that no PT_INTERP (dynamic linker interpreter) header exists
	for _, prog := range f.Progs {
		if prog.Type == elf.PT_INTERP {
			t.Errorf("binary %s contains PT_INTERP program header (dynamically linked interpreter)", binPath)
		}
	}

	// 2. Verify that no imported dynamic libraries exist
	libs, err := f.ImportedLibraries()
	if err != nil {
		t.Fatalf("failed to inspect imported libraries of %s: %v", binPath, err)
	}
	if len(libs) > 0 {
		t.Errorf("binary %s has dynamic library dependencies: %v", binPath, libs)
	}

	// 3. Verify that .dynamic section does not exist or has zero entries
	if sec := f.Section(".dynamic"); sec != nil && sec.Size > 0 {
		t.Errorf("binary %s contains non-empty .dynamic section of size %d bytes", binPath, sec.Size)
	}
}
