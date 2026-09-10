package contract_test

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestHardeningHarnessContracts(t *testing.T) {
	t.Parallel()
	t.Run("configuration", func(t *testing.T) {
		t.Parallel()
		cfg, err := parseChaosConfig(func(string) string { return "" })
		if err != nil || cfg.Seed != 1 || cfg.Iterations != 32 || cfg.Probes != 64 || cfg.Concurrency != 4 || cfg.Duration != time.Minute {
			t.Fatalf("defaults: %+v %v", cfg, err)
		}
		for key, bad := range map[string]string{
			"CHAOS_SEED": "-1", "CHAOS_ITERATIONS": "4097", "CHAOS_PROBES": "0",
			"CHAOS_CONCURRENCY": "65", "CHAOS_DURATION": "21m",
		} {
			if _, err := parseChaosConfig(func(k string) string {
				if k == key {
					return bad
				}
				return ""
			}); err == nil {
				t.Errorf("accepted %s=%s", key, bad)
			}
		}
	})
	t.Run("seeded schedule", func(t *testing.T) {
		t.Parallel()
		cfg := defaultChaosConfig()
		a, b := chaosScenario(cfg, 0), chaosScenario(cfg, 0)
		if !reflect.DeepEqual(a, b) {
			t.Fatal("same seed changed scenario")
		}
		if reflect.DeepEqual(a, chaosScenario(cfg, 1)) {
			t.Fatal("iteration did not change scenario")
		}
	})
	t.Run("bounded helper", func(t *testing.T) {
		t.Parallel()
		const deadline = 200 * time.Millisecond
		out := hardeningChild(context.Background(), deadline, "hang")
		if !errors.Is(out.Err, context.DeadlineExceeded) {
			t.Fatalf("watchdog: %+v", out)
		}
		out = hardeningChild(t.Context(), helperParentTimeout, "fail")
		if out.Err == nil {
			t.Fatal("helper failure swallowed")
		}
		out = hardeningChild(t.Context(), helperParentTimeout, "overflow")
		if out.Err == nil || len(out.Output) > helperOutputLimit {
			t.Fatal("unbounded or successful overflow")
		}
		out = hardeningChild(t.Context(), helperParentTimeout, "cleanup-failure")
		if out.Err == nil || !bytes.Contains(out.Output, []byte("cleanup completed")) {
			t.Fatalf("assertion cleanup failed: %s", out.Output)
		}
	})
}
