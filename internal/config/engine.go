package config

import (
	"errors"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

const MaxConfigList = 64

var (
	ErrConfigFieldInvalid     = errors.New("config_field_invalid")
	ErrConfigFieldUnsupported = errors.New("config_field_unsupported")
	runtimeName               = regexp.MustCompile(`^[a-zA-Z0-9_.+-]+$`)
)

// EngineLayer is a projection of one parsed document, before family-specific
// merge. Unknown fields are counted without retaining their names or values.
type EngineLayer struct {
	CgroupManagerInvalid                       bool
	Runtime, CgroupManager                     *string
	ConmonPath, HelperBinariesDir, RuntimePath *ListLayer
	Environment                                *ListLayer
	Runtimes                                   map[string]ListLayer
	SelectionOverrides                         bool
	UnprojectedFieldCount                      int
}

type ListLayer struct {
	Values []string
	Append *bool
}

// FieldError adds only a recognized field identifier to a fixed diagnostic.
// It never retains a decoder error, arbitrary key, or raw value.
type FieldError struct {
	Reason error
	Field  string
}

func (e *FieldError) Error() string { return e.Reason.Error() }
func (e *FieldError) Unwrap() error { return e.Reason }

func ParseEngine(data []byte) (EngineLayer, error) {
	layer := EngineLayer{}
	document, err := ParseTOML(data)
	if err != nil {
		return layer, err
	}
	value, exists := document["engine"]
	if !exists {
		return layer, nil
	}
	engine, ok := value.(map[string]any)
	if !ok {
		return layer, &FieldError{Reason: ErrConfigFieldInvalid, Field: "engine"}
	}
	for _, key := range slices.Sorted(maps.Keys(engine)) {
		if err := parseEngineField(&layer, key, engine[key]); err != nil {
			return EngineLayer{}, &FieldError{Reason: err, Field: "engine." + key}
		}
	}
	return layer, nil
}

func parseEngineField(layer *EngineLayer, key string, value any) error {
	switch key {
	case "runtime", "cgroup_manager":
		text, ok := value.(string)
		if !ok {
			return ErrConfigFieldInvalid
		}
		if key == "runtime" {
			if text != "" && !runtimeName.MatchString(text) && !configPath(text) {
				return ErrConfigFieldUnsupported
			}
			layer.Runtime = &text
		} else {
			if text != "" && text != "systemd" && text != "cgroupfs" {
				layer.CgroupManagerInvalid = true
				text = "" // Retain the invalid marker, never the unrecognized value.
			}
			layer.CgroupManager = &text
		}
	case "runtimes":
		return parseRuntimes(layer, value)
	case "conmon_path", "helper_binaries_dir", "runtime_path", "env":
		list, err := parseConfigList(value, true, key)
		if err != nil {
			return err
		}
		switch key {
		case "conmon_path":
			layer.ConmonPath = &list
		case "helper_binaries_dir":
			layer.HelperBinariesDir = &list
		case "runtime_path":
			layer.RuntimePath = &list
		case "env":
			layer.Environment = &list
		}
	case "platform_to_oci_runtime":
		layer.SelectionOverrides = true
		layer.UnprojectedFieldCount++
	default:
		layer.UnprojectedFieldCount++
	}
	return nil
}

func parseRuntimes(layer *EngineLayer, value any) error {
	runtimes, ok := value.(map[string]any)
	if !ok {
		return ErrConfigFieldInvalid
	}
	if len(runtimes) > MaxConfigList {
		return ErrConfigLimit
	}
	layer.Runtimes = map[string]ListLayer{}
	for _, name := range slices.Sorted(maps.Keys(runtimes)) {
		if !runtimeName.MatchString(name) {
			return ErrConfigFieldUnsupported
		}
		list, err := parseConfigList(runtimes[name], false, "runtimes")
		if err != nil {
			return err
		}
		layer.Runtimes[name] = list
	}
	return nil
}

func parseConfigList(value any, attributed bool, field string) (ListLayer, error) {
	list := ListLayer{Values: []string{}}
	items, ok := value.([]any)
	if !ok {
		return list, ErrConfigFieldInvalid
	}
	if len(items) > MaxConfigList {
		return list, ErrConfigLimit
	}
	for _, item := range items {
		switch typed := item.(type) {
		case string:
			if field == "env" {
				// Cardinality is sufficient to invalidate unsupported environment
				// effects; keep even the intermediate layer free of raw values.
				typed = "redacted"
			} else if !configListPath(typed, field) {
				return list, ErrConfigFieldUnsupported
			}
			list.Values = append(list.Values, typed)
		case map[string]any:
			if !attributed {
				return list, ErrConfigFieldInvalid
			}
			for key, value := range typed {
				flag, ok := value.(bool)
				if key != "append" || !ok {
					return list, ErrConfigFieldInvalid
				}
				list.Append = &flag
			}
		default:
			return list, ErrConfigFieldInvalid
		}
	}
	return list, nil
}

func configListPath(value, field string) bool {
	if field == "helper_binaries_dir" && (value == "$BINDIR" || strings.HasPrefix(value, "$BINDIR/")) {
		value = "/binary" + strings.TrimPrefix(value, "$BINDIR")
	}
	return configPath(value)
}

func configPath(value string) bool {
	return path.IsAbs(value) && !strings.ContainsAny(value, "$~") && !strings.ContainsFunc(value, unicode.IsControl)
}

// MergeEngine implements containers.conf overlay semantics. Attributed array
// append state persists until explicitly reset; ordinary runtime-map arrays
// replace their own key. All retained values are copied from borrowed inputs.
func MergeEngine(previous model.EngineConfiguration, layer EngineLayer, source string) model.EngineConfiguration {
	out := previous
	out.Runtime, out.CgroupManager = mergeString(previous.Runtime, layer.Runtime, source),
		mergeString(previous.CgroupManager, layer.CgroupManager, source)
	if layer.CgroupManager != nil {
		out.CgroupManager.Invalid = layer.CgroupManagerInvalid
	}
	out.ConmonPath = mergeList(previous.ConmonPath, layer.ConmonPath, source)
	out.HelperBinariesDir = mergeList(previous.HelperBinariesDir, layer.HelperBinariesDir, source)
	out.RuntimePath = mergeList(previous.RuntimePath, layer.RuntimePath, source)
	out.Runtimes = map[string]model.ConfigList{}
	for name, list := range previous.Runtimes {
		out.Runtimes[name] = *mergeList(&list, nil, source)
	}
	for name, list := range layer.Runtimes {
		out.Runtimes[name] = *mergeList(nil, &list, source)
	}
	out.Environment = mergeRedacted(previous.Environment, layer.Environment, source)
	out.SelectionOverrides = previous.SelectionOverrides || layer.SelectionOverrides
	out.UnprojectedFieldCount += layer.UnprojectedFieldCount
	return out
}

func mergeString(previous *model.ConfigString, value *string, source string) *model.ConfigString {
	if value != nil {
		return &model.ConfigString{Value: *value, SourceID: source}
	}
	if previous != nil {
		copy := *previous
		return &copy
	}
	return nil
}

func mergeList(previous *model.ConfigList, layer *ListLayer, source string) *model.ConfigList {
	if previous == nil && layer == nil {
		return nil
	}
	out := model.ConfigList{Values: []string{}, Origins: []string{}, InheritedDefault: true}
	if previous != nil {
		out = *previous
		out.Values, out.Origins = slices.Clone(previous.Values), slices.Clone(previous.Origins)
		out.Append = copyFlag(previous.Append)
	}
	if layer == nil {
		return &out
	}
	if layer.Append != nil {
		out.Append = copyFlag(layer.Append)
	}
	if out.Append == nil || !*out.Append {
		out.Values, out.Origins, out.InheritedDefault = []string{}, []string{}, false
	}
	out.Values = append(out.Values, layer.Values...)
	for range layer.Values {
		out.Origins = append(out.Origins, source)
	}
	out.SourceID = source
	return &out
}

func mergeRedacted(previous *model.ConfigRedactedList, layer *ListLayer, source string) *model.ConfigRedactedList {
	if previous == nil && layer == nil {
		return nil
	}
	out := model.ConfigRedactedList{InheritedDefault: true}
	if previous != nil {
		out = *previous
		out.Append = copyFlag(previous.Append)
	}
	if layer != nil {
		if layer.Append != nil {
			out.Append = copyFlag(layer.Append)
		}
		if out.Append == nil || !*out.Append {
			out.Count, out.InheritedDefault = 0, false
		}
		out.Count += len(layer.Values)
		out.SourceID = source
	}
	return &out
}

func copyFlag(flag *bool) *bool {
	if flag == nil {
		return nil
	}
	copy := *flag
	return &copy
}
