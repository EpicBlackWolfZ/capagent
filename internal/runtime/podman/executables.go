package podman

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

const conmonName = "conmon"

const maxExecutableCandidates = 16

// ExecutableProbe never searches ambient PATH or executes a candidate. A source
// owner supplies bounded absolute paths and its selection provenance.
const (
	sourceConfiguration = "configuration"
	sourceRuntime       = "runtime"
)

type ExecutableProbe struct {
	Role, Source, SourceID, RuntimePath string
	Paths                               []string
	Generator                           bool
	Now                                 func() time.Time
}

func (p ExecutableProbe) ID() string           { return "podman.executable." + p.Source + "." + p.Role }
func (ExecutableProbe) Dependencies() []string { return nil }
func (p ExecutableProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	obs := runtimeObservation(p.ID(), env.Scope(), measurementTime(p.Now))
	obs.Executable = &model.ExecutableObservation{Role: p.Role, Source: p.Source, SourceID: p.SourceID,
		RuntimePath: p.RuntimePath, Candidates: []model.ExecutableCandidate{}}
	if len(p.Paths) == 0 || len(p.Paths) > maxExecutableCandidates || env.Files() == nil {
		return failedRuntimeObservation(obs, "executable_candidates_unavailable", platform.ErrIncomplete)
	}
	for _, name := range p.Paths {
		candidate, err := observeExecutable(ctx, env.Files(), name, p.Generator)
		obs.Executable.Candidates = append(obs.Executable.Candidates, candidate)
		if err != nil {
			obs = incompleteObservation(obs, "executable_metadata_unavailable")
			obs.Facts[0].Completeness = model.Partial
			if p.Generator || ctx.Err() != nil {
				return obs, err
			}
		}
		if p.Generator && candidate.Present != nil && *candidate.Present {
			obs.Executable.SelectedPath = name
			break
		}
	}
	if (p.Source == sourceRuntime || p.Source == sourceConfiguration) && len(p.Paths) == 1 {
		obs.Executable.SelectedPath = p.Paths[0]
	}
	return obs, ctx.Err()
}

func observeExecutable(ctx context.Context, files platform.ScopedView, name string, generator bool) (model.ExecutableCandidate, error) {
	c := model.ExecutableCandidate{Path: name}
	if !safeInfoPath(name) {
		c.Path = ""
		return c, platform.ErrMalformed
	}
	if err := ctx.Err(); err != nil {
		return c, err
	}
	subpath := strings.TrimPrefix(name, "/")
	if generator {
		// A direct /dev/null mask remains a mask even in an offline filesystem
		// without device nodes. Empty regular files are masks as well.
		target, err := files.Readlink(subpath)
		if err != nil && !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.EINVAL) {
			return c, err
		}
		if err == nil {
			if !path.IsAbs(target) {
				target = path.Join(path.Dir(name), target)
			}
			if path.Clean(target) == "/dev/null" {
				c.Present, c.Masked = boolPointer(true), true
				return c, nil
			}
		}
	}
	info, err := files.Stat(subpath)
	if errors.Is(err, fs.ErrNotExist) {
		c.Present = boolPointer(false)
		return c, nil
	}
	if err != nil {
		return c, err
	}
	return executableFromInfo(ctx, files, name, info, generator)
}

func executableFromInfo(ctx context.Context, files platform.ScopedView, name string,
	info fs.FileInfo, generator bool,
) (model.ExecutableCandidate, error) {
	c := model.ExecutableCandidate{Path: name, Present: boolPointer(true)}
	c.File = &model.ExecutableMetadata{Regular: info.Mode().IsRegular(),
		ExecutableBits: info.Mode().Perm()&executableBits != 0, Mode: uint32(info.Mode().Perm())}
	if owner, known := platform.OwnershipOf(info); known {
		c.File.UID, c.File.GID = &owner.UID, &owner.GID
	}
	if id, known := platform.IdentityOf(info); known {
		c.Device, c.Inode = &id.Device, &id.Inode
	}
	if generator && (c.File.Regular && info.Size() == 0 || platform.IsNullDevice(info)) {
		c.Masked = true
		return c, nil
	}
	if !c.File.Regular {
		c.Executable = boolPointer(false)
		return c, nil
	}
	allowed, err := files.ExecutableAccess(ctx, strings.TrimPrefix(name, "/"))
	if err == nil {
		c.Executable = &allowed
	}
	return c, err
}

func boolPointer(value bool) *bool { return &value }

// HelperProbes inventory common packaging locations, not Podman's configured
// selection algorithm. Unlisted custom locations require info or configuration.
func HelperProbes(now func() time.Time) []ExecutableProbe {
	var probes []ExecutableProbe
	for _, role := range []string{"crun", "runc", conmonName, "netavark", "aardvark-dns", pastaName, slirpName, "fuse-overlayfs"} {
		p := ExecutableProbe{Role: strings.ReplaceAll(role, "-", "_"), Source: "trusted_candidates", Now: now}
		for _, dir := range []string{"/usr/local/bin", "/usr/bin", "/bin", "/usr/local/libexec/podman",
			"/usr/libexec/podman", "/usr/local/lib/podman", "/usr/lib/podman"} {
			p.Paths = append(p.Paths, dir+"/"+role)
		}
		probes = append(probes, p)
	}
	return probes
}

func SelectedHelperProbes(info model.Observation, now func() time.Time) []ExecutableProbe {
	p := info.Podman
	if p == nil || p.Available == nil || !*p.Available || info.Completeness != model.Complete {
		return nil
	}
	ociPath := ""
	if p.OCIRuntime != nil {
		ociPath = p.OCIRuntime.Path
	}
	var probes []ExecutableProbe
	for _, helper := range []struct{ role, name string }{{"oci_runtime", ociPath}, {conmonName, p.ConmonPath},
		{"netavark", p.HelperPath}, {"aardvark_dns", p.AardvarkPath}, {pastaName, p.PastaPath}, {slirpName, p.SlirpPath}} {
		if helper.name != "" {
			probes = append(probes, ExecutableProbe{Role: helper.role, Paths: []string{helper.name}, Source: sourceRuntime,
				SourceID: info.ID, RuntimePath: p.Path, Now: now})
		}
	}
	return probes
}
