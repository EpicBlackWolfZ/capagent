package config

import (
	"errors"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

func TestNetworkSourceMergeKeepsRedactedInvalidValuesUntilSelection(t *testing.T) {
	t.Parallel()
	first, err := ParseNetwork([]byte("[network]\nnetwork_backend='synthetic-secret'\n"+
		"[containers]\ndns_servers=['192.0.2.1',{append=true}]"), true)
	if err != nil {
		t.Fatal(err)
	}
	value := MergeNetwork(model.NetworkConfiguration{}, first, "initial-source")
	if !value.Strings["network.network_backend"].Invalid || value.Strings["network.network_backend"].Value != "" {
		t.Fatal("raw invalid backend retained")
	}
	second, err := ParseNetwork([]byte("[network]\nnetwork_backend='netavark'\n[containers]\ndns_servers=['2001:db8::53']"), true)
	if err != nil {
		t.Fatal(err)
	}
	result := MergeNetwork(value, second, "later-source")
	if result.Strings["network.network_backend"].Invalid || result.Strings["network.network_backend"].SourceID != "later-source" {
		t.Fatal("valid override failed")
	}
	list := result.Lists["containers.dns_servers"]
	if len(list.Values) != 2 || list.Origins[0] != "initial-source" || list.Origins[1] != "later-source" || !list.InheritedDefault {
		t.Fatal("sticky attributed append or unknown defaults lost")
	}
	result.Lists["containers.dns_servers"].Values[0] = "changed"
	if value.Lists["containers.dns_servers"].Values[0] != "192.0.2.1" {
		t.Fatal("borrowed ownership")
	}
}

func TestNetworkTypesRemainPerSourceAndVersionSpecific(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, text string
		modern     bool
		want       error
	}{
		{"backend type", "[network]\nnetwork_backend=3", true, ErrConfigFieldInvalid},
		{"DNS list type", "[containers]\ndns_servers='192.0.2.1'", true, ErrConfigFieldInvalid},
		{"DNS item type", "[containers]\ndns_servers=[3]", true, ErrConfigFieldInvalid},
		{"port range", "[network]\ndns_bind_port=65536", true, ErrConfigFieldInvalid},
		{"port zero", "[network]\ndns_bind_port=0", true, nil},
		{"old firewall ignored", "[network]\nfirewall_driver=3", false, nil},
		{"new firewall typed", "[network]\nfirewall_driver=3", true, ErrConfigFieldInvalid},
		{"section type", "network=3", true, ErrConfigFieldInvalid},
		{"pasta type", "[network]\npasta_options=3", true, ErrConfigFieldInvalid},
		{"plugin relative", "[network]\ncni_plugin_dirs=['relative']", true, ErrConfigFieldUnsupported},
		{"plugin paths", "[network]\nnetavark_plugin_dirs=['/plugins']", true, nil},
		{"firewall syntax", "[network]\nfirewall_driver='raw secret'", true, ErrConfigFieldUnsupported},
		{"unknown network setting", "[network]\nunknown='value'", true, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseNetwork([]byte(test.text), test.modern)
			if !errors.Is(err, test.want) {
				t.Fatal(err)
			}
		})
	}
}

func TestNetworkMergeOwnsScalarsAndDiscardsDerivedHelperPlans(t *testing.T) {
	t.Parallel()
	first, err := ParseNetwork([]byte("[network]\ndns_bind_port=5353\npasta_options=['--opaque',{append=true}]"), true)
	if err != nil {
		t.Fatal(err)
	}
	before := MergeNetwork(model.NetworkConfiguration{}, first, "before")
	before.HelperPaths = map[string]model.ConfigList{"pasta": {Values: []string{"/old/pasta"}}}
	before.EngineSourceID = "old-engine"
	after := MergeNetwork(before, NetworkLayer{}, "after")
	if after.HelperPaths != nil || after.EngineSourceID != "" {
		t.Fatal("source merge retained a stale derived helper plan")
	}
	after.DNSBindPort.Value = 0
	*after.PastaOptions.Append = false
	if before.DNSBindPort.Value != 5353 || !*before.PastaOptions.Append {
		t.Fatal("source merge borrowed scalar ownership")
	}
}

func TestNetworkListMarkersFollowAppendAndReplacement(t *testing.T) {
	t.Parallel()
	var result model.NetworkConfiguration
	for _, text := range []string{
		"[containers]\ndns_servers=['synthetic-secret']\ndns_options=['synthetic-secret']",
		"[containers]\ndns_servers=['192.0.2.53',{append=true}]\ndns_options=['rotate',{append=true}]",
	} {
		layer, err := ParseNetwork([]byte(text), true)
		if err != nil {
			t.Fatal(err)
		}
		result = MergeNetwork(result, layer, "source")
	}
	if len(result.Lists["containers.dns_servers"].InvalidIndices) != 1 ||
		len(result.Lists["containers.dns_options"].UnmodeledIndices) != 1 {
		t.Fatal("append discarded redacted markers")
	}
	layer, err := ParseNetwork([]byte("[containers]\ndns_servers=['2001:db8::53',{append=false}]\ndns_options=[]"), true)
	if err != nil {
		t.Fatal(err)
	}
	replaced := MergeNetwork(result, layer, "replacement")
	if len(replaced.Lists["containers.dns_servers"].InvalidIndices) != 0 || len(replaced.Lists["containers.dns_servers"].Values) != 1 {
		t.Fatal("replacement kept stale invalid value")
	}
	// DNS options remain in append mode until explicitly reset.
	if len(replaced.Lists["containers.dns_options"].UnmodeledIndices) != 1 {
		t.Fatal("append mode was not sticky")
	}
}

func TestNetworkRecognizedValueProjection(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		field, value       string
		invalid, unmodeled bool
	}{
		{"containers.dns_servers", "192.0.2.53", false, false},
		{"containers.dns_servers", "not-an-address", true, false},
		{"containers.dns_searches", "example.test", false, false},
		{"containers.dns_searches", ".", false, false},
		{"containers.dns_searches", "bad name", true, false},
		{"containers.dns_options", "ndots:2", false, false},
		{"containers.dns_options", "unknown-option", false, true},
		{slirpOptionsField, "cidr=192.0.2.0/24", false, false},
		{slirpOptionsField, "cidr=2001:db8::/64", true, false},
		{slirpOptionsField, "port_handler=rootlesskit", false, false},
		{slirpOptionsField, "port_handler=unknown", true, false},
		{slirpOptionsField, "allow_host_loopback=false", false, false},
		{slirpOptionsField, "enable_ipv6=invalid", true, false},
		{slirpOptionsField, "mtu=67", true, false},
		{slirpOptionsField, "mtu=1500", false, false},
		{slirpOptionsField, "mtu=invalid", true, false},
		{slirpOptionsField, "outbound_addr=192.0.2.1", false, false},
		{slirpOptionsField, "outbound_addr6=2001:db8::1", false, false},
		{slirpOptionsField, "outbound_addr=eth0", false, true},
		{slirpOptionsField, "unknown=secret", true, false},
		{slirpOptionsField, "missing-value", true, false},
	} {
		t.Run(test.value, func(t *testing.T) {
			t.Parallel()
			list, err := parseNetworkList([]any{test.value}, test.field)
			if err != nil || (len(list.InvalidIndices) != 0) != test.invalid || (len(list.UnmodeledIndices) != 0) != test.unmodeled {
				t.Fatal("wrong field projection", err)
			}
			if (test.invalid || test.unmodeled) && list.Values[0] != "" {
				t.Fatal("raw value retained")
			}
		})
	}
}
