package host

import (
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
	"time"
)

func Probes(now func() time.Time) []probe.Probe {
	return []probe.Probe{OSReleaseProbe{Now: now}, KernelProbe{Now: now}, SystemdProbe{Now: now}}
}
