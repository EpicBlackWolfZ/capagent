package host

import (
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"testing"
	"time"
)

func TestOSReleaseContract(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, text, id, version string
		partial                 bool
	}{
		{"quoted", "ID=debian\nVERSION_ID=\"12\"\nNAME='Debian Linux'\n", "debian", "12", false},
		{"duplicate", "ID=old\nID=fedora\n", "fedora", "", false},
		{"malformed", "ID=alpine\nbroken\n", "alpine", "", true},
		{"empty", "", "", "", true},
		{"no expansion", "ID=\"$(secret)\"\n", "", "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			mem.AddDir("/etc", 0o755)
			mem.AddFile("/etc/os-release", []byte(tt.text), 0o644)
			files := platform.NewScopedMemReader("/", mem)
			defer files.Close()
			env := platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).
				WithScope(model.EvaluationScope{RunID: "run", ContextID: hostTestContext})
			obs, _ := (OSReleaseProbe{Now: func() time.Time { return time.Unix(1, 0) }}).Run(t.Context(), env)
			if obs.Host.OS.ID != tt.id || obs.Host.OS.VersionID != tt.version || (obs.Completeness != model.Complete) != tt.partial {
				t.Fatalf("unexpected observation: %+v %+v", obs, obs.Host.OS)
			}
		})
	}
}

func TestKernelVersionContract(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		text  string
		valid bool
	}{
		{"5.14.0-427.el9.x86_64", true}, {"6.12.0-custom", true}, {"custom", false}, {"999999999999999.1.2", false},
	} {
		t.Run(tt.text, func(t *testing.T) {
			t.Parallel()
			if (parseKernelVersion(tt.text) != nil) != tt.valid {
				t.Fatal("version metadata mismatch")
			}
		})
	}
}
