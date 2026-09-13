package config

import (
	"maps"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

const (
	networkBackendField  = "network.network_backend"
	rootlessCommandField = "network.default_rootless_network_cmd"
	slirpOptionsField    = "engine.network_cmd_options"
	dnsServersField      = "containers.dns_servers"
	dnsOptionsField      = "containers.dns_options"
	dnsSearchesField     = "containers.dns_searches"
)

// NetworkLayer retains type-correct values for source merge. Invalid recognized
// strings are redacted, not rejected before a later assignment can replace them.
type NetworkLayer struct {
	Strings               map[string]model.ConfigString
	Lists                 map[string]ListLayer
	DNSBindPort           *uint64
	PastaOptions          *ListLayer
	UnprojectedFieldCount int
}

func ParseNetwork(data []byte, modern bool) (NetworkLayer, error) {
	layer := NetworkLayer{Strings: map[string]model.ConfigString{}, Lists: map[string]ListLayer{}}
	document, err := ParseTOML(data)
	if err != nil {
		return layer, err
	}
	for _, section := range []string{"network", "containers", "engine"} {
		value, exists := document[section]
		if !exists {
			continue
		}
		fields, ok := value.(map[string]any)
		if !ok {
			return NetworkLayer{}, &FieldError{Reason: ErrConfigFieldInvalid, Field: section}
		}
		for _, key := range slices.Sorted(maps.Keys(fields)) {
			if err := parseNetworkField(&layer, section+"."+key, fields[key], modern); err != nil {
				return NetworkLayer{}, &FieldError{Reason: err, Field: section + "." + key}
			}
		}
	}
	return layer, nil
}

func parseNetworkField(layer *NetworkLayer, field string, value any, modern bool) error {
	switch field {
	case networkBackendField, rootlessCommandField, "network.network_config_dir", "engine.network_cmd_path", "network.firewall_driver":
		return parseNetworkString(layer, field, value, modern)
	case "network.dns_bind_port":
		port, ok := value.(int64)
		const maxPort = 65535
		if !ok || port < 0 || port > maxPort {
			return ErrConfigFieldInvalid
		}
		v := uint64(port)
		layer.DNSBindPort = &v
	case "network.pasta_options":
		list, err := parseConfigList(value, true, "env")
		if err != nil {
			return err
		}
		layer.PastaOptions = &list
	case dnsServersField, dnsOptionsField, dnsSearchesField, slirpOptionsField, "network.cni_plugin_dirs", "network.netavark_plugin_dirs":
		list, err := parseNetworkList(value, field)
		if err != nil {
			return err
		}
		layer.Lists[field] = list
	default:
		if strings.HasPrefix(field, "network.") {
			layer.UnprojectedFieldCount++
		}
	}
	return nil
}

func parseNetworkList(value any, field string) (ListLayer, error) {
	// The shared decoder owns the attributed-array type and append contract.
	list, err := parseConfigList(value, true, "env")
	if err != nil {
		return list, err
	}
	i := 0
	for _, item := range value.([]any) {
		text, ok := item.(string)
		if !ok {
			continue
		}
		invalid, unmodeled := false, false
		switch field {
		case dnsServersField:
			invalid = net.ParseIP(text) == nil
		case dnsSearchesField:
			invalid = !networkSearchDomain(text)
		case dnsOptionsField:
			unmodeled = !dnsOption.MatchString(text)
		case slirpOptionsField:
			invalid, unmodeled = slirpOption(text)
		default:
			if !configPath(text) {
				return ListLayer{}, ErrConfigFieldUnsupported
			}
		}
		if invalid {
			list.InvalidIndices = append(list.InvalidIndices, i)
		}
		if unmodeled {
			list.UnmodeledIndices = append(list.UnmodeledIndices, i)
		}
		if invalid || unmodeled {
			text = ""
		}
		list.Values[i] = text
		i++
	}
	return list, nil
}

var dnsOption = regexp.MustCompile(`^(?:debug|rotate|no-check-names|inet6|ip6-bytestring|ip6-dotint|no-ip6-dotint|edns0|` +
	`single-request|single-request-reopen|no-tld-query|use-vc|no-reload|trust-ad|no-aaaa|(?:ndots|timeout|attempts):[0-9]+)$`)
var dnsSearchDomain = regexp.MustCompile(`^[a-zA-Z0-9_](?:[a-zA-Z0-9_.-]*[a-zA-Z0-9_])?\.?$`)

func networkSearchDomain(value string) bool {
	const maxDomain = 253
	return value == "." || len(value) <= maxDomain && dnsSearchDomain.MatchString(value)
}

func slirpOption(text string) (invalid, unmodeled bool) {
	key, value, ok := strings.Cut(text, "=")
	if !ok {
		return true, false
	}
	switch key {
	case "cidr":
		ip, _, err := net.ParseCIDR(value)
		return err != nil || ip.To4() == nil, false
	case "port_handler":
		return value != "slirp4netns" && value != "rootlesskit", false
	case "allow_host_loopback", "enable_ipv6":
		return value != "true" && value != "false", false
	case "mtu":
		const minMTU = 68
		mtu, err := strconv.Atoi(value)
		return err != nil || mtu < minMTU, false
	case "outbound_addr", "outbound_addr6":
		ip := net.ParseIP(value)
		if ip != nil && (key == "outbound_addr") == (ip.To4() != nil) {
			return false, false
		}
		// Upstream also accepts an existing interface name. No interface lookup
		// or raw unrecognized name enters this configuration projection.
		return false, true
	default:
		return true, false
	}
}

func MergeNetwork(previous model.NetworkConfiguration, layer NetworkLayer, source string) model.NetworkConfiguration {
	out := previous
	// Helper plans are derived only after source merge and engine binding.
	out.HelperPaths, out.EngineSourceID = nil, ""
	out.Strings = maps.Clone(previous.Strings)
	if out.Strings == nil {
		out.Strings = map[string]model.ConfigString{}
	}
	for key, value := range layer.Strings {
		value.SourceID = source
		out.Strings[key] = value
	}
	out.Lists = map[string]model.ConfigList{}
	for key, list := range previous.Lists {
		out.Lists[key] = *mergeList(&list, nil, source)
	}
	for key, list := range layer.Lists {
		var before *model.ConfigList
		if previous, ok := out.Lists[key]; ok {
			before = &previous
		}
		out.Lists[key] = *mergeList(before, &list, source)
	}
	if previous.DNSBindPort != nil {
		v := *previous.DNSBindPort
		out.DNSBindPort = &v
	}
	if layer.DNSBindPort != nil {
		out.DNSBindPort = &model.ConfigUint{Value: *layer.DNSBindPort, SourceID: source}
	}
	out.PastaOptions = mergeRedacted(previous.PastaOptions, layer.PastaOptions, source)
	out.UnprojectedFieldCount += layer.UnprojectedFieldCount
	return out
}

func parseNetworkString(layer *NetworkLayer, field string, value any, modern bool) error {
	if field == "network.firewall_driver" && !modern {
		layer.UnprojectedFieldCount++
		return nil
	}
	text, ok := value.(string)
	if !ok {
		return ErrConfigFieldInvalid
	}
	v := model.ConfigString{Value: text}
	switch field {
	case networkBackendField:
		v.Invalid = text != "" && text != "netavark" && text != "cni"
	case rootlessCommandField:
		v.Invalid = text != "" && text != "pasta" && text != "slirp4netns"
	case "network.firewall_driver":
		if text != "" && !runtimeName.MatchString(text) {
			return ErrConfigFieldUnsupported
		}
	default:
		if text != "" && !configPath(text) {
			return ErrConfigFieldUnsupported
		}
	}
	if v.Invalid {
		v.Value = ""
	}
	layer.Strings[field] = v
	return nil
}
