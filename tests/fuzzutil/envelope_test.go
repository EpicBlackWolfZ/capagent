package fuzzutil

import (
	"bytes"
	"testing"
)

func TestEnvelope(t *testing.T) {
	t.Parallel()
	e := Default(8)
	if e.AllocatedBytes != AllocationLimit || e.Timeout != IterationTimeout {
		t.Fatal("incorrect defaults")
	}
	calls := 0
	if !e.Run(t, []byte("ok"), func() { calls++ }) || calls != 1 {
		t.Fatal("callback not run")
	}
	if e.Run(t, bytes.Repeat([]byte("x"), 9), func() { t.Fatal("oversize callback ran") }) {
		t.Fatal("oversize accepted")
	}
}
