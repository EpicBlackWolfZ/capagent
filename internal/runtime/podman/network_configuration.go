package podman

import (
	"errors"
	"path"
	"strconv"
	"strings"

	"github.com/EpicBlackWolfZ/capagent/internal/config"
	"github.com/EpicBlackWolfZ/capagent/internal/knowledge"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

const NetworkConfigID = "podman.configuration.network"
const pastaName = "pasta"
const slirpName = "slirp4netns"
const configStatusParsed = "parsed"
const networkRootlessCommand = "network.default_rootless_network_cmd"
const networkLegacySourceRoot = "https://github.com/containers/podman/blob/v4.9.3/"
const networkModernSourceRoot = "https://github.com/containers/podman/blob/v5.8.4/"

type networkCollector struct {
	observation          model.Observation
	pending              *model.ConfigurationSource
	complete, applicable bool
}

func newNetworkCollector(p EngineProbe, env platform.Environment) *networkCollector {
	obs := runtimeObservation(NetworkConfigID, env.Scope(), measurementTime(p.Now))
	profile := p.profile(env.Scope())
	c := &model.ConfigurationObservation{Family: "network", RuntimePath: p.Path, Profile: profile,
		SelectionComplete: profile != knowledge.ContainersUnqualified, ParseComplete: true, Sources: []model.ConfigurationSource{},
		References: []string{knowledge.Containers493Source, knowledge.Containers584Source,
			networkLegacySourceRoot + "vendor/github.com/containers/common/pkg/config/default.go",
			networkModernSourceRoot + "vendor/go.podman.io/common/pkg/config/default.go",
			networkLegacySourceRoot + "vendor/github.com/containers/common/pkg/config/config.go",
			networkModernSourceRoot + "vendor/go.podman.io/common/pkg/config/config.go",
			networkLegacySourceRoot + "pkg/specgen/generate/container_create.go",
			networkModernSourceRoot + "pkg/specgen/generate/container_create.go",
			networkModernSourceRoot + "vendor/go.podman.io/common/libnetwork/slirp4netns/slirp4netns.go",
			networkModernSourceRoot + "libpod/container_internal_common.go"}}
	obs.Configuration = c
	if c.SelectionComplete {
		c.VersionSourceID = p.Version.ID
		c.Network = networkDefaults(profile, p.Version.ID)
	}
	return &networkCollector{observation: obs, complete: true, applicable: c.SelectionComplete}
}

func networkDefaults(profile, source string) *model.NetworkConfiguration {
	command := pastaName
	if profile == knowledge.Containers493 {
		command = slirpName
	}
	out := &model.NetworkConfiguration{Strings: map[string]model.ConfigString{
		networkRootlessCommand: {Value: command, SourceID: source}}, Lists: map[string]model.ConfigList{},
		DNSBindPort: &model.ConfigUint{Value: 0, SourceID: source}, PastaOptions: &model.ConfigRedactedList{SourceID: source}}
	for _, key := range []string{"containers.dns_servers", "containers.dns_options", "containers.dns_searches", "engine.network_cmd_options"} {
		out.Lists[key] = model.ConfigList{Values: []string{}, Origins: []string{}, SourceID: source}
	}
	return out
}

func (n *networkCollector) source(source model.ConfigurationSource) model.ConfigurationSource {
	source.ID = NetworkConfigID + ".source." + strconv.Itoa(source.Order)
	source.Engine = nil
	return source
}

func (n *networkCollector) file(source model.ConfigurationSource, data []byte) {
	source = n.source(source)
	c := n.observation.Configuration
	layer, err := config.ParseNetwork(data, c.Profile != knowledge.Containers493)
	if err != nil {
		source.Problem = err.Error()
		var field *config.FieldError
		if errors.As(err, &field) {
			source.Field = field.Field
		}
		c.ParseComplete, n.applicable = false, false
		switch {
		case errors.Is(err, config.ErrConfigMalformed):
			source.Status = "malformed"
		case errors.Is(err, config.ErrConfigFieldInvalid):
			source.Status = "invalid"
		case errors.Is(err, config.ErrConfigLimit):
			source.Status = "limit"
			n.complete = false
		default:
			source.Status = "unsupported"
			n.complete = false
		}
	} else {
		source.Status = configStatusParsed
		projection := config.MergeNetwork(model.NetworkConfiguration{}, layer, source.ID)
		source.Network = &projection
		if n.applicable {
			merged := config.MergeNetwork(*c.Network, layer, source.ID)
			c.Network = &merged
			source.Applied = true
		}
	}
	n.pending = &source
}

func (n *networkCollector) retain(source model.ConfigurationSource) {
	if n.pending != nil {
		source = *n.pending
		n.pending = nil
	} else {
		source = n.source(source)
		source.Applied = false
		switch source.Status {
		case "listed", configStatusAbsent, "symlink_skipped":
		case "stat_denied", "stat_incomplete":
			n.complete = false
		default:
			n.complete, n.applicable, n.observation.Configuration.ParseComplete = false, false, false
		}
	}
	n.observation.Configuration.Sources = append(n.observation.Configuration.Sources, source)
	switch source.Status {
	case configStatusParsed, "listed", configStatusAbsent, "symlink_skipped":
	default:
		n.observation.Diagnostics = append(n.observation.Diagnostics, model.Diagnostic{Code: "config_source_" + source.Status,
			Message: "network configuration source is " + source.Status, Reference: source.ID})
	}
}

func (n *networkCollector) finish(engine model.Observation) model.Observation {
	c := n.observation.Configuration
	c.SelectionComplete = c.SelectionComplete && engine.Configuration.SelectionComplete
	if !c.SelectionComplete {
		n.complete = false
		if len(c.Sources) == 0 {
			c.ParseComplete = false
		}
	}
	if c.Network != nil {
		c.Network.EngineSourceID = engine.ID
		c.Network.HelperPaths = networkHelperPaths(engine, *c.Network, n.observation.ID)
		if c.Network.PastaOptions != nil && c.Network.PastaOptions.Count != 0 {
			n.observation.Diagnostics = append(n.observation.Diagnostics, model.Diagnostic{Code: "pasta_options_unvalidated",
				Message: "pasta options retain redacted cardinality and merge provenance; helper option compatibility is unverified"})
		}
	}
	if !n.complete {
		n.observation, _ = failedRuntimeObservation(n.observation, "config_evidence_incomplete", platform.ErrIncomplete)
	}
	return n.observation
}

func networkHelperPaths(engine model.Observation, network model.NetworkConfiguration, source string) map[string]model.ConfigList {
	plans := map[string]model.ConfigList{}
	c := engine.Configuration
	if c == nil || c.Engine == nil || !c.SelectionComplete || !c.ParseComplete || engine.Completeness != model.Complete {
		return plans
	}
	e := c.Engine
	if e.CgroupManager != nil && e.CgroupManager.Invalid ||
		e.Environment != nil && (e.Environment.Count != 0 || e.Environment.InheritedDefault) {
		return plans
	}
	for _, role := range []string{"netavark", "aardvark_dns", pastaName, slirpName} {
		if role == slirpName {
			if selected, ok := network.Strings["engine.network_cmd_path"]; ok && selected.Value != "" {
				plans[role] = model.ConfigList{Values: []string{selected.Value}, Origins: []string{selected.SourceID}, SourceID: selected.SourceID}
				continue
			}
		}
		dirs := e.HelperBinariesDir
		if dirs == nil || dirs.InheritedDefault {
			continue
		}
		plan := model.ConfigList{Values: []string{}, Origins: []string{}, SourceID: dirs.SourceID}
		for i, directory := range dirs.Values {
			if strings.Contains(directory, "$BINDIR") {
				plan.InheritedDefault = true
				break
			}
			binary := role
			if role == "aardvark_dns" {
				binary = "aardvark-dns"
			}
			plan.Values = append(plan.Values, path.Join(directory, binary))
			origin := dirs.SourceID
			if i < len(dirs.Origins) {
				origin = dirs.Origins[i]
			}
			plan.Origins = append(plan.Origins, origin)
		}
		if role == pastaName || role == slirpName {
			for _, directory := range []string{"/usr/bin", "/bin"} {
				plan.Values = append(plan.Values, path.Join(directory, role))
				plan.Origins = append(plan.Origins, source)
			}
		}
		plans[role] = plan
	}
	return plans
}
