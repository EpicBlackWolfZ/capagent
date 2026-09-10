// Package fuzzutil supplies development-only callback envelopes. Production
// packages must never import it. It performs no host I/O or fault injection.
package fuzzutil

import (
	"flag"
	"runtime"
	"sync"
	"testing"
	"time"
)

const (
	AllocationLimit  = 32 << 20
	IterationTimeout = 10 * time.Second
	ParserInput      = 64 << 10
	PathInput        = 4 << 10
	PolicyInput      = 8 << 10
	GraphInput       = 4 << 10
)

var deterministic = flag.Bool("fuzz-deterministic", true, "require input-derived deterministic fuzz behavior")
var callbackMu sync.Mutex

// Envelope bounds callback input, cumulative allocation and elapsed time. The
// allocation ceiling is a regression assertion, not a kernel memory limit.
// Allocation includes fixture generation and both replays, excluding cleanup.
type Envelope struct {
	InputBytes     int
	AllocatedBytes uint64
	Timeout        time.Duration
}

func Default(inputBytes int) Envelope {
	return Envelope{InputBytes: inputBytes, AllocatedBytes: AllocationLimit, Timeout: IterationTimeout}
}

// Run rejects oversized inputs before fixture construction. Callbacks are serial
// within a test process so their allocation measurements do not overlap. Callers
// must not start unrelated background work or use t.Parallel inside a callback.
// The fatal watchdog also bounds corpus execution outside native fuzz workers.
func (e Envelope) Run(t *testing.T, input []byte, fn func()) bool {
	t.Helper()
	if !*deterministic || e.InputBytes <= 0 || e.AllocatedBytes == 0 || e.Timeout <= 0 {
		t.Fatal("fuzz envelope: invalid limits or deterministic mode disabled")
	}
	if len(input) > e.InputBytes {
		return false
	}
	callbackMu.Lock()
	defer callbackMu.Unlock()
	timer := time.AfterFunc(e.Timeout, func() { panic("fuzz envelope: callback watchdog expired") })
	defer timer.Stop()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	if after.TotalAlloc-before.TotalAlloc > e.AllocatedBytes {
		t.Fatalf("fuzz envelope: allocated %d bytes, limit %d", after.TotalAlloc-before.TotalAlloc, e.AllocatedBytes)
	}
	return true
}
