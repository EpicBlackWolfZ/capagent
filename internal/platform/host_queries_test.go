package platform

import (
	"errors"
	"golang.org/x/sys/unix"
	"testing"
)

func TestHostQueriesNative(t *testing.T) {
	t.Parallel()
	info, err := (LinuxHostQueries{}).Uname()
	if err != nil || info.Release == "" || info.Machine == "" {
		t.Fatalf("uname: %+v %v", info, err)
	}
	_, err = (LinuxHostQueries{}).NoNewPrivileges()
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewScopedOSReader("/")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	infoFS, err := r.StatFS(t.Context(), "proc")
	if err != nil || infoFS.Type != unix.PROC_SUPER_MAGIC {
		t.Fatalf("statfs: %+v %v", infoFS, err)
	}
	if _, err = r.StatFS(t.Context(), "../escape"); err == nil {
		t.Fatal("escaped statfs")
	}
	r.Close()
	if _, err = r.StatFS(t.Context(), "proc"); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestHostMemoryStatFSAndQueries(t *testing.T) {
	t.Parallel()
	mem := NewMemPlatformReader()
	mem.AddDir("/proc", 0o755)
	mem.AddFile("/proc/status", nil, 0o644)
	mounts := map[string]FilesystemInfo{"/": {Type: 1}, "/proc": {Type: 2}}
	r := NewScopedMemReaderWithFilesystems("/", mem, mounts)
	defer r.Close()
	mounts["/proc"] = FilesystemInfo{Type: 3}
	info, err := r.StatFS(t.Context(), "proc/status")
	if err != nil || info.Type != 2 {
		t.Fatal("lost scoped mount or aliased snapshot")
	}
	if _, err = r.StatFS(t.Context(), "missing"); err == nil {
		t.Fatal("missing statfs")
	}
	bare := NewScopedMemReader("/", mem)
	defer bare.Close()
	if _, err = bare.StatFS(t.Context(), "proc"); !errors.Is(err, ErrIncomplete) {
		t.Fatal(err)
	}
	snap := HostSnapshot{UnameError: unix.EPERM, SecurityError: unix.ENOSYS}
	if _, err = snap.Uname(); !errors.Is(err, unix.EPERM) {
		t.Fatal(err)
	}
	if _, err = snap.NoNewPrivileges(); !errors.Is(err, unix.ENOSYS) {
		t.Fatal(err)
	}
	if _, err = (HostMetadata{}).SystemdVersion(t.Context(), "/usr/bin/systemctl"); err == nil {
		t.Fatal("missing runner")
	}
	if _, err = NewHostMetadata(NewFakeCommandRunner()).SystemdVersion(t.Context(), "/tmp/systemctl"); err == nil {
		t.Fatal("command escaped allowlist")
	}
	r.Close()
	if _, err = r.StatFS(t.Context(), "proc"); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}
