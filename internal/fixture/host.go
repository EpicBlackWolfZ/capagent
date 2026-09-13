package fixture

import (
	"errors"
	"strings"

	"github.com/EpicBlackWolfZ/capagent/internal/config"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/requirement"
)

const hostProbe = "host"
const contextProbe = "context"
const hostVersionMillis = 2000

type MountPolicy struct {
	NoSUID bool `json:"nosuid"`
	NoExec bool `json:"noexec"`
}

type HostCalls struct {
	MountPolicies   map[string]MountPolicy `json:"mount_policies,omitempty"`
	Uname           *platform.UnameInfo    `json:"uname,omitempty"`
	UnameFailure    string                 `json:"uname_failure,omitempty"`
	NoNewPrivileges *bool                  `json:"no_new_privileges,omitempty"`
	SecurityFailure string                 `json:"security_failure,omitempty"`
	Filesystems     map[string]int64       `json:"filesystems,omitempty"`
}

func validateHost(d *Document) error {
	if d.Runtime != "" || d.Endpoint != "" || len(d.Commands) > 1 || d.Host == nil {
		return errors.New("invalid host fixture")
	}
	if len(d.Requirement) > 0 && string(d.Requirement) != "null" {
		return errors.New("host fixture cannot evaluate a requirement")
	}
	if d.Probe == contextProbe && (d.Context.Identity.Execution == nil || len(d.Commands) != 0) {
		return errors.New("context fixture requires execution authority and no commands")
	}
	if err := validateMounts(d.Host); err != nil {
		return err
	}
	for _, code := range []string{d.Host.UnameFailure, d.Host.SecurityFailure} {
		if _, err := failure(code); err != nil {
			return err
		}
	}
	for _, c := range d.Commands {
		if (c.Path != "/usr/bin/systemctl" && c.Path != "/bin/systemctl") || len(c.Args) != 1 || c.Args[0] != "--version" ||
			len(c.Environment) != 0 || c.Directory != "/" || c.TimeoutMillis != hostVersionMillis {
			return errors.New("host fixture command not allowlisted")
		}
	}
	return nil
}
func parseRequirement(d *Document) (*requirement.Node, error) {
	if d.Probe == hostProbe || d.Probe == contextProbe {
		return nil, nil
	}
	return config.ParseRequirement(d.Requirement)
}
func hostSnapshot(d *Document) platform.HostSnapshot {
	snapshot := platform.HostSnapshot{UnameError: platform.ErrIncomplete, SecurityError: platform.ErrIncomplete}
	if d.Host.Uname != nil {
		snapshot.UnameResult = *d.Host.Uname
		snapshot.UnameError = nil
	}
	if d.Host.NoNewPrivileges != nil {
		snapshot.SecurityResult = *d.Host.NoNewPrivileges
		snapshot.SecurityError = nil
	}
	if d.Host.UnameFailure != "" {
		snapshot.UnameError, _ = failure(d.Host.UnameFailure)
	}
	if d.Host.SecurityFailure != "" {
		snapshot.SecurityError, _ = failure(d.Host.SecurityFailure)
	}
	return snapshot
}
func hostFilesystems(d *Document) map[string]platform.FilesystemInfo {
	out := make(map[string]platform.FilesystemInfo, len(d.Host.Filesystems))
	for mount, kind := range d.Host.Filesystems {
		out[mount] = platform.FilesystemInfo{Type: kind}
		if policy, ok := d.Host.MountPolicies[mount]; ok {
			out[mount] = platform.FilesystemInfo{Type: kind, FlagsKnown: true, NoExec: policy.NoExec, NoSUID: policy.NoSUID}
		}
	}
	return out
}

func validateMounts(host *HostCalls) error {
	if len(host.Filesystems) > maxFiles {
		return errors.New("too many fixture filesystems")
	}
	for mount := range host.MountPolicies {
		if _, ok := host.Filesystems[mount]; !ok {
			return errors.New("mount policy lacks filesystem")
		}
	}
	for mount := range host.Filesystems {
		if !strings.HasPrefix(mount, "/") || mount != "/" && platform.ValidateSubpath(strings.TrimPrefix(mount, "/")) != nil {
			return errors.New("invalid fixture mount")
		}
	}
	return nil
}
