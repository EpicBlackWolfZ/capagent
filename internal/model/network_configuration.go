package model

// NetworkConfiguration contains only recognized network and container-DNS
// settings. HelperPaths is a selected search plan, never execution authority.
type NetworkConfiguration struct {
	Strings               map[string]ConfigString `json:"strings"`
	Lists                 map[string]ConfigList   `json:"lists"`
	DNSBindPort           *ConfigUint             `json:"dns_bind_port,omitempty"`
	PastaOptions          *ConfigRedactedList     `json:"pasta_options,omitempty"`
	HelperPaths           map[string]ConfigList   `json:"helper_paths,omitempty"`
	EngineSourceID        string                  `json:"engine_source_id,omitempty"`
	UnprojectedFieldCount int                     `json:"unprojected_field_count"`
}

type ConfigUint struct {
	Value    uint64 `json:"value"`
	SourceID string `json:"source_id"`
}
