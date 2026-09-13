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
	Regular        bool    `json:"regular"`
	ExecutableBits bool    `json:"executable_bits"`
	Mode           uint32  `json:"mode"`
	UID            *uint32 `json:"uid"`
	GID            *uint32 `json:"gid"`
}

// ExecutableCandidate is a metadata snapshot and a separate kernel access query.
// Neither establishes successful execution or pins a future command's identity.
type ExecutableCandidate struct {
	Path       string              `json:"path"`
	Present    *bool               `json:"present"`
	File       *ExecutableMetadata `json:"file,omitempty"`
	Device     *uint64             `json:"device"`
	Inode      *uint64             `json:"inode"`
	Executable *bool               `json:"executable"`
	Masked     bool                `json:"masked"`
}

// ExecutableObservation separates a bounded candidate inventory from a selected
// runtime/configuration path. SourceID links selection to its source observation.
type ExecutableObservation struct {
	Role         string                `json:"role"`
	Source       string                `json:"source"`
	SourceID     string                `json:"source_id,omitempty"`
	RuntimePath  string                `json:"runtime_path,omitempty"`
	SelectedPath string                `json:"selected_path,omitempty"`
	Candidates   []ExecutableCandidate `json:"candidates"`
}

type SelectedOCIRuntime struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type SearchLocation struct {
	Path      string `json:"path"`
	Present   *bool  `json:"present"`
	Directory *bool  `json:"directory"`
}

// QuadletObservation lists documented candidate input locations. It neither
// parses units nor asserts what a generator of an unmeasured version consumes.
type QuadletObservation struct {
	Rootless  bool             `json:"rootless"`
	Reference string           `json:"reference"`
	Locations []SearchLocation `json:"locations"`
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
