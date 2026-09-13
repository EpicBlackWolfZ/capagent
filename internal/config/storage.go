package config

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

// StorageLayer is a bounded projection. Attributed append arrays are not part of
// storage.conf. Unknown fields are counted, never retained as raw diagnostics.
type StorageLayer struct {
	invalidOptions                                               map[string]bool
	Driver, RunRoot, GraphRoot, RootlessStoragePath, ImageStore  *string
	DriverPriority, AdditionalImageStores, AdditionalLayerStores *ListLayer
	TransientStore                                               *bool
	Options                                                      map[string]string
	UnprojectedFieldCount                                        int
}

// StorageFields selects version-qualified fields without consulting runtime state.
type StorageFields struct{ Composefs bool }

// ParseStorage projects the modern bounded field set. Adapters must select the
// qualified fields explicitly with ParseStorageForFields.
func ParseStorage(data []byte) (StorageLayer, error) {
	return ParseStorageForFields(data, StorageFields{Composefs: true})
}

func ParseStorageForFields(data []byte, supported StorageFields) (StorageLayer, error) {
	layer := StorageLayer{Options: map[string]string{}}
	document, err := ParseTOML(data)
	if err != nil {
		return layer, err
	}
	value, exists := document["storage"]
	if !exists {
		return layer, nil
	}
	fields, ok := value.(map[string]any)
	if !ok {
		return StorageLayer{}, &FieldError{Reason: ErrConfigFieldInvalid, Field: "storage"}
	}
	if !supported.Composefs {
		if options, ok := fields["options"].(map[string]any); ok {
			if overlay, ok := options["overlay"].(map[string]any); ok {
				if _, present := overlay["use_composefs"]; present {
					delete(overlay, "use_composefs")
					layer.UnprojectedFieldCount++
				}
			}
		}
	}
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		if err := parseStorageField(&layer, key, fields[key]); err != nil {
			return StorageLayer{}, &FieldError{Reason: err, Field: "storage." + key}
		}
	}
	return layer, nil
}

func parseStorageField(layer *StorageLayer, key string, value any) error {
	switch key {
	case "driver", "runroot", "graphroot", "rootless_storage_path", "imagestore":
		text, ok := value.(string)
		if !ok {
			return ErrConfigFieldInvalid
		}
		if text != "" {
			if key == "driver" {
				if !runtimeName.MatchString(text) {
					return ErrConfigFieldUnsupported
				}
			} else if !storagePath(text, key != "imagestore") {
				return ErrConfigFieldUnsupported
			}
		}
		switch key {
		case "driver":
			layer.Driver = &text
		case "runroot":
			layer.RunRoot = &text
		case "graphroot":
			layer.GraphRoot = &text
		case "rootless_storage_path":
			layer.RootlessStoragePath = &text
		case "imagestore":
			layer.ImageStore = &text
		}
	case "driver_priority":
		list, err := storageList(value, "driver_priority")
		if err != nil {
			return err
		}
		layer.DriverPriority = &list
	case "transient_store":
		flag, ok := value.(bool)
		if !ok {
			return ErrConfigFieldInvalid
		}
		layer.TransientStore = &flag
	case "options":
		return parseStorageOptions(layer, value, "")
	default:
		layer.UnprojectedFieldCount++
	}
	return nil
}

func storagePath(value string, expansion bool) bool {
	if strings.ContainsFunc(value, unicode.IsControl) || strings.Contains(value, "~") {
		return false
	}
	if expansion && strings.HasPrefix(value, "$") {
		return true
	}
	if expansion {
		return strings.HasPrefix(value, "/")
	}
	return configPath(value)
}

func storageList(value any, field string) (ListLayer, error) {
	list := ListLayer{Values: []string{}}
	items, ok := value.([]any)
	if !ok {
		return list, ErrConfigFieldInvalid
	}
	if len(items) > MaxConfigList {
		return list, ErrConfigLimit
	}
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return list, ErrConfigFieldInvalid
		}
		switch field {
		case "driver_priority":
			if !runtimeName.MatchString(text) {
				return list, ErrConfigFieldUnsupported
			}
		default:
			for _, part := range strings.Split(text, ",") {
				if field == "additionallayerstores" {
					part = strings.TrimSuffix(part, ":ref")
				}
				if !configPath(part) || strings.Contains(part, ":") {
					return list, ErrConfigFieldUnsupported
				}
			}
		}
		list.Values = append(list.Values, text)
	}
	return list, nil
}

func parseStorageOptions(layer *StorageLayer, value any, prefix string) error {
	fields, ok := value.(map[string]any)
	if !ok {
		return ErrConfigFieldInvalid
	}
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		value := fields[key]
		if prefix == "" {
			switch key {
			case "overlay", "vfs":
				if err := parseStorageOptions(layer, value, key+"."); err != nil {
					return err
				}
				continue
			case "additionalimagestores", "additionallayerstores":
				list, err := storageList(value, key)
				if err != nil {
					return err
				}
				if key == "additionalimagestores" {
					layer.AdditionalImageStores = &list
				} else {
					layer.AdditionalLayerStores = &list
				}
				continue
			case "force_mask":
				mask, ok := value.(int64)
				if !ok || mask < 0 {
					return ErrConfigFieldInvalid
				}
				const maxMode = 0o7777
				if mask > maxMode {
					return ErrConfigFieldUnsupported
				}
				layer.Options[key] = strconv.FormatInt(mask, 8)
				continue
			}
		}
		if !recognizedStorageOption(prefix, key) {
			layer.UnprojectedFieldCount++
			continue
		}
		text, ok := value.(string)
		if !ok {
			return ErrConfigFieldInvalid
		}
		if text != "" {
			switch key {
			case "mount_program":
				if !configPath(text) {
					return ErrConfigFieldUnsupported
				}
			case "force_mask":
				if text != "shared" && text != "private" {
					if _, err := strconv.ParseUint(text, 8, 32); err != nil {
						text = layer.invalidOption(prefix + key)
					}
				}
			default:
				if _, err := strconv.ParseBool(text); err != nil {
					text = layer.invalidOption(prefix + key)
				}
			}
		}
		layer.Options[prefix+key] = text
	}
	return nil
}

func recognizedStorageOption(prefix, key string) bool {
	if prefix == "vfs." {
		return key == "ignore_chown_errors"
	}
	switch key {
	case "mount_program", "ignore_chown_errors", "skip_mount_home":
		return true
	case "force_mask", "use_composefs":
		return prefix == "overlay."
	default:
		return false
	}
}

// ProjectStorage copies a single source; it never merges a preceding file.
func ProjectStorage(layer StorageLayer, source string) model.StorageConfiguration {
	out := model.StorageConfiguration{Driver: mergeString(nil, layer.Driver, source),
		DriverPriority: mergeList(nil, layer.DriverPriority, source),
		RunRoot:        mergeString(nil, layer.RunRoot, source), GraphRoot: mergeString(nil, layer.GraphRoot, source),
		RootlessStoragePath: mergeString(nil, layer.RootlessStoragePath, source), ImageStore: mergeString(nil, layer.ImageStore, source),
		AdditionalImageStores: mergeList(nil, layer.AdditionalImageStores, source),
		AdditionalLayerStores: mergeList(nil, layer.AdditionalLayerStores, source),
		Options:               map[string]model.ConfigString{}, UnprojectedFieldCount: layer.UnprojectedFieldCount}
	if layer.TransientStore != nil {
		out.TransientStore = &model.ConfigBool{Value: *layer.TransientStore, SourceID: source}
	}
	for key, value := range layer.Options {
		out.Options[key] = model.ConfigString{Value: value, SourceID: source, Invalid: layer.invalidOptions[key]}
	}
	return out
}

func (layer *StorageLayer) invalidOption(key string) string {
	if layer.invalidOptions == nil {
		layer.invalidOptions = map[string]bool{}
	}
	layer.invalidOptions[key] = true
	return "" // Preserve shape/provenance without retaining an unrecognized raw value.
}
