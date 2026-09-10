package platform

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestBoundedByteLoop(t *testing.T) {
	t.Parallel()
	const limit = 7
	for _, length := range []int{0, limit - 1, limit, limit + 1} {
		data := strings.Repeat("x", length)
		got, err := readBounded(t.Context(), limit, strings.NewReader(data).Read)
		if len(got) != min(length, limit) || (length > limit) != errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("len=%d got %q, %v", length, got, err)
		}
	}
	calls, consumed := 0, 0
	got, err := readBounded(t.Context(), limit, func(p []byte) (int, error) { calls++; consumed += len(p); return len(p), nil })
	if len(got) != limit || consumed != limit+1 || calls > 2 || !errors.Is(err, ErrIncomplete) {
		t.Fatalf("stream = %d, %d, %d, %v", len(got), consumed, calls, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	got, err = readBounded(ctx, limit, func(p []byte) (int, error) { p[0] = 'x'; cancel(); return 1, unix.EIO })
	if string(got) != "x" || !errors.Is(err, unix.EIO) || !errors.Is(err, context.Canceled) {
		t.Fatalf("partial = %q, %v", got, err)
	}
	_, err = readBounded(ctx, limit, func([]byte) (int, error) { t.Fatal("read after cancellation"); return 0, io.EOF })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestCheckedRegularRejectsSpecialAndReplacement(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "file")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := NewScopedOSReader(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	scoped := reader.(*ScopedOSReader)
	for _, special := range []bool{false, true} {
		t.Run(map[bool]string{false: "replacement", true: fifoFixtureName}[special], func(t *testing.T) {
			if special {
				if err := unix.Mkfifo(filepath.Join(root, fifoFixtureName), 0o600); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(root, fifoFixtureName)
			}
			opens := 0
			withFD := func(flags uint64, fn func(int) error) error {
				opens++
				if opens == 1 && flags != unix.O_PATH {
					t.Fatal("preflight must not data-open the FIFO")
				}
				if !special && opens == 2 {
					replacement := filepath.Join(root, "replacement")
					if err := os.WriteFile(replacement, []byte("new"), 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(replacement, path); err != nil {
						t.Fatal(err)
					}
				}
				return scoped.readSubpath(filepath.Base(path), flags, fn)
			}
			err := checkedRegular(t.Context(), withFD, func(int) error { t.Fatal("unsafe descriptor read"); return nil })
			if special && (!errors.Is(err, ErrFileType) || opens != 1) {
				t.Fatalf("fifo = %d, %v", opens, err)
			}
			if !special && !errors.Is(err, ErrFileChanged) {
				t.Fatal(err)
			}
		})
	}
}
