package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/tests/fuzzutil"
)

const fuzzRecords = 256
const fuzzDiagnostics = 8

func fuzzTransport(data []byte) ([]byte, context.Context, error) {
	ctx := context.Background()
	if len(data) == 0 {
		return data, ctx, nil
	}
	const modes = 4
	switch data[0] % modes {
	case 1:
		return data[1:], ctx, errors.New("synthetic read failure")
	case 2:
		return data[1:], ctx, &LimitError{Resource: "fixture", Limit: len(data)}
	case 3:
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		return data[1:], cancelled, nil
	default:
		return data[1:], ctx, nil
	}
}

func fuzzParser[T any](f *testing.F, valid string, parse func(*ProcfsReader, context.Context) ([]T, error)) {
	f.Helper()
	for _, seed := range []string{"", "\x00" + valid, "\x01" + valid + "broken", "\x00bad\n" + valid, "\x03" + valid} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		fuzzutil.Default(fuzzutil.ParserInput).Run(t, input, func() {
			before := bytes.Clone(input)
			data, ctx, readErr := fuzzTransport(input)
			limits := DefaultParserLimits()
			limits.InputBytes, limits.LineBytes, limits.MountLineBytes = fuzzutil.ParserInput, fuzzutil.PathInput, fuzzutil.PathInput
			limits.Records, limits.Diagnostics = fuzzRecords, fuzzDiagnostics
			reader, err := NewProcfsReaderWithLimits(partialReader{data: data, err: readErr}, limits)
			if err != nil {
				t.Fatal(err)
			}
			a, ae := parse(reader, ctx)
			b, be := parse(reader, ctx)
			if !reflect.DeepEqual(a, b) || fmt.Sprint(ae) != fmt.Sprint(be) || !bytes.Equal(input, before) {
				t.Fatal("parser is nondeterministic or mutates input")
			}
			if len(a) > limits.Records {
				t.Fatal("record budget exceeded")
			}
			var diagnostic *ParseError
			if errors.As(ae, &diagnostic) && len(diagnostic.Diagnostics) > limits.Diagnostics {
				t.Fatal("diagnostic budget exceeded")
			}
			if (readErr != nil || ctx.Err() != nil) && !errors.Is(ae, ErrIncomplete) {
				t.Fatal("incomplete transport hidden")
			}
			if readErr != nil && !errors.Is(ae, readErr) {
				t.Fatal("transport error lost")
			}
			if ctx.Err() != nil && (!errors.Is(ae, ctx.Err()) || len(a) != 0) {
				t.Fatal("cancellation lost")
			}
		})
	})
}

func FuzzMountinfo(f *testing.F) {
	fuzzParser(f, "1 0 0:1 / / rw - proc proc rw\n", (*ProcfsReader).Mounts)
}
func FuzzCgroups(f *testing.F)     { fuzzParser(f, "0::/\n1:cpu,memory:/test\n", (*ProcfsReader).Cgroups) }
func FuzzFilesystems(f *testing.F) { fuzzParser(f, "nodev proc\next4\n", (*ProcfsReader).Filesystems) }

func FuzzSysfsState(f *testing.F) {
	for _, seed := range []string{"", "\x000", "\x001", "\x00cpu memory\n", "\x01cpu memory", "\x032"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		fuzzutil.Default(fuzzutil.ParserInput).Run(t, input, func() {
			before := bytes.Clone(input)
			data, ctx, readErr := fuzzTransport(input)
			a, ae := parseControllers(ctx, data, readErr)
			b, be := parseControllers(ctx, data, readErr)
			if !reflect.DeepEqual(a, b) || fmt.Sprint(ae) != fmt.Sprint(be) {
				t.Fatal("controller parsing changed")
			}
			seen := map[string]bool{}
			for _, name := range a {
				if seen[name] || !identifier(name) {
					t.Fatal("invalid or duplicate controller")
				}
				seen[name] = true
			}
			if len(a) > defaultRecords {
				t.Fatal("controller record budget exceeded")
			}
			if readErr != nil && !errors.Is(ae, ErrIncomplete) {
				t.Fatal("incomplete controllers reported as complete")
			}
			mode, err := parseSELinux(data)
			again, againErr := parseSELinux(data)
			if mode != again || fmt.Sprint(err) != fmt.Sprint(againErr) {
				t.Fatal("SELinux parsing changed")
			}
			if err == nil && mode != "0" && mode != "1" {
				t.Fatal("invalid SELinux mode")
			}
			if !bytes.Equal(before, input) {
				t.Fatal("input mutated")
			}
		})
	})
}

func FuzzCgroupNames(f *testing.F) {
	for _, seed := range []string{"cpu.max", "cpu..max", "../x", "cpu\x00max", "cgroup.controllers", "CPU.max"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		fuzzutil.Default(fuzzutil.PathInput).Run(t, input, func() {
			name := string(input)
			a, b := validateCgroupFilename(name), validateCgroupFilename(name)
			if fmt.Sprint(a) != fmt.Sprint(b) {
				t.Fatal("name validation changed")
			}
			if a == nil && (ValidateSubpath(name) != nil || bytes.ContainsAny(input, "/\\\x00 \n")) {
				t.Fatal("unsafe cgroup name accepted")
			}
		})
	})

}
