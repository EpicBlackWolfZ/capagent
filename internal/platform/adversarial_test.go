package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const adversarialMarker = "capagent-platform-adversarial"

func TestAdversarialPlatform(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"paths", "parsers", "reads"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			const parentTimeout = 15 * time.Second
			ctx, cancel := context.WithTimeout(t.Context(), parentTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAdversarialPlatformHelper$", "--", adversarialMarker, mode)
			cmd.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C", "GORACE=atexit_sleep_ms=0"}
			cmd.WaitDelay = time.Second
			const outputLimit = 1 << 20
			capture := newBoundedBuffer(outputLimit)
			cmd.Stdout, cmd.Stderr = capture, capture
			if err := cmd.Run(); err != nil || capture.Truncated() {
				t.Fatalf("%s helper: %v\n%s", mode, err, capture.Bytes())
			}
		})
	}
}

func TestAdversarialPlatformHelper(t *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-2] != adversarialMarker {
		return
	}
	const scenarioTimeout = 10 * time.Second
	watchdog := time.AfterFunc(scenarioTimeout, func() { fmt.Fprintln(os.Stderr, "platform resource watchdog"); os.Exit(2) })
	defer watchdog.Stop()
	switch os.Args[len(os.Args)-1] {
	case "paths":
		adversarialPaths(t)
	case "parsers":
		adversarialParsers(t)
	case "reads":
		adversarialReads(t)
	default:
		t.Fatal("unknown adversarial mode")
	}
}

func adversarialPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "value"), []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := NewScopedOSReader(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	const kernelHops = 40
	for _, hops := range []int{kernelHops, kernelHops + 1} {
		for i := 0; i < hops; i++ {
			name := fmt.Sprintf("%d-link-%d", hops, i)
			target := fmt.Sprintf("%d-link-%d", hops, i+1)
			if i == hops-1 {
				target = "value"
			}
			if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
				t.Fatal(err)
			}
		}
		data, err := r.ReadFile(t.Context(), fmt.Sprintf("%d-link-0", hops))
		if hops > kernelHops {
			if !errors.Is(err, unix.ELOOP) {
				t.Fatalf("hop rejection: %v", err)
			}
		} else if err != nil || string(data) != "inside" {
			t.Fatalf("hop boundary: %q %v", data, err)
		}
	}
	const depth = 128
	deep := strings.Repeat("d/", depth)
	if err := os.MkdirAll(filepath.Join(root, deep), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, deep, "value"), []byte("deep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err := r.ReadFile(t.Context(), deep+"value"); err != nil || string(data) != "deep" {
		t.Fatalf("deep path %v", err)
	}
	const longPathRepeats = 4096
	for _, path := range []string{strings.Repeat("x", unix.NAME_MAX+1), strings.Repeat("./", longPathRepeats) + "value"} {
		if _, err := r.ReadFile(t.Context(), path); !errors.Is(err, unix.ENAMETOOLONG) {
			t.Fatalf("long path: %v", err)
		}
	}
	const traversalCount = 8192
	for _, path := range []string{strings.Repeat("../", traversalCount) + "outside", strings.Repeat("d/../", traversalCount) + "../outside"} {
		if _, err := r.ReadFile(t.Context(), path); !errors.Is(err, ErrSubpathEscape) {
			t.Fatalf("deep escape: %v", err)
		}
	}
}

func adversarialParsers(t *testing.T) {
	limits := DefaultParserLimits()
	for _, parser := range []struct {
		name, line string
		call       func(*ProcfsReader) (int, error)
	}{
		{"filesystems", "ext4\n", func(p *ProcfsReader) (int, error) { r, e := p.Filesystems(t.Context()); return len(r), e }},
		{"cgroups", "0::/scope\n", func(p *ProcfsReader) (int, error) { r, e := p.Cgroups(t.Context()); return len(r), e }},
		{"mounts", "1 0 0:1 / / rw - proc proc rw\n", func(p *ProcfsReader) (int, error) { r, e := p.Mounts(t.Context()); return len(r), e }},
	} {
		for _, test := range []struct {
			name, input string
			count       int
			cause       error
		}{
			{"input", strings.Repeat(" ", limits.InputBytes+1), 0, ErrLimitExceeded},
			{"giant line", parser.line + strings.Repeat("x", limits.MountLineBytes+1) + "\n" + parser.line, 2, ErrLimitExceeded},
			{"record cap", strings.Repeat(parser.line, limits.Records+1), limits.Records, ErrLimitExceeded},
			{"malformed storm", strings.Repeat("bad extra tokens !\n", limits.Records) + parser.line, 1, ErrMalformed},
		} {
			p := NewProcfsReader(partialReader{data: []byte(test.input)})
			count, err := parser.call(p)
			if count != test.count || !errors.Is(err, test.cause) || !errors.Is(err, ErrIncomplete) {
				t.Fatalf("%s/%s count=%d err=%v", parser.name, test.name, count, err)
			}
			var diagnostic *ParseError
			if errors.As(err, &diagnostic) && len(diagnostic.Diagnostics) > limits.Diagnostics {
				t.Fatal("diagnostic budget exceeded")
			}
			if test.name == "malformed storm" && (diagnostic == nil || diagnostic.Omitted != limits.Records-limits.Diagnostics) {
				t.Fatal("diagnostic omissions lost")
			}
		}
	}
	for _, input := range []string{strings.Repeat("cpu ", limits.Records*2), strings.Repeat("invalid! ", limits.Records)} {
		controllers, err := parseControllers(t.Context(), []byte(input), nil)
		if len(controllers) > limits.Records {
			t.Fatal("controller record budget")
		}
		var diagnostic *ParseError
		if errors.As(err, &diagnostic) && len(diagnostic.Diagnostics) > limits.Diagnostics {
			t.Fatal("controller diagnostic budget")
		}
	}
}

func adversarialReads(t *testing.T) {
	const byteLimit = 64 << 10
	for _, final := range []error{io.EOF, unix.EIO, unix.EACCES} {
		calls := 0
		got, err := readBounded(t.Context(), byteLimit, func(p []byte) (int, error) {
			calls++
			if calls == 1 {
				p[0] = 'a'
				return 1, unix.EINTR
			}
			if calls == 2 {
				p[0] = 'b'
				return 1, nil
			}
			return 0, final
		})
		want := final
		if final == io.EOF {
			want = nil
		}
		if !bytes.Equal(got, []byte("ab")) || !errors.Is(err, want) || calls != 3 {
			t.Fatalf("short read sequence: %q %v calls=%d", got, err, calls)
		}
	}
	consumed := 0
	got, err := readBounded(t.Context(), byteLimit, func(p []byte) (int, error) { consumed += len(p); return len(p), nil })
	if consumed != byteLimit+1 || len(got) != byteLimit || !errors.Is(err, ErrLimitExceeded) {
		t.Fatal("endless source consumed beyond sentinel")
	}
	limits := DefaultReadLimits()
	limits.DirectoryEntries = 32
	for _, cause := range []error{unix.ENOENT, unix.EACCES, unix.EIO, unix.ELOOP, unix.ENOTDIR} {
		lookups := 0
		entries, err := captureDirectory(t.Context(), limits, namesForTest("a", "b", "c"), func(name string) (os.FileInfo, error) {
			lookups++
			if name == "b" {
				return nil, cause
			}
			return &memFileInfo{name: name}, nil
		})
		if cause == unix.ENOENT {
			if err != nil || len(entries) != 2 || lookups != 3 {
				t.Fatal("disappearing entry")
			}
		} else if !errors.Is(err, cause) || entries != nil || lookups != 2 {
			t.Fatal("metadata failure must discard entries and stop")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	_, err = readBounded(ctx, byteLimit, func(p []byte) (int, error) { calls++; cancel(); p[0] = 'x'; return 1, nil })
	if calls != 1 || !errors.Is(err, context.Canceled) {
		t.Fatal("read continued after cancellation")
	}
	// Actual large regular input verifies the production byte cap rather than
	// relying only on the finite callback surrogate.
	root := t.TempDir()
	path := filepath.Join(root, "large")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), byteLimit+1), 0o600); err != nil {
		t.Fatal(err)
	}
	limits.FileBytes = byteLimit
	r, err := NewScopedOSReaderWithLimits(root, limits)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	data, err := r.ReadFile(t.Context(), "large")
	if len(data) != byteLimit || !errors.Is(err, ErrLimitExceeded) {
		t.Fatal("OS byte budget")
	}
}
