package model

// HostObservation contains only typed measurements, never capability verdicts.
// Missing pointer fields are unobserved; false and empty observed values survive JSON.
type HostObservation struct {
	OS      *OSObservation      `json:"os,omitempty"`
	Kernel  *KernelObservation  `json:"kernel,omitempty"`
	Systemd *SystemdObservation `json:"systemd,omitempty"`
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
