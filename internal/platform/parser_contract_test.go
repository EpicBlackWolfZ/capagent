package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

type partialReader struct {
	ScopedReader
	data []byte
	err  error
}

func (r partialReader) ReadFile(context.Context, string) ([]byte, error) { return r.data, r.err }

func TestParserCompleteness(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, path, line string
		parse            func(*ProcfsReader, context.Context) (int, error)
	}{
		{"filesystems", "filesystems", "nodev proc\n", func(p *ProcfsReader, c context.Context) (int, error) {
			v, e := p.Filesystems(c)
			return len(v), e
		}},
		{"cgroup", "self/cgroup", "0::/path\n", func(p *ProcfsReader, c context.Context) (int, error) { v, e := p.Cgroups(c); return len(v), e }},
		{"mountinfo", "self/mountinfo", "1 0 0:1 / / rw - proc proc rw\n", func(p *ProcfsReader, c context.Context) (int, error) {
			v, e := p.Mounts(c)
			return len(v), e
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for _, bad := range []string{"bad invalid data\n", strings.Repeat("x", mountinfoMaxLineBytes+1) + "\n"} {
				p := NewProcfsReader(partialReader{data: []byte(tt.line + bad + tt.line)})
				n, err := tt.parse(p, t.Context())
				if n != 2 || !errors.Is(err, ErrIncomplete) {
					t.Fatalf("records=%d error=%v", n, err)
				}
			}
			p := NewProcfsReader(partialReader{data: []byte(tt.line + strings.TrimSuffix(tt.line, "\n")), err: unix.EIO})
			n, err := tt.parse(p, t.Context())
			if n != 1 || !errors.Is(err, unix.EIO) || !errors.Is(err, ErrIncomplete) {
				t.Fatalf("partial=%d %v", n, err)
			}
		})
	}
}

func TestSysfsRejectsUnexpectedValues(t *testing.T) {
	t.Parallel()
	m := NewMemPlatformReader()
	m.AddDir("/", 0o755)
	m.AddDir("/fs", 0o755)
	m.AddDir("/fs/selinux", 0o755)
	s := NewSysfsReader(NewScopedMemReader("/", m))
	for _, text := range []string{"", "2", "yes", "0 1"} {
		m.AddFile("/fs/selinux/enforce", []byte(text), 0o644)
		if v, err := s.IsSELinuxEnforcing(t.Context()); v || !errors.Is(err, ErrMalformed) {
			t.Fatalf("%q => %v %v", text, v, err)
		}
	}
	for _, name := range []string{"cpu\\max", "cpu..max", "cpu\x00max", "cpu max", "CPU.max"} {
		if _, err := s.ReadCgroupFile(t.Context(), name); !errors.Is(err, ErrMalformed) {
			t.Fatalf("accepted %q: %v", name, err)
		}
	}
}

func TestParserLimitsAndDiagnostics(t *testing.T) {
	t.Parallel()
	limits := DefaultParserLimits()
	limits.Diagnostics = 2
	limits.Records = 2
	limits.LineBytes = 16
	limits.InputBytes = 128
	tests := []struct {
		name, input        string
		count              int
		malformed, limited bool
	}{
		{"diagnostic storm", strings.Repeat("bad extra tokens\n", 5) + "ext4\n", 1, true, false},
		{"record budget", "ext4\nxfs\nbtrfs\n", 2, false, true},
		{"line budget", "ext4\n" + strings.Repeat("x", 17) + "\nxfs\n", 2, false, true},
		{"input budget", strings.Repeat(" \n", 64) + "ext4\n", 0, false, true},
		{"clean unterminated", "ext4", 1, false, false},
		{"empty", " \n", 0, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewProcfsReaderWithLimits(partialReader{data: []byte(tt.input)}, limits)
			if err != nil {
				t.Fatal(err)
			}
			got, err := p.Filesystems(t.Context())
			if len(got) != tt.count || errors.Is(err, ErrMalformed) != tt.malformed || errors.Is(err, ErrLimitExceeded) != tt.limited {
				t.Fatalf("records %d error %v", len(got), err)
			}
			if err == nil {
				return
			}
			if err.Error() == "" {
				t.Fatal("missing error text")
			}
			var diagnostic *ParseError
			if errors.As(err, &diagnostic) {
				if len(diagnostic.Diagnostics) > limits.Diagnostics {
					t.Fatal("unbounded diagnostics")
				}
				if tt.name == "diagnostic storm" && diagnostic.Omitted != 3 {
					t.Fatalf("omitted=%d", diagnostic.Omitted)
				}
			}
			var limitErr *LimitError
			if tt.limited && !errors.As(err, &limitErr) {
				t.Fatalf("missing typed limit: %v", err)
			}
		})
	}
	if _, err := NewProcfsReaderWithLimits(nil, limits); err == nil {
		t.Fatal("nil reader accepted")
	}
	for _, invalid := range []ParserLimits{{}, {InputBytes: -1}, {InputBytes: int(^uint(0) >> 1)}} {
		if _, err := NewProcfsReaderWithLimits(partialReader{}, invalid); err == nil {
			t.Fatal("invalid parser limits accepted")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	p, _ := NewProcfsReaderWithLimits(partialReader{data: []byte("ext4\n")}, limits)
	if got, err := p.Filesystems(ctx); len(got) != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled=%v %v", got, err)
	}
}

func TestRecordGrammar(t *testing.T) {
	t.Parallel()
	for _, line := range []string{"::/", "a::/", "1::relative", "1:cpu,,memory:/", "1:CPU:/", "1:cpu:/a\x00"} {
		if _, err := parseCgroupLine(line); !errors.Is(err, ErrMalformed) {
			t.Fatalf("cgroup %q: %v", line, err)
		}
	}
	for _, line := range []string{"a 0 0:1 / / rw - proc proc rw", "1 0 x:y / / rw - proc proc rw",
		"1 0 0:1 relative / rw - proc proc rw", "1 0 0:1 / / rw - proc proc rw extra", "1 0 0:1 / / rw - proc proc rw\x00"} {
		if _, err := parseMountLine(line); !errors.Is(err, ErrMalformed) {
			t.Fatalf("mount %q: %v", line, err)
		}
	}
	mount, err := parseMountLine(`1 0 0:1 /a\040b /c\134d rw future:1 - ext4 /dev/a\040b rw`)
	if err != nil || mount.Root != `/a\040b` || mount.MountPoint != `/c\134d` || mount.MountSource != `/dev/a\040b` {
		t.Fatalf("raw mount=%v %v", mount, err)
	}
	cgroup, err := parseCgroupLine("1:name=systemd:/has:colon")
	if err != nil || cgroup.Path != "/has:colon" {
		t.Fatalf("cgroup=%v %v", cgroup, err)
	}
}

func TestControllerCompleteness(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		input             string
		cause, errorClass error
		count             int
	}{
		{"cpu invalid! memory", nil, ErrMalformed, 2},
		{"cpu cpu memory", nil, nil, 2},
		{"cpu memory", unix.EIO, unix.EIO, 1},
		{"cpu " + strings.Repeat("x", defaultLineBytes+1), nil, ErrLimitExceeded, 1},
		{strings.Repeat(" ", defaultFileBytes+1), nil, ErrLimitExceeded, 0},
	} {
		got, err := parseControllers(t.Context(), []byte(tt.input), tt.cause)
		if len(got) != tt.count || !errors.Is(err, tt.errorClass) {
			t.Fatalf("controllers count=%d error=%v", len(got), err)
		}
	}
	var input strings.Builder
	for n := 0; n <= defaultRecords; n++ {
		fmt.Fprintf(&input, "c%d ", n)
	}
	got, err := parseControllers(t.Context(), []byte(input.String()), nil)
	if len(got) != defaultRecords || !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("record cap=%d %v", len(got), err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := parseControllers(ctx, []byte("cpu"), nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestSysfsMissingAndCancelled(t *testing.T) {
	t.Parallel()
	m := NewMemPlatformReader()
	m.AddDir("/", 0o755)
	m.AddDir("/fs", 0o755)
	m.AddDir("/fs/selinux", 0o755)
	s := NewSysfsReader(NewScopedMemReader("/", m))
	if _, err := s.IsSELinuxEnforcing(t.Context()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	m.AddError("/fs/selinux/enforce", unix.EIO)
	if _, err := s.SELinuxMode(t.Context()); !errors.Is(err, unix.EIO) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.IsSELinuxEnforcing(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, name := range []string{"cpu.max", "memory.current", "cgroup.controllers", "cpu.1"} {
		if err := validateCgroupFilename(name); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for _, name := range []string{"cpu.MAX", "cpu. ", "cpu.\\", "cpu."} {
		if err := validateCgroupFilename(name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
}

func TestCgroupPathPreservesTrailingWhitespace(t *testing.T) {
	t.Parallel()
	p := NewProcfsReader(partialReader{data: []byte("0::/trailing space \n")})
	got, err := p.Cgroups(t.Context())
	if err != nil || len(got) != 1 || got[0].Path != "/trailing space " {
		t.Fatalf("path changed: %v %v", got, err)
	}
}

func TestFilesystemRejectsNUL(t *testing.T) {
	t.Parallel()
	p := NewProcfsReader(partialReader{data: []byte("nodev pro\x00c\n")})
	if got, err := p.Filesystems(t.Context()); len(got) != 0 || !errors.Is(err, ErrMalformed) {
		t.Fatalf("NUL=%v %v", got, err)
	}
}

func TestNamedCgroupHierarchyGrammar(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"systemd", "Build-Team.v1", "1_legacy", ".hidden"} {
		line := "1:name=" + name + ":/path"
		if got, err := parseCgroupLine(line); err != nil || got.Controllers != "name="+name {
			t.Fatalf("named hierarchy %q: %v %v", name, got, err)
		}
	}
	for _, name := range []string{"", "bad/name", "white space", "bad=extra"} {
		if _, err := parseCgroupLine("1:name=" + name + ":/path"); !errors.Is(err, ErrMalformed) {
			t.Fatalf("accepted hierarchy %q: %v", name, err)
		}
	}
}
