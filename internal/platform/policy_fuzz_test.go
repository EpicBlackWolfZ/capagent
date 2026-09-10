package platform

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/tests/fuzzutil"
)

const fuzzPolicyFields = 16

func FuzzCommandSpec(f *testing.F) {
	for _, seed := range []string{"/bin/tool\n/\narg", "relative", "/bin/x\x00\n/", "/x\nrelative", "/x\n/\na;echo secret"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		fuzzutil.Default(fuzzutil.PolicyInput).Run(t, input, func() {
			parts := strings.SplitN(string(input), "\n", fuzzPolicyFields+2)
			spec := CommandSpec{Path: parts[0]}
			if len(parts) > 1 {
				spec.Dir = parts[1]
			}
			if len(parts) > 2 {
				spec.Args = parts[2:]
			}
			if len(input) > 0 {
				const timeoutModes = 3
				spec.Timeout = time.Duration(int(input[len(input)-1])%timeoutModes - 1)
			}
			original := cloneCommandSpec(spec)
			a, ae := spec.normalized()
			b, be := spec.normalized()
			if !reflect.DeepEqual(a, b) || fmt.Sprint(ae) != fmt.Sprint(be) {
				t.Fatal("normalization changed")
			}
			if !reflect.DeepEqual(spec, original) {
				t.Fatal("normalization mutated specification")
			}
			if ae != nil {
				if !errors.Is(ae, ErrInvalidCommandSpec) {
					t.Fatal("untyped invalid specification")
				}
				return
			}
			if a.Dir == "" || a.Timeout < 0 || !strings.HasPrefix(a.Path, "/") {
				t.Fatal("invalid normalized authority")
			}
			if !reflect.DeepEqual(spec.Args, a.Args) {
				t.Fatal("literal arguments changed")
			}
			fake := NewFakeCommandRunner()
			result := ExecResult{Stdout: []byte("synthetic")}
			if err := fake.Register(spec, result); err != nil {
				t.Fatal(err)
			}
			got, err := fake.Run(t.Context(), spec)
			if err != nil || !bytes.Equal(got.Stdout, result.Stdout) {
				t.Fatal("fake policy mismatch")
			}
			if len(a.Args) > 0 {
				a.Args[0] = "mutated"
			}
			if !reflect.DeepEqual(spec, original) {
				t.Fatal("normalized data aliases caller")
			}
		})
	})
}

func FuzzEnvPolicy(f *testing.F) {
	for _, seed := range []string{"", "X=value", "PATH=/fixture\nLC_ALL=C", "bad-name=value", "X=\x00", "EMPTY="} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		fuzzutil.Default(fuzzutil.PolicyInput).Run(t, input, func() {
			overrides := map[string]string{}
			for _, line := range strings.SplitN(string(input), "\n", fuzzPolicyFields) {
				name, value, _ := strings.Cut(line, "=")
				overrides[name] = value
			}
			// Only this synthetic, process-fixed key is eligible for named capture.
			inherit := []string{"CAPAGENT_FUZZ_FIXED"}
			a, ae := NewEnvPolicy(inherit, overrides)
			b, be := NewEnvPolicy(inherit, overrides)
			if !reflect.DeepEqual(a, b) || fmt.Sprint(ae) != fmt.Sprint(be) {
				t.Fatal("environment policy changed")
			}
			if ae != nil {
				if !errors.Is(ae, ErrInvalidEnvPolicy) {
					t.Fatal("untyped environment error")
				}
				return
			}
			original := a.Variables()
			variables := a.Variables()
			for i, value := range variables {
				key, val, ok := strings.Cut(value, "=")
				if !ok || !validEnvName(key) || strings.ContainsRune(val, 0) {
					t.Fatal("invalid environment entry")
				}
				if i > 0 && variables[i-1] >= value {
					t.Fatal("environment is not canonical")
				}
			}
			overrides["X"] = "mutated"
			variables[0] = "mutated"
			if !reflect.DeepEqual(original, a.Variables()) {
				t.Fatal("policy retains mutable aliases")
			}
			if fmt.Sprintf("%#v", a) != "EnvPolicy[redacted]" {
				t.Fatal("environment diagnostics expose data")
			}
		})
	})
}

func FuzzBoundedBuffer(f *testing.F) {
	for _, seed := range [][]byte{{}, {0}, {1, 2, 3}, bytes.Repeat([]byte{255}, fuzzutil.PathInput)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		fuzzutil.Default(fuzzutil.ParserInput).Run(t, input, func() {
			limit, chunk := 0, 1
			if len(input) > 0 {
				limit = int(input[0]) * fuzzPolicyFields
				chunk += int(input[len(input)-1])
			}
			b := newBoundedBuffer(limit)
			for offset := 0; offset < len(input); offset += chunk {
				data := input[offset:min(len(input), offset+chunk)]
				if n, err := b.Write(data); n != len(data) || err != nil {
					t.Fatal("write contract violated")
				}
				if cap(b.data) > limit {
					t.Fatal("backing allocation exceeds capacity")
				}
			}
			whole := newBoundedBuffer(limit)
			whole.Write(input)
			want := input[:min(len(input), limit)]
			if !bytes.Equal(b.Bytes(), want) || !bytes.Equal(b.Bytes(), whole.Bytes()) || b.Truncated() != (len(input) > limit) {
				t.Fatal("retained prefix or truncation changed with chunking")
			}
			copyResult := b.Bytes()
			if len(copyResult) > 0 {
				copyResult[0] ^= 1
			}
			if !bytes.Equal(b.Bytes(), want) {
				t.Fatal("result aliases buffer")
			}
		})
	})

}
