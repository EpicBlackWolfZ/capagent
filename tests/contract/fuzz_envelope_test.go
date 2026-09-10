package contract_test

import (
	"context"
	"github.com/EpicBlackWolfZ/capagent/tests/fuzzutil"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

var fuzzHeldAllocation []byte

func TestFuzzEnvelopeFailures(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"hang", "allocate", "invalid", "nondeterministic"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestEnvelopeHelper$", "--", mode)
			if mode == "nondeterministic" {
				cmd.Args = append(cmd.Args[:2], "-fuzz-deterministic=false", "--", mode)
			}
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil || err == nil || !strings.Contains(string(out), "fuzz envelope") {
				t.Fatalf("%v: %s", err, out)
			}
		})
	}
}

func TestEnvelopeHelper(t *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	e := fuzzutil.Default(8)
	const tinyTimeout = 20 * time.Millisecond
	mode := os.Args[len(os.Args)-1]
	switch mode {
	case "hang":
		e.Timeout = tinyTimeout
	case "allocate":
		e.AllocatedBytes = 1
	case "invalid":
		e.InputBytes = 0
	}
	e.Run(t, nil, func() {
		if mode == "hang" {
			select {}
		}
		if mode == "allocate" {
			fuzzHeldAllocation = make([]byte, 4096)
		}
	})
}
