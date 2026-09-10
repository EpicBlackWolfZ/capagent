package contract_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestArchitecture_ExactPlatformDependencies(t *testing.T) {
	t.Parallel()
	for _, imp := range []string{stretchrAssert, "golang.org/x/sys/windows", "golang.org/x/sys/unix/extra"} {
		t.Run(imp, func(t *testing.T) {
			t.Parallel()
			if len(CheckArchitecture(PackageImports{pkgInternalPlatform: {imp}}, ArchitectureRules)) == 0 {
				t.Fatalf("platform accepted unauthorized dependency %s", imp)
			}
		})
	}
	if got := CheckArchitecture(PackageImports{pkgInternalPlatform: {pkgUnix}}, ArchitectureRules); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestArchitecture_HostScannerCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, path, source string
		denied             bool
	}{
		{"syscall file", hostFixturePath, `package p; import "syscall"; var read = syscall.Open`, true},
		{"raw syscall", hostFixturePath, `package p; import sc "syscall"; var raw = sc.RawSyscall6`, true},
		{"low-level stat", hostFixturePath, `package p; import "syscall"; var stat = syscall.Statfs`, true},
		{"socket", "internal/runtime/read.go", `package p; import "syscall"; var connect = syscall.Connect`, true},
		{"credentials", hostFixturePath, `package p; import "syscall"; var uid = syscall.Getuid`, true},
		{"resource", hostFixturePath, `package p; import "syscall"; var limit = syscall.Setrlimit`, true},
		{"process", appFixturePath, `package p; import "syscall"; var run = syscall.ForkExec`, true},
		{"unix alias", hostFixturePath, `package p; import u "golang.org/x/sys/unix"; var open = u.Openat2`, true},
		{"fd wrapper", appFixturePath, `package p; import "os"; var wrap = os.NewFile`, true},
		{"secure root wrapper", appFixturePath, `package p; import "os"; var root = os.OpenRoot`, true},
		{"cwd", appFixturePath, `package p; import "os"; var cwd = os.Getwd`, true},
		{"path IO", hostFixturePath, `package p; import "path/filepath"; var eval = filepath.EvalSymlinks`, true},
		{"ambient path", hostFixturePath, `package p; import "path/filepath"; var abs = filepath.Abs`, true},
		{"old file IO", hostFixturePath, `package p; import "io/ioutil"; var read = ioutil.ReadFile`, true},
		{"ambient syscall", appFixturePath, `package p; import "syscall"; var env = syscall.Getenv`, true},
		{"environment mutation", "internal/app/read_test.go", `package p; import "os"; var env = os.Setenv`, true},
		{"test file IO", "internal/host/read_test.go", `package p; import "syscall"; var read = syscall.Read`, true},
		{"test exec", "cmd/capagent/read_test.go", `package p; import _ "os/exec"`, true},
		{"dot import", hostFixturePath, `package p; import . "syscall"; var read = Open`, true},
		{"os dot import", hostFixturePath, `package p; import . "os"; var read = ReadFile`, true},
		{"ordinary alias", hostFixturePath, `package p; import host "os"; var read = host.ReadFile`, true},
		{"platform lookalike", "internal/platformish/read.go", `package p; import "os"; var read = os.ReadFile`, true},
		{"platform", "internal/platform/read.go", `package p; import "golang.org/x/sys/unix"; var read = unix.Openat2`, false},
		{"contract harness", "tests/contract/read_test.go", `package p; import "os/exec"; var run = exec.Command`, false},
		{"errno", hostFixturePath, `package p; import "syscall"; var errno = syscall.EACCES`, false},
		{"unix constant", hostFixturePath, `package p; import "golang.org/x/sys/unix"; var errno = unix.ELOOP`, false},
		{"metadata type", hostFixturePath, `package p; import "syscall"; var st syscall.Stat_t`, false},
		{"errno conversion", hostFixturePath, `package p; import "syscall"; var errno = syscall.Errno(2)`, false},
		{"pure path", hostFixturePath, `package p; import "path/filepath"; var join = filepath.Join`, false},
		{"CLI primitives", "cmd/capagent/main.go", `package p; import "os"; var a = os.Args; var o = os.Stdout; var e = os.Exit`, false},
		{"test ambient", "internal/app/read_test.go", `package p; import "os"; var env = os.LookupEnv`, false},
		{"test syscall ambient", "internal/app/read_test.go", `package p; import "syscall"; var env = syscall.Environ`, false},
		{"shadowed variable", hostFixturePath, `package p; import "os"; func f(){ os := struct{ ReadFile int }{}; _ = os.ReadFile }`, false},
		{"shadowed parameter", hostFixturePath, `package p; import s "syscall"; func f(s struct{ Open int }){ _ = s.Open }`, false},
		{"application abstraction", appFixturePath,
			`package p; import "github.com/EpicBlackWolfZ/capagent/internal/platform"; var env = platform.NewEnvPolicy`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := scanHostIOFile(tt.path, []byte(tt.source))
			if err != nil {
				t.Fatal(err)
			}
			if (len(got) != 0) != tt.denied {
				t.Fatalf("violations = %v, denied = %v", got, tt.denied)
			}
		})
	}
	if _, err := scanHostIOFile("internal/host/broken.go", []byte("not go source")); err == nil {
		t.Fatal("ignored parse failure")
	}
}

func TestArchitecture_DirectoryUsesFileScanner(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logical := "internal/app/fixture.go"
	source := []byte(`package p; import sc "syscall"; var raw = sc.RawSyscall`)
	path := filepath.Join(root, filepath.FromSlash(logical))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := scanHostIOFile(logical, source)
	if err != nil {
		t.Fatal(err)
	}
	got, err := scanHostIODirectory(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || len(got) == 0 {
		t.Fatalf("directory = %v, file = %v", got, want)
	}
	fixtureDir := filepath.Join(root, "tests", "contract", "fixtures")
	if err := os.MkdirAll(fixtureDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtureDir, "fixture.go"), source, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = scanDirForHostIOViolations(fixtureDir)
	if err != nil || len(got) == 0 || !strings.Contains(got[0], "syscall.RawSyscall") {
		t.Fatalf("fixture = %v, %v", got, err)
	}
	if _, err := scanHostIODirectory(filepath.Join(root, "missing"), false); err == nil {
		t.Fatal("ignored walk failure")
	}
}

func TestArchitecture_ApplicationComposition(t *testing.T) {
	t.Parallel()
	tests := []struct {
		pkg, imp string
		denied   bool
	}{
		{pkgInternalApp, pkgInternalPlatform, false},
		{pkgInternalApp, pkgInternalProbe, false},
		{pkgInternalApp, pkgInternalCapability, false},
		{pkgInternalApp, pkgCmdCapagent, true},
		{pkgCmdCapagent, pkgInternalApp, false},
		{pkgCmdCapagent, "internal/version", false},
		{pkgCmdCapagent, pkgInternalProbe, true},
		{pkgCmdCapagent, pkgInternalPlatform, true},
		{pkgInternalRuntime + "/podman", pkgInternalApp, true},
		{pkgInternalHost, pkgInternalApp, true},
		{pkgInternalPlatform, pkgInternalApp, true},
		{pkgInternalApp, "internal/appsupport", true},
	}
	for _, tt := range tests {
		t.Run(tt.pkg+"->"+tt.imp, func(t *testing.T) {
			t.Parallel()
			got := CheckArchitecture(PackageImports{tt.pkg: {modulePrefix + tt.imp}}, ArchitectureRules)
			if (len(got) != 0) != tt.denied {
				t.Fatalf("violations = %v; denied = %v", got, tt.denied)
			}
		})
	}
}
