package host

import (
	"context"
	"errors"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

const maxHostDiagnostics = 32
const maxHostText = 4096
const versionNumberBits = 32

var osIDPattern = regexp.MustCompile(`^[a-z0-9._-]+$`)
var kernelVersionPattern = regexp.MustCompile(`^([0-9]+)\.([0-9]+)\.([0-9]+)(?:[^0-9].*)?$`)
var systemdVersionPattern = regexp.MustCompile(`^systemd ([0-9]+)(?:[ .(-].*)?$`)

type OSReleaseProbe struct{ Now func() time.Time }

func (OSReleaseProbe) ID() string             { return "host.os" }
func (OSReleaseProbe) Dependencies() []string { return nil }
func (p OSReleaseProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	obs := newHostObservation(p.ID(), env.Scope(), p.Now)
	obs.Host.OS = &model.OSObservation{IDLike: []string{}}
	source := "etc/os-release"
	data, err := env.Files().ReadFile(ctx, source)
	if errors.Is(err, fs.ErrNotExist) {
		source = "usr/lib/os-release"
		data, err = env.Files().ReadFile(ctx, source)
	}
	recordSource(&obs, source, err)
	lines, parseErr := hostLines(data, err)
	seen := map[string]bool{}
	for _, line := range lines {
		if ctx.Err() != nil {
			parseErr = errors.Join(parseErr, ctx.Err())
			break
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, raw, ok := strings.Cut(line, "=")
		value, valid := assignmentValue(raw)
		if !ok || !valid || !validKey(key) {
			parseErr = platform.ErrMalformed
			continue
		}
		if seen[key] {
			hostWarning(&obs, "duplicate_os_key")
		}
		seen[key] = true
		os := obs.Host.OS
		switch key {
		case "ID":
			if !osIDPattern.MatchString(value) {
				parseErr = platform.ErrMalformed
			} else {
				os.ID = value
			}
		case "VERSION_ID":
			os.VersionID = value
		case "NAME":
			os.Name = value
		case "PRETTY_NAME":
			os.PrettyName = value
		case "VARIANT":
			os.Variant = value
		case "ID_LIKE":
			os.IDLike = strings.Fields(value)
		}
	}
	if obs.Host.OS.ID == "" {
		parseErr = errors.Join(parseErr, platform.ErrIncomplete)
	}
	finishSource(&obs, parseErr)
	return obs, errors.Join(err, parseErr, ctx.Err())
}

func validKey(key string) bool {
	if key == "" {
		return false
	}
	for _, ch := range key {
		if ch != '_' && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') {
			return false
		}
	}
	return true
}
func validHostText(value string) bool {
	return len(value) <= maxHostText && utf8.ValidString(value) && !strings.ContainsFunc(value, unicode.IsControl)
}
func assignmentValue(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", true
	}
	quote := byte(0)
	if raw[0] == '\'' || raw[0] == '"' {
		quote = raw[0]
		raw = raw[1:]
	}
	var value strings.Builder
	closed := quote == 0
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if quote != 0 && ch == quote {
			tail := strings.TrimSpace(raw[i+1:])
			closed = tail == "" || strings.HasPrefix(tail, "#")
			break
		}
		if quote == 0 && (ch == ' ' || ch == '\t') {
			tail := strings.TrimSpace(raw[i:])
			closed = tail == "" || strings.HasPrefix(tail, "#")
			break
		}
		if ch == '\\' && quote != '\'' {
			i++
			if i == len(raw) {
				return "", false
			}
			value.WriteByte(raw[i])
			continue
		}
		if quote != '\'' && (ch == '$' || ch == '`') {
			return "", false
		}
		if quote == 0 && (ch == '\'' || ch == '"' || ch == ';') {
			return "", false
		}
		value.WriteByte(ch)
	}
	result := value.String()
	return result, closed && validHostText(result)
}

func hostLines(data []byte, readErr error) ([]string, error) {
	limits := platform.DefaultParserLimits()
	if len(data) > limits.InputBytes {
		data = data[:limits.InputBytes]
		readErr = errors.Join(readErr, platform.ErrLimitExceeded)
	}
	if readErr != nil {
		data = data[:strings.LastIndexByte(string(data), '\n')+1]
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > limits.Records {
		lines = lines[:limits.Records]
		readErr = errors.Join(readErr, platform.ErrLimitExceeded)
	}
	for i, line := range lines {
		if len(line) > limits.LineBytes {
			lines[i] = ""
			readErr = errors.Join(readErr, platform.ErrLimitExceeded)
		}
	}
	return lines, readErr
}
func newHostObservation(id string, scope model.EvaluationScope, now func() time.Time) model.Observation {
	return model.Observation{ID: id, ProbeID: id, Scope: scope, Timestamp: now(), Completeness: model.Complete,
		Summary: "Passive current-context host measurement", Host: &model.HostObservation{}}
}
func hostWarning(obs *model.Observation, code string) {
	if len(obs.Diagnostics) < maxHostDiagnostics {
		obs.Diagnostics = append(obs.Diagnostics, model.Diagnostic{
			Code: code, Message: "host measurement diagnostic", Reference: obs.ID})
	}
}
func recordSource(obs *model.Observation, source string, err error) {
	completeness := model.Complete
	if err != nil {
		completeness = model.Partial
	}
	obs.Facts = append(obs.Facts, model.Fact{ID: obs.ID + "." + strconv.Itoa(len(obs.Facts)), Source: source, Scope: obs.Scope,
		Timestamp: obs.Timestamp, Completeness: completeness})
	finishSource(obs, err)
}
func finishSource(obs *model.Observation, err error) {
	if err == nil {
		return
	}
	obs.Completeness = model.Partial
	if len(obs.Facts) > 0 {
		obs.Facts[len(obs.Facts)-1].Completeness = model.Partial
	}
	code := "host_read_failed"
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		code = "host_cancelled"
	case errors.Is(err, fs.ErrPermission):
		code = "host_permission_denied"
	case errors.Is(err, fs.ErrNotExist):
		code = "host_source_missing"
	case errors.Is(err, platform.ErrLimitExceeded):
		code = "host_limit_exceeded"
	case errors.Is(err, platform.ErrMalformed):
		code = "host_malformed"
	case errors.Is(err, platform.ErrIncomplete):
		code = "host_incomplete"
	}
	hostWarning(obs, code)
}

type KernelProbe struct{ Now func() time.Time }

func (KernelProbe) ID() string             { return "host.kernel" }
func (KernelProbe) Dependencies() []string { return nil }
func (p KernelProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	obs := newHostObservation(p.ID(), env.Scope(), p.Now)
	info := platform.UnameInfo{}
	err := ctx.Err()
	if err == nil {
		if env.Host() == nil {
			err = platform.ErrIncomplete
		} else {
			info, err = env.Host().Uname()
		}
	}
	if !validHostText(info.Release) || !validHostText(info.Version) || !validHostText(info.Machine) {
		info = platform.UnameInfo{}
		err = errors.Join(err, platform.ErrMalformed)
	}
	recordSource(&obs, "syscall.uname", err)
	architecture := info.Machine
	switch architecture {
	case "x86_64":
		architecture = "amd64"
	case "aarch64":
		architecture = "arm64"
	}
	obs.Host.Kernel = &model.KernelObservation{Release: info.Release, Version: info.Version, Machine: info.Machine,
		Architecture: architecture, Parts: parseKernelVersion(info.Release)}
	if info.Release == "" || info.Machine == "" {
		finishSource(&obs, platform.ErrIncomplete)
	}
	if obs.Host.Kernel.Parts == nil {
		hostWarning(&obs, "kernel_version_unparsed")
	}
	return obs, err
}
func parseKernelVersion(release string) *model.KernelVersion {
	fields := kernelVersionPattern.FindStringSubmatch(release)
	if fields == nil {
		return nil
	}
	var parts [3]uint32
	for i, value := range fields[1:] {
		n, err := strconv.ParseUint(value, 10, versionNumberBits)
		if err != nil {
			return nil
		}
		parts[i] = uint32(n)
	}
	return &model.KernelVersion{Major: parts[0], Minor: parts[1], Patch: parts[2]}
}

type SystemdProbe struct{ Now func() time.Time }

func (SystemdProbe) ID() string             { return "host.systemd" }
func (SystemdProbe) Dependencies() []string { return nil }
func (p SystemdProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	obs := newHostObservation(p.ID(), env.Scope(), p.Now)
	state := &model.SystemdObservation{}
	obs.Host.Systemd = state
	comm, err := env.Files().ReadFile(ctx, "proc/1/comm")
	recordSource(&obs, "proc/1/comm", err)
	if err == nil && validHostText(strings.TrimSpace(string(comm))) && len(strings.TrimSpace(string(comm))) > 0 {
		state.Running = ptr(strings.TrimSpace(string(comm)) == "systemd")
	} else {
		finishSource(&obs, platform.ErrIncomplete)
	}
	info, dirErr := env.Files().Stat("run/systemd/system")
	recordSource(&obs, "run/systemd/system", missingIsKnown(dirErr))
	if dirErr == nil {
		state.RuntimeDirectory = ptr(info.IsDir())
	} else if errors.Is(dirErr, fs.ErrNotExist) {
		state.RuntimeDirectory = ptr(false)
	}
	for _, candidate := range []string{"usr/lib/systemd/systemd", "lib/systemd/systemd"} {
		fi, e := env.Files().Stat(candidate)
		recordSource(&obs, candidate, missingIsKnown(e))
		if e == nil && fi.Mode().IsRegular() {
			state.Installed = ptr(true)
			break
		}
		if e == nil || errors.Is(e, fs.ErrNotExist) {
			state.Installed = ptr(false)
		} else {
			state.Installed = nil
			break
		}
	}
	if state.Running != nil && *state.Running {
		state.Installed = ptr(true)
	}
	for _, candidate := range []string{"usr/bin/systemctl", "bin/systemctl"} {
		fi, e := env.Files().Stat(candidate)
		recordSource(&obs, candidate, missingIsKnown(e))
		if e != nil && !errors.Is(e, fs.ErrNotExist) {
			break
		}
		if e == nil && fi.Mode().IsRegular() {
			state.UtilityInstalled = ptr(true)
			state.UtilityPath = "/" + candidate
			result, runErr := env.HostMetadata().SystemdVersion(ctx, state.UtilityPath)
			if result.ExitCode != 0 || result.StdoutTruncated || result.StderrTruncated {
				runErr = errors.Join(runErr, platform.ErrIncomplete)
			}
			recordSource(&obs, "command.systemctl_version", runErr)
			first, _, _ := strings.Cut(string(result.Stdout), "\n")
			matches := systemdVersionPattern.FindStringSubmatch(first)
			if runErr == nil && matches != nil {
				state.Version = matches[1]
			} else {
				finishSource(&obs, platform.ErrMalformed)
			}
			break
		}
		state.UtilityInstalled = ptr(false)
	}
	return obs, ctx.Err()
}
func missingIsKnown(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
func ptr[T any](value T) *T { return &value }
