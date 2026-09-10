package platform

import (
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestModeTranslation(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		raw  uint32
		want os.FileMode
	}{
		{"regular", unix.S_IFREG, 0}, {"directory", unix.S_IFDIR, os.ModeDir},
		{"link", unix.S_IFLNK, os.ModeSymlink}, {fifoFixtureName, unix.S_IFIFO, os.ModeNamedPipe},
		{"socket", unix.S_IFSOCK, os.ModeSocket}, {"block", unix.S_IFBLK, os.ModeDevice},
		{"character", unix.S_IFCHR, os.ModeDevice | os.ModeCharDevice}, {"unknown", 0, os.ModeIrregular},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw := tt.raw | unix.S_ISUID | unix.S_ISGID | unix.S_ISVTX | 0o640
			want := tt.want | os.ModeSetuid | os.ModeSetgid | os.ModeSticky | 0o640
			if got := scopedStatMode(raw); got != want {
				t.Fatalf("mode = %v; want %v", got, want)
			}
		})
	}
}

func TestOpenErrorIdentity(t *testing.T) {
	t.Parallel()
	for _, err := range []error{unix.ENOENT, unix.EACCES, unix.EPERM, unix.EIO, unix.EEXIST, unix.EXDEV, unix.ENOSYS} {
		t.Run(err.Error(), func(t *testing.T) {
			t.Parallel()
			if got := mapOpenError(err, "path"); !errors.Is(got, err) {
				t.Fatalf("lost %v in %v", err, got)
			}
		})
	}
}

func TestFixtureOwnershipSnapshot(t *testing.T) {
	t.Parallel()
	m := NewMemPlatformReader()
	m.AddDir("/", 0o755)
	m.AddFile("/file", nil, os.ModeSetuid|0o640)
	before, err := m.Stat("/file")
	if err != nil {
		t.Fatal(err)
	}
	if _, known := OwnershipOf(before); known {
		t.Fatal("unspecified fixture owner is known")
	}
	if err := m.SetOwnership("/file", FileOwnership{}); err != nil {
		t.Fatal(err)
	}
	r := NewScopedMemReader("/", m)
	entries, err := r.ReadDir(t.Context(), ".")
	if err != nil {
		t.Fatal(err)
	}
	info, err := entries[0].Info()
	if err != nil {
		t.Fatal(err)
	}
	if owner, known := OwnershipOf(info); !known || owner != (FileOwnership{}) {
		t.Fatalf("owner = %v, %v", owner, known)
	}
	if info.Mode()&os.ModeSetuid == 0 {
		t.Fatal("lost setuid")
	}
	if err := m.SetOwnership("/file", FileOwnership{UID: 1}); err != nil {
		t.Fatal(err)
	}
	m.Snapshot()["/file"].Ownership.UID = 2
	if owner, _ := OwnershipOf(info); owner.UID != 0 {
		t.Fatal("snapshot mutated")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, known := OwnershipOf(info); !known {
		t.Fatal("ownership lost after close")
	}
}
