package platform_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

func TestNewEnvironment_PreservesComponents(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	procfs := platform.NewProcfsReader(mem, "/proc")
	sysfs := platform.NewSysfsReader(mem, "/sys")
	runner := platform.NewFakeCommandRunner()

	env := platform.NewEnvironment(mem, procfs, sysfs, runner)
	if env.Reader != mem {
		t.Error("Reader not preserved")
	}
	if env.Procfs != procfs {
		t.Error("Procfs not preserved")
	}
	if env.Sysfs != sysfs {
		t.Error("Sysfs not preserved")
	}
	if env.Runner != runner {
		t.Error("Runner not preserved")
	}
}

func TestNewEnvironment_AllowsNilComponents(t *testing.T) {
	t.Parallel()

	env := platform.NewEnvironment(nil, nil, nil, nil)
	if env.Reader != nil || env.Procfs != nil || env.Sysfs != nil || env.Runner != nil {
		t.Errorf("NewEnvironment with nils non-nil: %+v", env)
	}
}

func TestNewTestEnvironment_WithMemReader(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	mem.AddFile("/proc/version", []byte("Linux 6.0.0"), 0o644)
	mem.AddFile("/sys/fs/cgroup/cgroup.controllers", []byte("cpu memory\n"), 0o644)

	runner := platform.NewFakeCommandRunner()
	env := platform.NewTestEnvironment(mem, runner)

	if env.Reader == nil {
		t.Error("Reader is nil")
	}
	if env.Procfs == nil {
		t.Error("Procfs is nil")
	}
	if env.Sysfs == nil {
		t.Error("Sysfs is nil")
	}
	if env.Runner != runner {
		t.Error("Runner not preserved")
	}

	// Verify the readers can read the seeded fixtures via the Env.
	data, err := env.Procfs.ReadProcFile("version")
	if err != nil {
		t.Fatalf("ReadProcFile: %v", err)
	}
	if string(data) != "Linux 6.0.0" {
		t.Errorf("ReadProcFile = %q, want %q", string(data), "Linux 6.0.0")
	}

	controllers, err := env.Sysfs.CgroupControllers()
	if err != nil {
		t.Fatalf("CgroupControllers: %v", err)
	}
	if len(controllers) != 2 {
		t.Errorf("CgroupControllers = %v, want 2 entries", controllers)
	}

	// Runner is the supplied one.
	fakeRunner, ok := env.Runner.(*platform.FakeCommandRunner)
	if !ok {
		t.Fatal("Runner is not *FakeCommandRunner")
	}
	fakeRunner.Register("/bin/true", nil, platform.ExecResult{Stdout: []byte("ok"), ExitCode: 0})
	result, err := env.Runner.Run(context.Background(), "/bin/true")
	if err != nil {
		t.Errorf("Runner.Run unmocked: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("Runner.Run exit = %d", result.ExitCode)
	}
}

func TestNewTestEnvironment_NilReaderCreatesFresh(t *testing.T) {
	t.Parallel()

	runner := platform.NewFakeCommandRunner()
	env := platform.NewTestEnvironment(nil, runner)

	if env.Reader == nil {
		t.Fatal("Reader is nil")
	}
	if env.Procfs == nil || env.Sysfs == nil {
		t.Fatal("derived Procfs/Sysfs are nil")
	}
	if env.Runner != runner {
		t.Error("Runner not preserved")
	}

	// The fresh reader should be empty and report missing files.
	if _, err := env.Procfs.ReadProcFile("version"); err == nil {
		t.Error("expected error on empty reader")
	}
}

// TestMemFileInfo_Methods verifies that memFileInfo satisfies the os.FileInfo
// interface contract. Each method is exercised to keep coverage near 100%.
func TestMemFileInfo_Methods(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	mem.AddFile("/file", []byte("hello"), 0o600)
	mem.AddDir("/dir", 0o755)

	info, err := mem.Stat("/file")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if info.Name() != "file" {
		t.Errorf("Name = %q, want file", info.Name())
	}
	if info.Size() != 5 {
		t.Errorf("Size = %d, want 5", info.Size())
	}
	if info.Mode()&0o600 == 0 {
		t.Errorf("Mode = %v, missing perm bits", info.Mode())
	}
	if info.IsDir() {
		t.Error("IsDir = true, want false")
	}
	if info.Sys() != nil {
		t.Error("Sys returned non-nil")
	}
	if info.ModTime().IsZero() == false {
		// ModTime returns time.Time{} (zero), which IsZero() should report as true.
		t.Errorf("ModTime not zero: %v", info.ModTime())
	}

	// Directory entries.
	dirInfo, err := mem.Stat("/dir")
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if !dirInfo.IsDir() {
		t.Error("dir IsDir = false, want true")
	}
}

// TestMemDirEntry_Accessors verifies that the DirEntry accessors surface
// the documented kinds for regular files, directories, and symlinks, and
// that Info() returns an error because MemPlatformReader does not
// implement the FileInfo construction path.
func TestMemDirEntry_Accessors(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	mem.AddDir("/d", 0o755)
	mem.AddFile("/d/file", []byte("x"), 0o644)
	mem.AddDir("/d/sub", 0o755)
	mem.AddSymlink("/d/link", "file")

	entries, err := mem.ReadDir("/d")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	wantKind := map[string]struct {
		isDir bool
		mode  os.FileMode
	}{
		"file": {isDir: false, mode: 0},
		"sub":  {isDir: true, mode: os.ModeDir},
		"link": {isDir: false, mode: os.ModeSymlink},
	}
	for _, e := range entries {
		want, ok := wantKind[e.Name()]
		if !ok {
			t.Errorf("unexpected entry %q", e.Name())
			continue
		}
		if e.IsDir() != want.isDir {
			t.Errorf("%s IsDir = %v, want %v", e.Name(), e.IsDir(), want.isDir)
		}
		if e.Type() != want.mode {
			t.Errorf("%s Type() = %v, want %v", e.Name(), e.Type(), want.mode)
		}
		// Info() is intentionally not supported (returns error).
		if _, err := e.Info(); err == nil {
			t.Errorf("Info() for %q returned nil error", e.Name())
		}
	}
}

// TestMemPlatformReader_NormalizeEmptyPath ensures that the normalize
// helper rejects empty paths across every reader entry point. Each
// constructor (AddFile/AddDir/AddSymlink/AddError) calls normalize, so
// an empty path must not silently produce a stored entry.
func TestMemPlatformReader_NormalizeEmptyPath(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	mem.AddFile("", []byte("x"), 0o644)
	mem.AddDir("", 0o755)
	mem.AddSymlink("", "target")
	mem.AddError("", nil)

	if _, err := mem.ReadFile(""); err == nil {
		t.Error("ReadFile on empty path returned nil error")
	}
	if _, err := mem.Stat(""); err == nil {
		t.Error("Stat on empty path returned nil error")
	}
	if _, err := mem.ReadDir(""); err == nil {
		t.Error("ReadDir on empty path returned nil error")
	}
	if _, err := mem.Readlink(""); err == nil {
		t.Error("Readlink on empty path returned nil error")
	}
}

// TestOSPlatformReader_SymlinkReadFollowsTarget verifies that OSPlatformReader
// ReadFile and Stat both follow symlinks on the real filesystem. This is
// the production PathReader behaviour: callers pass a path that may be a
// symlink and expect target content / target metadata.
func TestOSPlatformReader_SymlinkReadFollowsTarget(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(target, []byte("payload"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	r := platform.NewOSPlatformReader()

	data, err := r.ReadFile(link)
	if err != nil {
		t.Fatalf("ReadFile via symlink: %v", err)
	}
	if string(data) != "payload" {
		t.Errorf("ReadFile via symlink = %q, want %q", string(data), "payload")
	}

	info, err := r.Stat(link)
	if err != nil {
		t.Fatalf("Stat via symlink: %v", err)
	}
	if info.Size() != int64(len("payload")) {
		t.Errorf("Stat size via symlink = %d, want %d", info.Size(), len("payload"))
	}
}

// TestMemPlatformReader_StatSelfLoop covers the self-referential
// symlink case for Stat (a distinct shape from the 2-node cycle covered
// by reader_test.go). The link target is its own directory entry, so
// resolveSymlinkChain must terminate with ELOOP rather than panicking.
func TestMemPlatformReader_StatSelfLoop(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	mem.AddDir("/c", 0o755)
	// Direct self-loop: link -> link.
	mem.AddSymlink("/c/loop", "loop")

	if _, err := mem.Stat("/c/loop"); err == nil {
		t.Error("Stat on self-loop returned nil error")
	}
}

// TestMemPlatformReader_ReadDirEmptyDirectory exercises the empty-directory
// branch of ReadDir.
func TestMemPlatformReader_ReadDirEmptyDirectory(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	mem.AddDir("/empty", 0o755)

	got, err := mem.ReadDir("/empty")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}
