package probe

import (
	"errors"
	"slices"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

var ErrObservationScope = errors.New("probe returned a different evaluation scope")

// retainObservation snapshots all mutable payloads before publishing a result.
// Probes must not mutate a returned value concurrently with this transfer.
func retainObservation(obs model.Observation, scope model.EvaluationScope) (model.Observation, error) {
	obs = SnapshotObservation(obs)
	var mismatch bool
	if obs.Scope == (model.EvaluationScope{}) {
		obs.Scope = scope
	}
	mismatch = obs.Scope != scope
	for i := range obs.Facts {
		fact := &obs.Facts[i]
		if fact.Scope == (model.EvaluationScope{}) {
			fact.Scope = scope
		}
		mismatch = mismatch || fact.Scope != scope
	}
	if mismatch {
		return obs, ErrObservationScope
	}
	return obs, nil
}

// SnapshotObservation copies all mutable payloads for transfer to a run owner.
// The caller must not mutate inputs concurrently with the copy.
func SnapshotObservation(obs model.Observation) model.Observation {
	obs.Executable = snapshotExecutable(obs.Executable)
	obs.Quadlet = copyValue(obs.Quadlet)
	if q := obs.Quadlet; q != nil {
		q.Locations = slices.Clone(q.Locations)
		for i := range q.Locations {
			q.Locations[i].Present = copyValue(q.Locations[i].Present)
			q.Locations[i].Directory = copyValue(q.Locations[i].Directory)
		}
	}
	obs.Identity = copyValue(obs.Identity)
	obs.SubIDs = snapshotSubIDs(obs.SubIDs)
	obs.UserContext = snapshotUserContext(obs.UserContext)
	obs.Host = snapshotHost(obs.Host)
	obs.Facts = slices.Clone(obs.Facts)
	obs.Diagnostics = slices.Clone(obs.Diagnostics)
	obs.Podman = copyValue(obs.Podman)
	if p := obs.Podman; p != nil {
		p.OCIRuntime = copyValue(p.OCIRuntime)
		p.VersionParts = copyValue(p.VersionParts)
		p.GraphRoot, p.RunRoot = copyValue(p.GraphRoot), copyValue(p.RunRoot)
		p.ServiceIsRemote = copyValue(p.ServiceIsRemote)
		p.NetworkBackend = copyValue(p.NetworkBackend)
		p.StorageDriver = copyValue(p.StorageDriver)
		p.CgroupVersion = copyValue(p.CgroupVersion)
		p.CgroupManager = copyValue(p.CgroupManager)
		p.Rootless = copyValue(p.Rootless)
		p.Available = copyValue(p.Available)
	}
	obs.PodmanHelper = copyValue(obs.PodmanHelper)
	if h := obs.PodmanHelper; h != nil {
		h.Present = copyValue(h.Present)
	}
	obs.Discovery = copyValue(obs.Discovery)
	if d := obs.Discovery; d != nil {
		d.Installed = copyValue(d.Installed)
		d.File = copyValue(d.File)
		if f := d.File; f != nil {
			f.UID, f.GID = copyValue(f.UID), copyValue(f.GID)
		}
	}
	obs.Version = copyValue(obs.Version)
	if v := obs.Version; v != nil {
		v.Runnable, v.Version = copyValue(v.Runnable), copyValue(v.Version)
	}
	for i := range obs.Facts {
		obs.Facts[i].RawData = slices.Clone(obs.Facts[i].RawData)
	}
	return obs
}

func snapshotExecutable(input *model.ExecutableObservation) *model.ExecutableObservation {
	out := copyValue(input)
	if out == nil {
		return nil
	}
	out.Candidates = slices.Clone(out.Candidates)
	for i := range out.Candidates {
		c := &out.Candidates[i]
		c.Present, c.Executable = copyValue(c.Present), copyValue(c.Executable)
		c.Device, c.Inode = copyValue(c.Device), copyValue(c.Inode)
		c.File = copyValue(c.File)
		if c.File != nil {
			c.File.UID, c.File.GID = copyValue(c.File.UID), copyValue(c.File.GID)
		}
	}
	return out
}

func copyValue[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func snapshotHost(input *model.HostObservation) *model.HostObservation {
	out := copyValue(input)
	if out == nil {
		return nil
	}
	out.Filesystems = copyValue(out.Filesystems)
	if f := out.Filesystems; f != nil {
		f.Registered = slices.Clone(f.Registered)
		f.OverlayRegistered = copyValue(f.OverlayRegistered)
		f.FUSERegistered = copyValue(f.FUSERegistered)
	}
	out.Network = copyValue(out.Network)
	if n := out.Network; n != nil {
		n.Protocols = slices.Clone(n.Protocols)
		n.IPv4TCP = copyValue(n.IPv4TCP)
		n.IPv4UDP = copyValue(n.IPv4UDP)
		n.IPv6TCP = copyValue(n.IPv6TCP)
		n.IPv6UDP = copyValue(n.IPv6UDP)
		n.IPv6AllDisabled = copyValue(n.IPv6AllDisabled)
		n.IPv6DefaultDisabled = copyValue(n.IPv6DefaultDisabled)
	}
	out.Resolver = copyValue(out.Resolver)
	if r := out.Resolver; r != nil {
		r.Present = copyValue(r.Present)
		r.Nameservers = slices.Clone(r.Nameservers)
		r.Search = slices.Clone(r.Search)
		r.Options = slices.Clone(r.Options)
	}
	out.OS = copyValue(out.OS)
	if out.OS != nil {
		out.OS.IDLike = slices.Clone(out.OS.IDLike)
	}
	out.Kernel = copyValue(out.Kernel)
	if out.Kernel != nil {
		out.Kernel.Parts = copyValue(out.Kernel.Parts)
	}
	out.Cgroups = copyValue(out.Cgroups)
	if c := out.Cgroups; c != nil {
		c.Memberships = slices.Clone(c.Memberships)
		c.Mounts = slices.Clone(c.Mounts)
		for i := range c.Memberships {
			c.Memberships[i].Controllers = slices.Clone(c.Memberships[i].Controllers)
		}
		for i := range c.Mounts {
			c.Mounts[i].Controllers = slices.Clone(c.Mounts[i].Controllers)
			c.Mounts[i].SubtreeControl = slices.Clone(c.Mounts[i].SubtreeControl)
		}
	}
	out.Namespaces = copyValue(out.Namespaces)
	if out.Namespaces != nil {
		out.Namespaces.Namespaces = slices.Clone(out.Namespaces.Namespaces)
	}
	out.Security = copyValue(out.Security)
	if s := out.Security; s != nil {
		s.LSMs = slices.Clone(s.LSMs)
		s.AppArmorProfiles = slices.Clone(s.AppArmorProfiles)
		s.SeccompActions = slices.Clone(s.SeccompActions)
		s.AppArmorEnabled = copyValue(s.AppArmorEnabled)
		s.SeccompMode = copyValue(s.SeccompMode)
		s.NoNewPrivileges = copyValue(s.NoNewPrivileges)
		s.UnprivilegedUserNSClone = copyValue(s.UnprivilegedUserNSClone)
		s.MaxUserNamespaces = copyValue(s.MaxUserNamespaces)
	}
	out.Systemd = copyValue(out.Systemd)
	if s := out.Systemd; s != nil {
		s.Installed, s.UtilityInstalled = copyValue(s.Installed), copyValue(s.UtilityInstalled)
		s.Running, s.RuntimeDirectory, s.Accessible = copyValue(s.Running), copyValue(s.RuntimeDirectory), copyValue(s.Accessible)
	}
	return out
}

func snapshotSubIDs(input *model.SubIDObservation) *model.SubIDObservation {
	out := copyValue(input)
	if out == nil {
		return nil
	}
	for _, a := range []*model.SubIDAllocation{&out.UID, &out.GID} {
		a.Present = copyValue(a.Present)
		a.Total = copyValue(a.Total)
		a.Valid = copyValue(a.Valid)
		a.Ranges = slices.Clone(a.Ranges)
		a.Records = slices.Clone(a.Records)
		for i := range a.Records {
			a.Records[i].Range = copyValue(a.Records[i].Range)
		}
	}
	out.Helpers = slices.Clone(out.Helpers)
	for i := range out.Helpers {
		h := &out.Helpers[i]
		h.PrivilegeBlocked = copyValue(h.PrivilegeBlocked)
		h.Present = copyValue(h.Present)
		h.Regular = copyValue(h.Regular)
		h.UID = copyValue(h.UID)
		h.GID = copyValue(h.GID)
		h.Mode = copyValue(h.Mode)
		h.SetUID = copyValue(h.SetUID)
		h.SetGID = copyValue(h.SetGID)
		h.Executable = copyValue(h.Executable)
		h.NoSUID = copyValue(h.NoSUID)
		h.NoExec = copyValue(h.NoExec)
		h.NoNewPrivileges = copyValue(h.NoNewPrivileges)
		h.Usable = copyValue(h.Usable)
		h.Capabilities = copyValue(h.Capabilities)
		if h.Capabilities != nil {
			h.Capabilities.RootID = copyValue(h.Capabilities.RootID)
		}
	}
	return out
}

func snapshotUserContext(input *model.UserContextObservation) *model.UserContextObservation {
	out := copyValue(input)
	if out == nil {
		return nil
	}
	r := &out.Runtime
	r.Present = copyValue(r.Present)
	r.Directory = copyValue(r.Directory)
	r.UID = copyValue(r.UID)
	r.Mode = copyValue(r.Mode)
	r.Private = copyValue(r.Private)
	r.Valid = copyValue(r.Valid)
	out.SocketPresent = copyValue(out.SocketPresent)
	out.SocketValid = copyValue(out.SocketValid)
	out.Accessible = copyValue(out.Accessible)
	out.LingerEnabled = copyValue(out.LingerEnabled)
	return out
}
