package model

// HostObservation contains only typed measurements, never capability verdicts.
// Missing pointer fields are unobserved; false and empty observed values survive JSON.
type HostObservation struct {
	Cgroups    *CgroupObservation    `json:"cgroups,omitempty"`
	Namespaces *NamespaceObservation `json:"namespaces,omitempty"`
	Security   *SecurityObservation  `json:"security,omitempty"`
	OS         *OSObservation        `json:"os,omitempty"`
	Kernel     *KernelObservation    `json:"kernel,omitempty"`
	Systemd    *SystemdObservation   `json:"systemd,omitempty"`
}

type OSObservation struct {
	ID         string   `json:"id"`
	VersionID  string   `json:"version_id"`
	Name       string   `json:"name"`
	PrettyName string   `json:"pretty_name"`
	Variant    string   `json:"variant"`
	IDLike     []string `json:"id_like"`
}

type KernelVersion struct {
	Major uint32 `json:"major"`
	Minor uint32 `json:"minor"`
	Patch uint32 `json:"patch"`
}
type KernelObservation struct {
	Release      string         `json:"release"`
	Version      string         `json:"version"`
	Machine      string         `json:"machine"`
	Architecture string         `json:"architecture"`
	Parts        *KernelVersion `json:"parts,omitempty"`
}

type SystemdObservation struct {
	Installed        *bool  `json:"installed"`
	UtilityInstalled *bool  `json:"utility_installed"`
	Running          *bool  `json:"running"`
	RuntimeDirectory *bool  `json:"runtime_directory"`
	Accessible       *bool  `json:"accessible"`
	Version          string `json:"version"`
	UtilityPath      string `json:"utility_path"`
}

type CgroupObservation struct {
	Mode        string             `json:"mode"`
	Memberships []CgroupMembership `json:"memberships"`
	Mounts      []CgroupMount      `json:"mounts"`
}
type CgroupMembership struct {
	Hierarchy   string   `json:"hierarchy"`
	Controllers []string `json:"controllers"`
	Path        string   `json:"path"`
}
type CgroupMount struct {
	Path           string   `json:"path"`
	Root           string   `json:"root"`
	Type           string   `json:"type"`
	ControllerPath string   `json:"controller_path"`
	Controllers    []string `json:"controllers"`
	SubtreeControl []string `json:"subtree_control"`
}
type NamespaceObservation struct {
	Namespaces []Namespace `json:"namespaces"`
}
type SecurityObservation struct {
	SELinux                 string            `json:"selinux"`
	LSMs                    []string          `json:"lsms"`
	AppArmorEnabled         *bool             `json:"apparmor_enabled"`
	AppArmorProfiles        []AppArmorProfile `json:"apparmor_profiles"`
	SeccompMode             *uint32           `json:"seccomp_mode"`
	SeccompActions          []string          `json:"seccomp_actions"`
	NoNewPrivileges         *bool             `json:"no_new_privileges"`
	ProcessScope            string            `json:"process_scope"`
	UnprivilegedUserNSClone *bool             `json:"unprivileged_userns_clone"`
	MaxUserNamespaces       *uint64           `json:"max_user_namespaces"`
}
type AppArmorProfile struct {
	Name string `json:"name"`
	Mode string `json:"mode"`
}
