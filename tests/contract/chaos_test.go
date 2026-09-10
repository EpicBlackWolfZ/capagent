package contract_test

import (
	"context"
	"crypto/sha256"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"reflect"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
)

var errChaosFault = errors.New("synthetic fault")

type scriptedProbe struct {
	id   string
	deps []string
	run  func(context.Context, platform.Environment) (model.Observation, error)
}

func (p *scriptedProbe) ID() string             { return p.id }
func (p *scriptedProbe) Dependencies() []string { return p.deps }
func (p *scriptedProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	return p.run(ctx, env)
}

type faultScenario struct {
	Count, Width, FaultAt int
	Workers               int
	Order                 []int
	Profile               string
	Cancel                bool
	Abort                 bool
}

// Entire frontiers fit within the worker budget. Later frontiers depend on all
// preceding-frontier nodes, so the coordinator can await starts without guessing
// which goroutine the OS will run first. At most four edges per node are generated.
func chaosScenario(cfg chaosConfig, iteration int) faultScenario {
	const maxWidth = 4
	rng := rand.New(rand.NewPCG(cfg.Seed, uint64(iteration)))
	s := faultScenario{Count: cfg.Probes, Width: min(maxWidth, cfg.Concurrency, cfg.Probes),
		Workers: cfg.Concurrency, FaultAt: rng.IntN(cfg.Probes)}
	profiles := []string{"filesystem", "command", "scheduler", profileRegistry}
	s.Profile = profiles[iteration%len(profiles)]
	s.Cancel = s.Profile == "scheduler" && (iteration/len(profiles))%2 == 1
	for start := 0; start < s.Count; start += s.Width {
		for _, index := range rng.Perm(min(s.Width, s.Count-start)) {
			s.Order = append(s.Order, start+index)
		}
	}
	return s
}

type normalizedProbe struct {
	ID          string
	Status      probe.ProbeStatus
	Observation model.Observation
	Error       string
}
type scenarioResult struct {
	Results     []normalizedProbe
	Checkpoints []int
	Peak        int32
}

func TestChaosCampaign(t *testing.T) {
	t.Parallel()
	cfg, err := parseChaosConfig(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	requireHardeningChild(t, cfg.Duration+helperParentTimeout, "campaign")
}

func runChaosCampaign(t *testing.T) {
	cfg, err := parseChaosConfig(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.AfterFunc(cfg.Duration, func() { fmt.Fprintln(os.Stderr, "chaos campaign watchdog"); os.Exit(2) })
	defer deadline.Stop()
	start := time.Now()
	hardeningEvent(t, map[string]any{eventKind: "campaign_start", "config": cfg, "duration": cfg.Duration.String()})
	for iteration := 0; iteration < cfg.Iterations; iteration++ {
		s := chaosScenario(cfg, iteration)
		func() {
			var progress atomic.Int64
			progress.Store(-1)
			stop := scenarioWatchdog(fmt.Sprintf("seed=%d iteration=%d profile=%s", cfg.Seed, iteration, s.Profile),
				func() string {
					return fmt.Sprintf("last coordinator checkpoint=%d fault_at=%d width=%d", progress.Load(), s.FaultAt, s.Width)
				})
			defer stop()
			hardeningEvent(t, map[string]any{eventKind: "chaos_begin", "iteration": iteration, "seed": cfg.Seed,
				"profile": s.Profile, "fault_at": s.FaultAt, "width": s.Width})
			a, b := executeChaosScenario(t, s, &progress), executeChaosScenario(t, s, &progress)
			if !reflect.DeepEqual(a, b) {
				t.Fatalf("seed=%d iteration=%d scenario=%+v replay mismatch: %+v / %+v", cfg.Seed, iteration, s, a, b)
			}
			data, err := json.Marshal(a)
			if err != nil {
				t.Fatal(err)
			}
			hardeningEvent(t, map[string]any{eventKind: "chaos_case", "seed": cfg.Seed, "iteration": iteration,
				"profile": s.Profile, "digest": fmt.Sprintf("%x", sha256.Sum256(data)), "checkpoints": len(a.Checkpoints),
				"peak_workers": a.Peak, "probes": s.Count, "concurrency": cfg.Concurrency})
		}()
	}
	hardeningEvent(t, map[string]any{eventKind: "campaign_complete", "iterations": cfg.Iterations, "duration_ns": int64(time.Since(start))})
}

func executeChaosScenario(t *testing.T, s faultScenario, progress ...*atomic.Int64) scenarioResult {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	registry := probe.NewRegistry()
	if s.Profile == profileRegistry {
		checkRegistryFault(t, s.FaultAt)
	}
	mem := platform.NewMemPlatformReader()
	mem.AddDir("/", 0o755)
	fakeRunner := platform.NewFakeCommandRunner()
	env := platform.NewTestEnvironment(mem, fakeRunner)
	started := make(chan int, s.Count)
	release := make([]chan struct{}, s.Count)
	completed := make([]atomic.Bool, s.Count)
	var active, peak atomic.Int32
	for id := 0; id < s.Count; id++ {
		release[id] = make(chan struct{})
		p := &scriptedProbe{id: strconv.Itoa(id)}
		first := id / s.Width * s.Width
		if first > 0 {
			for parent := max(0, first-s.Width); parent < first; parent++ {
				p.deps = append(p.deps, strconv.Itoa(parent))
			}
		}
		mem.AddFile("/"+p.id, []byte("observed"), 0o600)
		if id == s.FaultAt && s.Profile == "filesystem" {
			mem.AddError("/"+p.id, errChaosFault)
		}
		commandSpec := platform.CommandSpec{Path: "/synthetic/probe", Args: []string{p.id}}
		var commandErr error
		if id == s.FaultAt && s.Profile == "command" {
			commandErr = errChaosFault
		}
		if err := fakeRunner.RegisterWithError(commandSpec, platform.ExecResult{Stdout: []byte(partialSummary)}, commandErr); err != nil {
			t.Fatal(err)
		}
		p.run = func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			current := active.Add(1)
			defer active.Add(-1)
			for previous := peak.Load(); current > previous; previous = peak.Load() {
				if peak.CompareAndSwap(previous, current) {
					break
				}
			}
			for _, dep := range p.deps {
				parent, _ := strconv.Atoi(dep)
				if !completed[parent].Load() {
					t.Errorf("%d before dependency %d", id, parent)
				}
			}
			started <- id
			select {
			case <-release[id]:
			case <-ctx.Done():
				return model.Observation{Summary: partialSummary}, ctx.Err()
			}
			obs := model.Observation{Summary: partialSummary}
			if s.Profile == "command" {
				result, err := env.Runner().Run(ctx, commandSpec)
				if err != nil {
					return model.Observation{Summary: string(result.Stdout)}, err
				}
			}
			if id == s.FaultAt && s.Profile != profileRegistry {
				if s.Profile == "filesystem" {
					_, err := env.Reader().ReadFile(ctx, "/"+p.id)
					return obs, err
				}
				return obs, errChaosFault
			}
			completed[id].Store(true)
			return model.Observation{Summary: "observed"}, nil
		}
		if err := registry.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	orchestrator, err := probe.NewOrchestrator(registry, probe.WithMaxConcurrency(s.Workers))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan []probe.ProbeResult, 1)
	go func() { done <- orchestrator.Run(ctx, env) }()
	var joined bool
	defer func() {
		cancel()
		if !joined {
			<-done
		}
		if active.Load() != 0 {
			t.Error("probe workers survived join")
		}
	}()
	var checkpoints []int
	for first := 0; first < s.Count; first += s.Width {
		for _, p := range progress {
			p.Store(int64(first))
		}
		count := min(s.Width, s.Count-first)
		entered := make([]int, 0, count)
		for range count {
			entered = append(entered, <-started)
		}
		slices.Sort(entered)
		for offset, id := range entered {
			if id != first+offset {
				t.Fatalf("unexpected frontier: %v at %d", entered, first)
			}
		}
		faultFrontier := s.FaultAt >= first && s.FaultAt < first+count && s.Profile != profileRegistry
		if s.Abort {
			t.Fatal("intentional assertion with workers blocked")
		}
		if faultFrontier && s.Cancel {
			cancel()
			checkpoints = append(checkpoints, -1)
			break
		}
		for _, id := range s.Order[first : first+count] {
			close(release[id])
			checkpoints = append(checkpoints, id)
		}
		if faultFrontier {
			break
		}
	}
	results := <-done
	joined = true
	if len(results) != s.Count || peak.Load() > int32(s.Width) {
		t.Fatalf("results/workers: %d/%d", len(results), peak.Load())
	}
	out := scenarioResult{Checkpoints: checkpoints, Peak: peak.Load()}
	faultFrontier := s.FaultAt / s.Width
	for id, result := range results {
		want := probe.ProbeSucceeded
		if s.Profile != profileRegistry {
			switch {
			case id/s.Width > faultFrontier:
				want = probe.ProbeSkipped
			case id/s.Width == faultFrontier && s.Cancel:
				want = probe.ProbeCancelled
			case id == s.FaultAt:
				want = probe.ProbeFailed
			}
		}
		if result.ProbeID != strconv.Itoa(id) || result.Status != want {
			t.Fatalf("scenario=%+v probe=%d result=%+v want=%v", s, id, result, want)
		}
		if (want == probe.ProbeFailed || want == probe.ProbeCancelled) && result.Observation.Summary != partialSummary {
			t.Fatal("lost partial facts")
		}
		class := ""
		switch {
		case errors.Is(result.Err, context.Canceled):
			class = "cancelled"
		case errors.Is(result.Err, probe.ErrDependencyFailed):
			class = "dependency"
		case errors.Is(result.Err, errChaosFault):
			class = "fault"
		case result.Err != nil:
			t.Fatalf("unexpected error: %v", result.Err)
		}
		out.Results = append(out.Results, normalizedProbe{result.ProbeID, result.Status, result.Observation, class})
	}
	return out
}
