package contract_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"syscall"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
)

func TestChaosFilesystem(t *testing.T) {
	t.Parallel()
	requireHardeningChild(t, helperParentTimeout, "filesystem")
}

func runChaosFilesystem(t *testing.T) {
	for _, cause := range []error{syscall.ENOENT, syscall.EACCES, syscall.EIO, syscall.ELOOP, syscall.ENOTDIR} {
		t.Run(cause.Error(), func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			mem.AddDir("/", 0o755)
			mem.AddError("/fault", cause)
			scoped := platform.NewScopedMemReader("/", mem)
			defer scoped.Close()
			for _, operation := range []struct {
				name string
				call func() error
			}{
				{"read", func() error { _, err := scoped.ReadFile(t.Context(), "fault"); return err }},
				{"procfs", func() error {
					_, err := platform.NewProcfsReader(scoped).ReadProcFile(t.Context(), "fault")
					return err
				}},
				{"sysfs", func() error { _, err := platform.NewSysfsReader(scoped).ReadSysFile(t.Context(), "fault"); return err }},
				{"stat", func() error { _, err := scoped.Stat("fault"); return err }},
				{"directory", func() error { _, err := scoped.ReadDir(t.Context(), "fault"); return err }},
				{"link", func() error { _, err := scoped.Readlink("fault"); return err }},
				{"capabilities", func() error { _, err := scoped.FileCapabilities(t.Context(), "fault"); return err }},
			} {
				if err := operation.call(); !errors.Is(err, cause) {
					t.Errorf("%s: lost %v: %v", operation.name, cause, err)
				}
			}
		})
	}
	// Sequential scripted responses expose read-to-read changes without racing
	// fixture mutation against an operation that does not promise that contract.
	t.Run("changing partial records", func(t *testing.T) {
		t.Parallel()
		mem := platform.NewMemPlatformReader()
		mem.AddDir("/", 0o755)
		reader := &scriptedRead{ScopedReader: platform.NewScopedMemReader("/", mem), responses: []readResponse{
			{[]byte("ext4\nx"), syscall.EIO}, {[]byte("xfs\n"), nil},
		}}
		defer reader.Close()
		proc := platform.NewProcfsReader(reader)
		a, err := proc.Filesystems(t.Context())
		if len(a) != 1 || !errors.Is(err, syscall.EIO) || !errors.Is(err, platform.ErrIncomplete) {
			t.Fatalf("partial: %v %v", a, err)
		}
		b, err := proc.Filesystems(t.Context())
		if len(b) != 1 || err != nil || reflect.DeepEqual(a, b) || reader.calls != 2 {
			t.Fatalf("changed: %v %v", b, err)
		}
	})
}

type readResponse struct {
	data []byte
	err  error
}
type scriptedRead struct {
	platform.ScopedReader
	responses []readResponse
	calls     int
}

func (r *scriptedRead) ReadFile(context.Context, string) ([]byte, error) {
	if r.calls == len(r.responses) {
		return nil, errors.New("unexpected scripted read")
	}
	out := r.responses[r.calls]
	r.calls++
	return append([]byte(nil), out.data...), out.err
}

func checkRegistryFault(t *testing.T, choice int) {
	t.Helper()
	types := []string{missingFixtureName, "self", "cycle"}
	kind := types[choice%len(types)]
	r := probe.NewRegistry()
	a := &scriptedProbe{id: "a", deps: []string{missingFixtureName}}
	if kind == "self" {
		a.deps = []string{"a"}
	}
	if kind == "cycle" {
		a.deps = []string{"b"}
		if err := r.Register(&scriptedProbe{id: "b", deps: []string{"a"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Register(a); err != nil {
		t.Fatal(err)
	}
	first := r.Resolve()
	if first == nil {
		t.Fatal("malformed registry accepted")
	}
	if err := r.Resolve(); err != first {
		t.Fatal("failed retry changed result")
	}
	if _, err := r.ResolvedPlan(); err != first {
		t.Fatal("failed plan changed result")
	}
	if _, err := probe.NewOrchestrator(r); err != first {
		t.Fatal("failed constructor changed result")
	}
	if err := r.Register(&scriptedProbe{id: "late"}); !errors.Is(err, probe.ErrRegistryResolved) {
		t.Fatal("failed resolution did not seal registry")
	}
}

func TestChaosRegistry(t *testing.T) {
	t.Parallel()
	requireHardeningChild(t, helperParentTimeout, "registry")
}

func runChaosRegistry(t *testing.T) {
	stop := scenarioWatchdog("registry")
	defer stop()
	const repeats = 24
	for i := 0; i < repeats; i++ {
		checkRegistryFault(t, i)
		checkRegistryConstructionRace(t)
	}
	r := probe.NewRegistry()
	if err := r.Register(&scriptedProbe{id: "first", run: func(context.Context, platform.Environment) (model.Observation, error) {
		return model.Observation{}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(&scriptedProbe{id: "first"}); err == nil {
		t.Fatal("duplicate accepted")
	}
	// Readers compete with one resolver and duplicate writers. Outcomes may
	// differ by schedule; the allowed terminal state and plan cannot differ.
	start := make(chan struct{})
	var wg sync.WaitGroup
	const workers = 16
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range repeats {
				if err := r.Resolve(); err != nil {
					t.Error(err)
				}
				plan, err := r.ResolvedPlan()
				if err != nil || !reflect.DeepEqual(plan, []string{"first"}) {
					t.Errorf("plan %v %v", plan, err)
				}
				plan[0] = "mutated"
				if _, ok := r.Get("first"); !ok {
					t.Error("missing registered probe")
				}
				if len(r.All()) != 1 {
					t.Error("registry grew")
				}
				if err := r.Register(&scriptedProbe{id: "first"}); !errors.Is(err, probe.ErrRegistryResolved) {
					t.Errorf("terminal registration: %v", err)
				}
			}
		}()
	}
	close(start)
	wg.Wait()
}

func checkRegistryConstructionRace(t *testing.T) {
	t.Helper()
	r := probe.NewRegistry()
	if err := r.Register(&scriptedProbe{id: "base"}); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	const operations = 3
	outcomes := make([]error, operations)
	for index := 0; index < operations; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if index == operations-1 {
				outcomes[index] = r.Resolve()
				return
			}
			dependency := "base"
			if index == 1 {
				dependency = missingFixtureName
			}
			outcomes[index] = r.Register(&scriptedProbe{id: "child", deps: []string{dependency}})
		}()
	}
	close(start)
	wg.Wait()
	if outcomes[0] == nil && outcomes[1] == nil {
		t.Fatal("conflicting registrations both succeeded")
	}
	err := r.Resolve()
	if err != outcomes[operations-1] {
		t.Fatal("resolution changed after concurrent construction")
	}
	child, present := r.Get("child")
	invalid := present && child.Dependencies()[0] == missingFixtureName
	if (err != nil) != invalid {
		t.Fatal("resolution disagrees with the winning declaration")
	}
	if err := r.Register(&scriptedProbe{id: "late"}); !errors.Is(err, probe.ErrRegistryResolved) {
		t.Fatal("registry reopened after construction race")
	}
}

// Compile-time conformance catches accidental loss of an injection boundary.
var _ platform.ScopedReader = (*scriptedRead)(nil)
