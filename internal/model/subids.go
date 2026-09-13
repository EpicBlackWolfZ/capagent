package model

// SubIDAllocation preserves local-file measurements, independently of the effective NSS provider.
type SubIDAllocation struct {
	Present *bool         `json:"present"`
	Ranges  []SubIDRange  `json:"ranges"`
	Records []SubIDRecord `json:"records"`
	Total   *uint64       `json:"total"`
	Valid   *bool         `json:"valid"`
}
type SubIDRecord struct {
	Line    uint32      `json:"line"`
	Owner   string      `json:"owner"`
	Range   *SubIDRange `json:"range"`
	Problem string      `json:"problem,omitempty"`
}
type SubIDObservation struct {
	UID      SubIDAllocation `json:"uid"`
	GID      SubIDAllocation `json:"gid"`
	Provider string          `json:"provider"`
	Helpers  []MappingHelper `json:"helpers"`
}

// MappingHelper distinguishes metadata/access checks from a successful mapping operation.
// Usable can be false for a measured obstacle; passive collection cannot prove true.
type MappingHelper struct {
	PrivilegeBlocked *bool                `json:"privilege_blocked"`
	Name             string               `json:"name"`
	Path             string               `json:"path"`
	Present          *bool                `json:"present"`
	Regular          *bool                `json:"regular"`
	UID              *uint32              `json:"uid"`
	GID              *uint32              `json:"gid"`
	Mode             *uint32              `json:"mode"`
	SetUID           *bool                `json:"setuid"`
	SetGID           *bool                `json:"setgid"`
	Executable       *bool                `json:"executable"`
	Capabilities     *MappingCapabilities `json:"capabilities"`
	NoSUID           *bool                `json:"nosuid"`
	NoExec           *bool                `json:"noexec"`
	NoNewPrivileges  *bool                `json:"no_new_privileges"`
	Usable           *bool                `json:"usable"`
}
type MappingCapabilities struct {
	Present   bool    `json:"present"`
	Revision  uint32  `json:"revision"`
	Effective bool    `json:"effective"`
	SetUID    bool    `json:"setuid"`
	SetGID    bool    `json:"setgid"`
	RootID    *uint32 `json:"root_id,omitempty"`
}
