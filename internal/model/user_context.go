package model

// RuntimeDirectoryObservation validates metadata; it does not establish a login session.
type RuntimeDirectoryObservation struct {
	Path      string  `json:"path"`
	Source    string  `json:"source"`
	Present   *bool   `json:"present"`
	Directory *bool   `json:"directory"`
	UID       *uint32 `json:"uid"`
	Mode      *uint32 `json:"mode"`
	Private   *bool   `json:"private"`
	Valid     *bool   `json:"valid"`
}
type UserContextObservation struct {
	Runtime        RuntimeDirectoryObservation `json:"runtime"`
	SocketPresent  *bool                       `json:"socket_present"`
	SocketValid    *bool                       `json:"socket_valid"`
	Accessible     *bool                       `json:"accessible"`
	LingerEnabled  *bool                       `json:"linger_enabled"`
	QueryAttempted bool                        `json:"query_attempted"`
	ManagerVersion string                      `json:"manager_version"`
}
