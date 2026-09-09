package probe

import (
	"fmt"
	"runtime"
	"testing"
)

func TestOrchestratorDefaultWorkers(t *testing.T) {
	t.Parallel()
	const safetyCap = 8
	orchestrator, err := NewOrchestrator(NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if want := min(runtime.NumCPU(), safetyCap); orchestrator.maxConcurrency != want {
		t.Errorf("default workers=%d, want %d", orchestrator.maxConcurrency, want)
	}
	const explicitWorkers = 16
	orchestrator, err = NewOrchestrator(NewRegistry(), WithMaxConcurrency(explicitWorkers))
	if err != nil {
		t.Fatal(err)
	}
	if orchestrator.maxConcurrency != explicitWorkers {
		t.Fatal("explicit override was capped")
	}
}

func TestDefaultConcurrencyCPUCounts(t *testing.T) {
	t.Parallel()
	tests := []struct{ cpus, want int }{{-1, 1}, {0, 1}, {1, 1}, {4, 4}, {8, 8}, {64, 8}}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.cpus), func(t *testing.T) {
			t.Parallel()
			if got := defaultConcurrency(tt.cpus); got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}
