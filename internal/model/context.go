package model

// UserIdentity models a Linux user credential set.
type UserIdentity struct {
	UID      uint32 `json:"uid"`
	GID      uint32 `json:"gid"`
	Username string `json:"username"`
	HomeDir  string `json:"home_dir"`
}

// SubIDRange models an allocated subuid or subgid contiguous range.
type SubIDRange struct {
	Start  uint32 `json:"start"`
	Length uint32 `json:"length"`
}

// IdentityContext captures execution identity information.
//
// It explicitly separates the executing process identity (Current) from the target
// identity (Target) under which container capabilities should be evaluated, supporting
// delegation modes such as root running checks on behalf of a non-root target user.
type IdentityContext struct {
	Current        UserIdentity `json:"current"`
	Target         UserIdentity `json:"target"`
	IsRootless     bool         `json:"is_rootless"`
	SubUIDRanges   []SubIDRange `json:"sub_uid_ranges,omitempty"`
	SubGIDRanges   []SubIDRange `json:"sub_gid_ranges,omitempty"`
	XDGRuntimeDir  string       `json:"xdg_runtime_dir,omitempty"`
	HasUserSystemd bool         `json:"has_user_systemd"`
	InContainer    bool         `json:"in_container"`
}

// HostContext captures observed host-level kernel and distribution environment details.
type HostContext struct {
	OS            string `json:"os"`
	OSVersion     string `json:"os_version"`
	Kernel        string `json:"kernel"`
	Architecture  string `json:"architecture"`
	CgroupVersion string `json:"cgroup_version"`
	SystemdActive bool   `json:"systemd_active"`
}

// RuntimeContext holds structural runtime environment metadata.
//
// NOTE: Milestone 0 defines the context boundary only. Runtime discovery and adapter
// population semantics are deferred to Milestone 4.
type RuntimeContext struct {
	ActiveRuntimes []string `json:"active_runtimes,omitempty"`
	DefaultRuntime string   `json:"default_runtime,omitempty"`
}

// ConfigContext holds structural configuration environment metadata.
//
// NOTE: Milestone 0 defines the context boundary only. Configuration discovery, drop-in
// resolution, and precedence evaluation semantics are deferred to Milestone 6.
type ConfigContext struct {
	SearchPaths []string `json:"search_paths,omitempty"`
}

// EvaluationContext represents the complete execution identity and host environment
// under which capabilities and requirements are evaluated.
type EvaluationContext struct {
	Host          HostContext     `json:"host"`
	Identity      IdentityContext `json:"identity"`
	Runtime       RuntimeContext  `json:"runtime"`
	Configuration ConfigContext   `json:"configuration"`
}
