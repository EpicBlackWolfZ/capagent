package model

// StoragePathObservation measures selected roots or read-only stores. When a
// target is absent, access and filesystem metadata describe CheckedPath only;
// an accessible ancestor never proves that the target can be created.
type StoragePathObservation struct {
	Source      string                `json:"source"`
	SourceID    string                `json:"source_id"`
	RuntimePath string                `json:"runtime_path"`
	Writable    bool                  `json:"writable"`
	Paths       []StoragePathMetadata `json:"paths"`
}

type StoragePathMetadata struct {
	Role        string             `json:"role"`
	Path        string             `json:"path"`
	Present     *bool              `json:"present"`
	Directory   *bool              `json:"directory"`
	CheckedPath string             `json:"checked_path,omitempty"`
	Ancestor    bool               `json:"ancestor"`
	CheckedUID  *uint32            `json:"checked_uid"`
	CheckedGID  *uint32            `json:"checked_gid"`
	CheckedMode *uint32            `json:"checked_mode"`
	Accessible  *bool              `json:"accessible"`
	Filesystem  *StorageFilesystem `json:"filesystem,omitempty"`
}

type StorageFilesystem struct {
	Type     int64 `json:"type"`
	ReadOnly *bool `json:"read_only"`
	NoSUID   *bool `json:"nosuid"`
	NoExec   *bool `json:"noexec"`
}
