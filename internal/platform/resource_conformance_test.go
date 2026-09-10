package platform

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const fifoFixtureName = "fifo"

type readerCase struct {
	name, prefix string
	reader       PlatformReader
}

func resourceReaders(t *testing.T, limits ReadLimits) []readerCase {
	t.Helper()
	root := t.TempDir()
	mem, err := NewMemPlatformReaderWithLimits(limits)
	if err != nil {
		t.Fatal(err)
	}
	mem.AddDir("/", 0o755)
	for _, name := range []string{"a", "b", "c", "d"} {
		data := []byte(strings.Repeat(name, limits.FileBytes+1))
		if err := os.WriteFile(filepath.Join(root, name), data, 0o640); err != nil {
			t.Fatal(err)
		}
		mem.AddFile("/"+name, data, 0o640)
	}
	scoped, err := NewScopedOSReaderWithLimits(root, limits)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := scoped.Close(); err != nil {
			t.Error(err)
		}
	})
	host, err := NewOSPlatformReaderWithLimits(limits)
	if err != nil {
		t.Fatal(err)
	}
	scopedMem, err := NewScopedMemReaderWithLimits("/", mem, limits)
	if err != nil {
		t.Fatal(err)
	}
	return []readerCase{{"scoped-os", "", scoped}, {"host-os", root + "/", host}, {"scoped-memory", "", scopedMem}, {"memory", "/", mem}}
}

func TestReadBudgetConformance(t *testing.T) {
	t.Parallel()
	const smallBytes = 3
	for _, limits := range []ReadLimits{
		{FileBytes: smallBytes, DirectoryEntries: 2, DirectoryNameBytes: 100},
		{FileBytes: smallBytes, DirectoryEntries: 10, DirectoryNameBytes: 2},
		{FileBytes: smallBytes, DirectoryEntries: 4, DirectoryNameBytes: 4},
	} {
		for _, tt := range resourceReaders(t, limits) {
			t.Run(fmt.Sprintf("%s-%d-%d", tt.name, limits.DirectoryEntries, limits.DirectoryNameBytes), func(t *testing.T) {
				data, err := tt.reader.ReadFile(t.Context(), tt.prefix+"a")
				var limitErr *LimitError
				if string(data) != "aaa" || !errors.As(err, &limitErr) || limitErr.Limit != smallBytes {
					t.Fatalf("file=%q %v", data, err)
				}
				if limitErr.Error() == "" {
					t.Fatal("missing diagnostic")
				}
				entries, err := tt.reader.ReadDir(t.Context(), tt.prefix+".")
				expected := min(4, limits.DirectoryEntries, limits.DirectoryNameBytes)
				if len(entries) != expected || (expected < 4) != errors.Is(err, ErrIncomplete) {
					t.Fatalf("dir=%v %v", entries, err)
				}
				previous := ""
				for _, entry := range entries {
					if entry.Name() <= previous {
						t.Fatal("unsorted subset")
					}
					previous = entry.Name()
					if _, err := entry.Info(); err != nil {
						t.Fatal(err)
					}
				}
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				if _, err := tt.reader.ReadFile(ctx, tt.prefix+"missing"); !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if _, err := tt.reader.ReadDir(ctx, tt.prefix+"."); !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if _, err := tt.reader.FileCapabilities(ctx, tt.prefix+"a"); !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				attr, err := tt.reader.FileCapabilities(t.Context(), tt.prefix+"a")
				if err != nil || attr.Present {
					t.Fatalf("absent xattr=%v %v", attr, err)
				}
				if _, err := tt.reader.FileCapabilities(t.Context(), tt.prefix+"missing"); !errors.Is(err, os.ErrNotExist) {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestBudgetValidation(t *testing.T) {
	t.Parallel()
	invalid := []ReadLimits{{}, {FileBytes: -1, DirectoryEntries: 1, DirectoryNameBytes: 1},
		{FileBytes: int(^uint(0) >> 1), DirectoryEntries: 1, DirectoryNameBytes: 1}}
	for _, limits := range invalid {
		if _, err := NewOSPlatformReaderWithLimits(limits); err == nil {
			t.Fatal("host accepted invalid limits")
		}
		if _, err := NewScopedOSReaderWithLimits("/does-not-exist", limits); err == nil {
			t.Fatal("scoped accepted invalid limits")
		}
		if _, err := NewMemPlatformReaderWithLimits(limits); err == nil {
			t.Fatal("memory accepted invalid limits")
		}
		if _, err := NewScopedMemReaderWithLimits("/", nil, limits); err == nil {
			t.Fatal("scoped memory accepted invalid limits")
		}
	}
}

// A parent-owned process deadline makes the FIFO regression safe even if an
// implementation reintroduces a blocking open. No writer or orphan goroutine is needed.
func TestFIFOReadHelper(t *testing.T) {
	const helperArg = "capagent-fifo-read"
	if len(os.Args) < 2 || os.Args[len(os.Args)-1] != helperArg {
		return
	}
	root := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(root, fifoFixtureName), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fifoFixtureName, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	scoped, err := NewScopedOSReader(root)
	if err != nil {
		t.Fatal(err)
	}
	defer scoped.Close()
	for _, tt := range []readerCase{{"scoped", "", scoped}, {"host", root + "/", NewOSPlatformReader()}} {
		for _, name := range []string{fifoFixtureName, "link"} {
			if _, err := tt.reader.ReadFile(t.Context(), tt.prefix+name); !errors.Is(err, ErrFileType) {
				t.Fatal(err)
			}
			if _, err := tt.reader.FileCapabilities(t.Context(), tt.prefix+name); !errors.Is(err, ErrFileType) {
				t.Fatal(err)
			}
		}
	}
}
func TestFIFOReadIsBounded(t *testing.T) {
	t.Parallel()
	const helperTimeout = 10 * time.Second
	ctx, cancel := context.WithTimeout(t.Context(), helperTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFIFOReadHelper$", "--", "capagent-fifo-read")
	cmd.WaitDelay = time.Second
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("FIFO helper: %v %s", err, out)
	}
}

func TestMetadataOSConformance(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	regular := filepath.Join(root, "file")
	if err := os.WriteFile(regular, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	mode := os.FileMode(0o640) | os.ModeSetuid | os.ModeSetgid | os.ModeSticky
	if err := os.Chmod(regular, mode); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(root, fifoFixtureName), 0o600); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	err = unix.Bind(fd, &unix.SockaddrUnix{Name: filepath.Join(root, "socket")})
	closeErr := unix.Close(fd)
	if err != nil || closeErr != nil {
		t.Fatalf("socket fixture: %v %v", err, closeErr)
	}
	scoped, err := NewScopedOSReader(root)
	if err != nil {
		t.Fatal(err)
	}
	defer scoped.Close()
	mem := NewMemPlatformReader()
	mem.AddDir("/", 0o755)
	mem.AddFile("/file", []byte("x"), mode)
	for name, mode := range map[string]os.FileMode{fifoFixtureName: os.ModeNamedPipe | 0o600, "socket": os.ModeSocket | 0o755} {
		if err := mem.AddSpecial("/"+name, mode); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"file", fifoFixtureName, "socket"} {
		physical, err := os.Stat(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		owner, known := OwnershipOf(physical)
		if !known {
			t.Fatal("OS owner unknown")
		}
		if err := mem.SetOwnership("/"+name, owner); err != nil {
			t.Fatal(err)
		}
		for _, tt := range []readerCase{{"scoped", "", scoped}, {"host", root + "/", NewOSPlatformReader()},
			{"memory", "/", mem}, {"scoped-memory", "", NewScopedMemReader("/", mem)}} {
			info, err := tt.reader.Stat(tt.prefix + name)
			if err != nil {
				t.Fatal(err)
			}
			got, known := OwnershipOf(info)
			if got != owner || !known || info.Mode().Type() != physical.Mode().Type() {
				t.Fatalf("%s: metadata mismatch %v", tt.name, info)
			}
			if name == "file" && info.Mode() != mode {
				t.Fatalf("%s: mode %v != %v", tt.name, info.Mode(), mode)
			}
		}
	}
	entries, err := scoped.ReadDir(t.Context(), ".")
	if err != nil {
		t.Fatal(err)
	}
	if err := scoped.Close(); err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		if _, known := OwnershipOf(info); !known {
			t.Fatal("lost ownership")
		}
	}
}

func TestOSCapabilityAttributePresent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "capability")
	if err := os.WriteFile(path, []byte("never executed"), 0o600); err != nil {
		t.Fatal(err)
	}
	const revision2 = 0x02000000
	const capabilitySize = 20
	const permittedOffset = 4
	raw := make([]byte, capabilitySize)
	binary.LittleEndian.PutUint32(raw, revision2)
	binary.LittleEndian.PutUint32(raw[permittedOffset:], 1)
	if err := unix.Setxattr(path, "security.capability", raw, 0); err != nil {
		if errors.Is(err, unix.EPERM) || errors.Is(err, unix.ENOTSUP) {
			t.Skipf("present capability fixture requires CAP_SETFCAP/xattrs: %v", err)
		}
		t.Fatal(err)
	}
	scoped, err := NewScopedOSReader(root)
	if err != nil {
		t.Fatal(err)
	}
	defer scoped.Close()
	for _, tt := range []readerCase{{"scoped", "", scoped}, {"host", root + "/", NewOSPlatformReader()}} {
		got, err := tt.reader.FileCapabilities(t.Context(), tt.prefix+"capability")
		if err != nil || !got.Present || !bytes.Equal(got.Bytes, raw) {
			t.Fatalf("%s: attribute=%v %v", tt.name, got, err)
		}
	}
}
