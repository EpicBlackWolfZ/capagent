package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMetadataIdentityKnownAndUnknown(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	name := filepath.Join(root, "file")
	if err := os.WriteFile(name, []byte("metadata"), 0o644); err != nil {
		t.Fatal(err)
	}
	native, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	want, known := IdentityOf(native)
	if !known || want.Inode == 0 {
		t.Fatal("native identity unavailable")
	}
	files, err := NewScopedOSReader(root)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	confined, err := files.Stat("file")
	if err != nil {
		t.Fatal(err)
	}
	got, known := IdentityOf(confined)
	if !known || got != want {
		t.Fatal("confined identity differs")
	}
	mem := NewMemPlatformReader()
	mem.AddFile("/file", nil, 0o644)
	virtual, err := mem.Stat("/file")
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range []os.FileInfo{nil, virtual, identityWithoutSystemData{native}} {
		if _, known := IdentityOf(info); known {
			t.Fatal("invented identity")
		}
		if IsNullDevice(info) {
			t.Fatal("invented null device")
		}
	}
	null, err := os.Stat("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	if !IsNullDevice(null) || IsNullDevice(identityWithoutSystemData{null}) {
		t.Fatal("invalid device metadata")
	}
}

type identityWithoutSystemData struct{ os.FileInfo }

func (identityWithoutSystemData) Sys() any { return nil }
