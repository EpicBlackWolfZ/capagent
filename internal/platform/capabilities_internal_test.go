package platform

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

func TestCapabilityInspectionTransport(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{nil, unix.ENODATA, unix.ENOTSUP, unix.EACCES, unix.EIO, unix.ERANGE} {
		got, err := readCapabilityAttribute(t.Context(), func(buf []byte) (int, error) { copy(buf, "raw"); return 3, cause })
		if cause == nil {
			if !got.Present || string(got.Bytes) != "raw" || err != nil {
				t.Fatalf("present=%v %v", got, err)
			}
			continue
		}
		if cause == unix.ENODATA {
			if got.Present || err != nil {
				t.Fatalf("absent=%v %v", got, err)
			}
			continue
		}
		if !errors.Is(err, cause) || got.Present {
			t.Fatalf("cause %v = %v %v", cause, got, err)
		}
		if cause == unix.ERANGE && !errors.Is(err, ErrLimitExceeded) {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := readCapabilityAttribute(ctx, func([]byte) (int, error) {
		t.Fatal("called after cancel")
		return 0, nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestCapabilityFixtureCopy(t *testing.T) {
	t.Parallel()
	mem := NewMemPlatformReader()
	mem.AddDir("/", 0o755)
	mem.AddFile("/file", nil, 0o644)
	bytes := []byte("raw")
	if err := mem.SetFileCapabilities("/file", CapabilityAttribute{Present: true, Bytes: bytes}, nil); err != nil {
		t.Fatal(err)
	}
	bytes[0] = 'x'
	env := NewTestEnvironment(mem, nil)
	value, err := env.Reader().FileCapabilities(t.Context(), "/file")
	if err != nil || string(value.Bytes) != "raw" {
		t.Fatalf("attribute=%v %v", value, err)
	}
	value.Bytes[0] = 'x'
	mem.Snapshot()["/file"].Capabilities.Bytes[0] = 'y'
	value, err = env.Reader().FileCapabilities(t.Context(), "/file")
	if err != nil || string(value.Bytes) != "raw" {
		t.Fatalf("mutated attribute=%v %v", value, err)
	}
}
