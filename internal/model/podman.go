package model

// PodmanVersion retains the upstream numeric version separately from vendor
// revisions. Suffix and Build are descriptive; they do not establish feature
// support or an ordering between distribution packages.
type PodmanVersion struct {
	Major     uint32 `json:"major"`
	Minor     uint32 `json:"minor"`
	Patch     uint32 `json:"patch"`
	Canonical string `json:"canonical"`
	Suffix    string `json:"suffix,omitempty"`
	Build     string `json:"build,omitempty"`
	Trailing  string `json:"trailing,omitempty"`
	Raw       string `json:"raw"`
}

// ExecutableMetadata describes a file, not actual execution permission. Mode
// contains permission bits; ownership is unknown when UID/GID are nil.
type ExecutableMetadata struct {
	Regular        bool
	ExecutableBits bool
	Mode           uint32
	UID, GID       *uint32
}

type RuntimeDiscovery struct {
	Path      string
	Installed *bool
	File      *ExecutableMetadata
}

// PodmanVersionObservation preserves start/parse uncertainty separately from
// discovery. Successful parsing establishes only the selected CLI's version.
type PodmanVersionObservation struct {
	Path     string
	Runnable *bool
	Version  *PodmanVersion
}
