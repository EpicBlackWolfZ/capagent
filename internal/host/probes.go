package host

import (
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
	"time"
)

func Probes(now func() time.Time) []probe.Probe {
	return []probe.Probe{OSReleaseProbe{Now: now}, KernelProbe{Now: now}, SystemdProbe{Now: now},
		CgroupProbe{Now: now}, NamespaceProbe{Now: now}, SecurityProbe{Now: now},
		FilesystemProbe{Now: now}, NetworkProbe{Now: now}, ResolverProbe{Now: now}}
}
