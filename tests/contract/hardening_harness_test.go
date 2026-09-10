package contract_test

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const (
	missingFixtureName    = "missing"
	helperOutputLimit     = 4 << 20
	helperScenarioTimeout = 10 * time.Second
	helperParentTimeout   = 15 * time.Second
	helperDrainTimeout    = time.Second
	hardeningMarker       = "capagent-hardening"
	hardeningEventPrefix  = "HARDENING "
	chaosMaxCount         = 4096
	chaosMaxWorkers       = 64
	chaosMaxDuration      = 20 * time.Minute
	profileRegistry       = "registry"
	partialSummary        = "partial"
	eventKind             = "kind"
)

type chaosConfig struct {
	Seed                            uint64
	Iterations, Probes, Concurrency int
	Duration                        time.Duration `json:"-"`
}

func defaultChaosConfig() chaosConfig {
	const iterations, probes, workers = 32, 64, 4
	return chaosConfig{Seed: 1, Iterations: iterations, Probes: probes, Concurrency: workers, Duration: time.Minute}
}

func parseChaosConfig(get func(string) string) (chaosConfig, error) {
	cfg := defaultChaosConfig()
	if raw := get("CHAOS_SEED"); raw != "" {
		seed, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return cfg, fmt.Errorf("CHAOS_SEED: %w", err)
		}
		cfg.Seed = seed
	}
	for _, field := range []struct {
		name  string
		value *int
		max   int
	}{
		{"CHAOS_ITERATIONS", &cfg.Iterations, chaosMaxCount},
		{"CHAOS_PROBES", &cfg.Probes, chaosMaxCount},
		{"CHAOS_CONCURRENCY", &cfg.Concurrency, chaosMaxWorkers},
	} {
		if raw := get(field.name); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > field.max {
				return cfg, fmt.Errorf("%s must be in [1,%d]", field.name, field.max)
			}
			*field.value = n
		}
	}
	if raw := get("CHAOS_DURATION"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 || d > chaosMaxDuration {
			return cfg, errors.New("CHAOS_DURATION must be positive and <=20m")
		}
		cfg.Duration = d
	}
	return cfg, nil
}

type helperCapture struct {
	mu       sync.Mutex
	data     bytes.Buffer
	overflow bool
}

func (b *helperCapture) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := min(len(p), helperOutputLimit-b.data.Len())
	b.data.Write(p[:n])
	b.overflow = b.overflow || n != len(p)
	return len(p), nil
}

type helperOutcome struct {
	Output []byte
	Err    error
}

// The parent owns the process group until Wait completes; no unbounded output
// capture or detached watchdog goroutine survives a failing assertion.
func hardeningChild(ctx context.Context, budget time.Duration, args ...string) helperOutcome {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	commandArgs := append([]string{"-test.run=^TestHardeningHelper$", "--", hardeningMarker}, args...)
	cmd := exec.CommandContext(ctx, os.Args[0], commandArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = helperDrainTimeout
	lifetime, err := cmd.StdinPipe()
	if err != nil {
		return helperOutcome{Err: err}
	}
	defer lifetime.Close()
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C", "GORACE=atexit_sleep_ms=0"}
	for _, key := range []string{"CHAOS_SEED", "CHAOS_ITERATIONS", "CHAOS_PROBES", "CHAOS_CONCURRENCY", "CHAOS_DURATION", "GOMAXPROCS"} {
		if value, ok := os.LookupEnv(key); ok {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	var capture helperCapture
	cmd.Stdout, cmd.Stderr = &capture, &capture
	err = cmd.Run()
	if ctx.Err() != nil {
		err = errors.Join(err, ctx.Err())
	}
	if capture.overflow {
		err = errors.Join(err, errors.New("helper diagnostic budget exceeded"))
	}
	return helperOutcome{Output: capture.data.Bytes(), Err: err}
}

func requireHardeningChild(t *testing.T, budget time.Duration, args ...string) {
	t.Helper()
	got := hardeningChild(t.Context(), budget, args...)
	if len(got.Output) > 0 {
		t.Logf("%s", got.Output)
	}
	if got.Err != nil {
		t.Fatalf("helper %v: %v", args, got.Err)
	}
}

func hardeningEvent(t *testing.T, event map[string]any) {
	t.Helper()
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(os.Stdout, "%s%s\n", hardeningEventPrefix, data); err != nil {
		t.Fatal(err)
	}
}

// A process-level watchdog also bounds cleanup paths that regress to blocking.
// Only isolated helpers call this; an expired watchdog is always a test failure.
func scenarioWatchdog(label string, progress ...func() string) func() {
	timer := time.AfterFunc(helperScenarioTimeout, func() {
		fmt.Fprintf(os.Stderr, "hardening scenario watchdog: %s\n", label)
		for _, describe := range progress {
			fmt.Fprintln(os.Stderr, describe())
		}
		os.Exit(2)
	})
	return func() { timer.Stop() }
}

func TestHardeningHelper(t *testing.T) {
	index := -1
	for i, arg := range os.Args {
		if arg == hardeningMarker {
			index = i + 1
			break
		}
	}
	if index < 0 {
		return
	}
	args := os.Args[index:]
	if len(args) == 0 {
		t.Fatal("missing helper mode")
	}
	// EOF on the parent-owned lifetime pipe terminates a helper even if the
	// parent crashes while the helper is in a separate process group.
	if args[0] != "payload" {
		go func() {
			var buf [1]byte
			if _, err := os.Stdin.Read(buf[:]); err != nil {
				os.Exit(2)
			}
		}()
	}
	switch args[0] {
	case "hang":
		time.Sleep(time.Hour)
	case "fail":
		t.Fatal("intentional helper failure")
	case "overflow":
		fmt.Print(strings.Repeat("x", helperOutputLimit+1))
	case "cleanup-failure":
		s := chaosScenario(defaultChaosConfig(), 0)
		s.Abort = true
		t.Cleanup(func() { assertNoProbeWorkers(t); fmt.Fprintln(os.Stdout, "cleanup completed") })
		executeChaosScenario(t, s)
	case "campaign":
		runChaosCampaign(t)
	case "filesystem":
		runChaosFilesystem(t)
	case profileRegistry:
		runChaosRegistry(t)
	case "graph":
		if len(args) != 3 {
			t.Fatal("graph arguments")
		}
		runResourceGraph(t, args[1], args[2])
	case "commands":
		runResourceCommands(t)
	case "payload":
		runCommandPayload(t, args[1:])
	case "ownership":
		runResourceOwnership(t)
	default:
		t.Fatalf("unknown helper mode %q", args[0])
	}
}
