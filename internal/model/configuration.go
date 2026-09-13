package model

// ConfigurationSource identifies the bytes inspected, not a pinned inode or an
// atomic transaction with runtime inspection. Selected means membership in the
// source plan; Applied additionally requires successful preceding inputs.
type ConfigurationSource struct {
	ID       string               `json:"id"`
	Path     string               `json:"path"`
	Kind     string               `json:"kind"`
	Status   string               `json:"status"`
	Order    int                  `json:"order"`
	Selected bool                 `json:"selected"`
	Applied  bool                 `json:"applied"`
	SHA256   string               `json:"sha256,omitempty"`
	Field    string               `json:"field,omitempty"`
	Problem  string               `json:"problem,omitempty"`
	Engine   *EngineConfiguration `json:"engine,omitempty"`
}

type ConfigurationObservation struct {
	Family            string                `json:"family"`
	RuntimePath       string                `json:"runtime_path,omitempty"`
	VersionSourceID   string                `json:"version_source_id,omitempty"`
	Profile           string                `json:"profile"`
	References        []string              `json:"references"`
	SelectionComplete bool                  `json:"selection_complete"`
	ParseComplete     bool                  `json:"parse_complete"`
	Sources           []ConfigurationSource `json:"sources"`
	Engine            *EngineConfiguration  `json:"engine,omitempty"`
}

// ConfigString and ConfigList retain the provenance of configured values,
// separately from runtime-effective measurements. An inherited default is not
// known merely because a configuration appends additional entries to it.
type ConfigString struct {
	Value    string `json:"value"`
	SourceID string `json:"source_id"`
}

type ConfigList struct {
	Values           []string `json:"values"`
	Origins          []string `json:"origins"`
	SourceID         string   `json:"source_id"`
	Append           *bool    `json:"append"`
	InheritedDefault bool     `json:"inherited_default"`
}

// ConfigRedactedList records only cardinality and merge state, never environment
// variable names or values. Its presence can affect interpretation of later
// runtime-selected configuration and helper paths.
type ConfigRedactedList struct {
	Count            int    `json:"count"`
	SourceID         string `json:"source_id"`
	Append           *bool  `json:"append"`
	InheritedDefault bool   `json:"inherited_default"`
}

type EngineConfiguration struct {
	Runtime               *ConfigString         `json:"runtime,omitempty"`
	CgroupManager         *ConfigString         `json:"cgroup_manager,omitempty"`
	Runtimes              map[string]ConfigList `json:"runtimes"`
	ConmonPath            *ConfigList           `json:"conmon_path,omitempty"`
	HelperBinariesDir     *ConfigList           `json:"helper_binaries_dir,omitempty"`
	RuntimePath           *ConfigList           `json:"runtime_path,omitempty"`
	Environment           *ConfigRedactedList   `json:"environment,omitempty"`
	SelectionOverrides    bool                  `json:"selection_overrides"`
	UnprojectedFieldCount int                   `json:"unprojected_field_count"`
}
