package host

import (
	"context"
	"errors"
	"io/fs"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

type FilesystemProbe struct{ Now func() time.Time }

func (FilesystemProbe) ID() string             { return "host.filesystems" }
func (FilesystemProbe) Dependencies() []string { return nil }
func (p FilesystemProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	obs := newHostObservation(p.ID(), env.Scope(), p.Now)
	state := &model.FilesystemObservation{Registered: []model.FilesystemRegistration{}}
	obs.Host.Filesystems = state
	entries, err := platform.NewHostProcfsReader(env.Files()).Filesystems(ctx)
	recordSource(&obs, "proc/filesystems", err)
	var names []string
	for _, entry := range entries {
		if !validHostText(entry.Name) {
			err = platform.ErrMalformed
			finishSource(&obs, err)
			continue
		}
		names = append(names, entry.Name)
		state.Registered = append(state.Registered, model.FilesystemRegistration{Name: entry.Name, Nodev: entry.NoDev})
	}
	state.OverlayRegistered = registered(names, "overlay", err)
	state.FUSERegistered = registered(names, "fuse", err)
	return obs, ctx.Err()
}
func registered(names []string, name string, err error) *bool {
	if slices.Contains(names, name) {
		return ptr(true)
	}
	if err == nil {
		return ptr(false)
	}
	return nil
}

type NetworkProbe struct{ Now func() time.Time }

func (NetworkProbe) ID() string             { return "host.network" }
func (NetworkProbe) Dependencies() []string { return nil }
func (p NetworkProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	obs := newHostObservation(p.ID(), env.Scope(), p.Now)
	state := &model.NetworkObservation{Protocols: []string{}}
	obs.Host.Network = state
	entries, err := platform.NewHostProcfsReader(env.Files()).Protocols(ctx)
	recordSource(&obs, "proc/self/net/protocols", err)
	for _, entry := range entries {
		state.Protocols = append(state.Protocols, entry.Name)
	}
	state.IPv4TCP = registered(state.Protocols, "TCP", err)
	state.IPv4UDP = registered(state.Protocols, "UDP", err)
	state.IPv6TCP = registered(state.Protocols, "TCPv6", err)
	state.IPv6UDP = registered(state.Protocols, "UDPv6", err)
	ipv6Absent := state.IPv6TCP != nil && !*state.IPv6TCP && state.IPv6UDP != nil && !*state.IPv6UDP
	state.IPv6AllDisabled = readBoolSysctl(ctx, env, &obs, "proc/sys/net/ipv6/conf/all/disable_ipv6", ipv6Absent)
	state.IPv6DefaultDisabled = readBoolSysctl(ctx, env, &obs, "proc/sys/net/ipv6/conf/default/disable_ipv6", ipv6Absent)
	return obs, ctx.Err()
}

type ResolverProbe struct{ Now func() time.Time }

func (ResolverProbe) ID() string             { return "host.resolver" }
func (ResolverProbe) Dependencies() []string { return nil }
func (p ResolverProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	obs := newHostObservation(p.ID(), env.Scope(), p.Now)
	state := &model.ResolverObservation{Nameservers: []string{}, Search: []string{}, Options: []string{}}
	obs.Host.Resolver = state
	data, err := env.Files().ReadFile(ctx, "etc/resolv.conf")
	recordSource(&obs, "etc/resolv.conf", missingIsKnown(err))
	if errors.Is(err, fs.ErrNotExist) {
		state.Present = ptr(false)
		return obs, ctx.Err()
	}
	if err == nil {
		state.Present = ptr(true)
	}
	lines, parseErr := hostLines(data, err)
	finishSource(&obs, parseErr)
	for _, line := range lines {
		if ctx.Err() != nil {
			finishSource(&obs, ctx.Err())
			break
		}
		if index := strings.IndexAny(line, "#;"); index >= 0 {
			line = line[:index]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if parseResolverDirective(state, fields) != nil {
			finishSource(&obs, platform.ErrMalformed)
		}
	}
	return obs, ctx.Err()
}
func parseResolverDirective(state *model.ResolverObservation, fields []string) error {
	const pairFields = 2
	switch fields[0] {
	case "nameserver":
		if len(fields) != pairFields {
			return platform.ErrMalformed
		}
		address, err := netip.ParseAddr(fields[1])
		if err != nil || !validHostText(fields[1]) {
			return platform.ErrMalformed
		}
		state.Nameservers = append(state.Nameservers, address.String())
	case "domain", "search":
		if len(fields) < pairFields || fields[0] == "domain" && len(fields) != pairFields {
			return platform.ErrMalformed
		}
		for _, name := range fields[1:] {
			if !resolverDomain(name) {
				return platform.ErrMalformed
			}
		}
		state.Domain = ""
		state.Search = []string{}
		if fields[0] == "domain" {
			state.Domain = fields[1]
		} else {
			state.Search = append(state.Search, fields[1:]...)
		}
	case "options":
		for _, option := range fields[1:] {
			if !resolverOption(option) {
				return platform.ErrMalformed
			}
			state.Options = append(state.Options, option)
		}
	default:
		return platform.ErrMalformed
	}
	return nil
}
func resolverDomain(name string) bool {
	const maxDomain = 253
	const maxLabel = 63
	if len(name) > maxDomain {
		return false
	}
	name = strings.TrimSuffix(name, ".")
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > maxLabel {
			return false
		}
		for _, ch := range label {
			if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') && ch != '_' && ch != '-' {
				return false
			}
		}
	}
	return true
}
func resolverOption(option string) bool {
	name, value, hasValue := strings.Cut(option, ":")
	if hasValue {
		if !slices.Contains([]string{"ndots", "timeout", "attempts"}, name) {
			return false
		}
		_, err := strconv.ParseUint(value, 10, versionNumberBits)
		return err == nil
	}
	return slices.Contains([]string{"rotate", "single-request", "single-request-reopen", "no-tld-query", "use-vc", "trust-ad",
		"edns0", "no-reload", "no-aaaa"}, name)
}
