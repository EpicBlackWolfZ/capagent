package model

// StorageConfiguration is a projection of one storage.conf selection phase.
// Source replacement and rootless defaults are determined by the adapter, not a
// generic field overlay. Options contains only recognized, redacted-safe keys.
type StorageConfiguration struct {
	Problems              []string                `json:"problems,omitempty"`
	MountProgram          *ConfigString           `json:"mount_program,omitempty"`
	Driver                *ConfigString           `json:"driver,omitempty"`
	DriverPriority        *ConfigList             `json:"driver_priority,omitempty"`
	RunRoot               *ConfigString           `json:"runroot,omitempty"`
	GraphRoot             *ConfigString           `json:"graphroot,omitempty"`
	RootlessStoragePath   *ConfigString           `json:"rootless_storage_path,omitempty"`
	ImageStore            *ConfigString           `json:"imagestore,omitempty"`
	TransientStore        *ConfigBool             `json:"transient_store,omitempty"`
	AdditionalImageStores *ConfigList             `json:"additionalimagestores,omitempty"`
	AdditionalLayerStores *ConfigList             `json:"additionallayerstores,omitempty"`
	Options               map[string]ConfigString `json:"options"`
	UnprojectedFieldCount int                     `json:"unprojected_field_count"`
}

type ConfigBool struct {
	Value    bool   `json:"value"`
	SourceID string `json:"source_id"`
}
