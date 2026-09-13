package contract_test

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/host"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
	"github.com/EpicBlackWolfZ/capagent/tests/fuzzutil"
)

func FuzzOSRelease(f *testing.F) {
	f.Add([]byte("ID=debian\nVERSION_ID=12\n"))
	f.Fuzz(func(t *testing.T, input []byte) {
		fuzzutil.Default(fuzzutil.ParserInput).Run(t, input, func() {
			fuzzHostMeasurements(t, input, []probe.Probe{host.OSReleaseProbe{Now: hostFuzzClock}})
		})
	})
}
func FuzzHostNetwork(f *testing.F) {
	f.Add([]byte("nameserver 192.0.2.53\noptions ndots:2\n"))
	f.Fuzz(func(t *testing.T, input []byte) {
		fuzzutil.Default(fuzzutil.ParserInput).Run(t, input, func() {
			fuzzHostMeasurements(t, input, []probe.Probe{host.ResolverProbe{Now: hostFuzzClock}, host.NetworkProbe{Now: hostFuzzClock}})
		})
	})
}
func hostFuzzClock() time.Time { return time.Unix(1, 0) }
func fuzzHostMeasurements(t *testing.T, input []byte, probes []probe.Probe) {
	t.Helper()
	before := bytes.Clone(input)
	mem := platform.NewMemPlatformReader()
	for _, dir := range []string{"/etc", "/proc", "/proc/self", "/proc/self/net"} {
		mem.AddDir(dir, 0o755)
	}
	for _, path := range []string{"/etc/os-release", "/etc/resolv.conf", "/proc/self/net/protocols"} {
		mem.AddFile(path, input, 0o644)
	}
	files := platform.NewScopedMemReader("/", mem)
	defer files.Close()
	env := platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(model.EvaluationScope{RunID: "fuzz", ContextID: "current"})
	for _, p := range probes {
		a, ae := p.Run(t.Context(), env)
		b, be := p.Run(t.Context(), env)
		if !reflect.DeepEqual(a, b) || (ae == nil) != (be == nil) || !bytes.Equal(input, before) {
			t.Fatal("host parsing changed inputs or replay")
		}
	}
}
