package platform

import (
	"context"
	"errors"
	"maps"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type UnameInfo struct{ Release, Version, Machine string }
type FilesystemInfo struct {
	Type                       int64
	NoSUID, NoExec, FlagsKnown bool
}

// HostQueries exposes only fixed read-only operations. No operation changes credentials or namespaces.
type HostQueries interface {
	Uname() (UnameInfo, error)
	NoNewPrivileges() (bool, error)
}
type LinuxHostQueries struct{}

func (LinuxHostQueries) Uname() (UnameInfo, error) {
	var value unix.Utsname
	if err := unix.Uname(&value); err != nil {
		return UnameInfo{}, err
	}
	return UnameInfo{Release: unix.ByteSliceToString(value.Release[:]), Version: unix.ByteSliceToString(value.Version[:]),
		Machine: unix.ByteSliceToString(value.Machine[:])}, nil
}

// NoNewPrivileges describes the calling OS thread, not every thread or another user.
func (LinuxHostQueries) NoNewPrivileges() (bool, error) {
	value, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
	return value == 1, err
}

// HostSnapshot is a value-owned offline transport response, including preserved errors.
type HostSnapshot struct {
	UnameResult    UnameInfo
	UnameError     error
	SecurityResult bool
	SecurityError  error
}

func (s HostSnapshot) Uname() (UnameInfo, error)      { return s.UnameResult, s.UnameError }
func (s HostSnapshot) NoNewPrivileges() (bool, error) { return s.SecurityResult, s.SecurityError }

func (r *ScopedOSReader) StatFS(ctx context.Context, subpath string) (FilesystemInfo, error) {
	var info unix.Statfs_t
	if err := ctx.Err(); err != nil {
		return FilesystemInfo{}, err
	}
	err := r.readSubpath(subpath, unix.O_PATH, func(fd int) error { return unix.Fstatfs(fd, &info) })
	return FilesystemInfo{Type: info.Type, NoSUID: info.Flags&unix.ST_NOSUID != 0,
		NoExec: info.Flags&unix.ST_NOEXEC != 0, FlagsKnown: err == nil}, err
}

// NewScopedMemReaderWithFilesystems snapshots explicit filesystem mount responses.
// Keys are resolved absolute mount paths; the longest ancestor mount supplies the type.
func NewScopedMemReaderWithFilesystems(root string, mem *MemPlatformReader, mounts map[string]FilesystemInfo) ScopedReader {
	r := NewScopedMemReader(root, mem).(*ScopedMemReader)
	r.filesystems = maps.Clone(mounts)
	return r
}
func (r *ScopedMemReader) StatFS(ctx context.Context, subpath string) (FilesystemInfo, error) {
	if err := r.validateRead(ctx, subpath); err != nil {
		return FilesystemInfo{}, err
	}
	_, resolved, err := r.mem.resolvePath(subpath, r.root, true, maxSymlinkDepthMemory)
	if err != nil {
		return FilesystemInfo{}, err
	}
	var result FilesystemInfo
	longest := -1
	for mount, info := range r.filesystems {
		if (resolved == mount || strings.HasPrefix(resolved, strings.TrimSuffix(mount, "/")+"/")) && len(mount) > longest {
			result, longest = info, len(mount)
		}
	}
	if longest < 0 {
		return result, ErrIncomplete
	}
	return result, nil
}

const HostVersionTimeout = 2 * time.Second

// HostMetadata exposes a single allowlisted command; callers cannot supply arguments or environment.
type HostMetadata struct{ runner CommandRunner }

func NewHostMetadata(runner CommandRunner) HostMetadata { return HostMetadata{runner: runner} }
func (m HostMetadata) SystemdVersion(ctx context.Context, executable string) (ExecResult, error) {
	if m.runner == nil || (executable != "/usr/bin/systemctl" && executable != "/bin/systemctl") {
		return ExecResult{}, errors.New("host metadata command unavailable")
	}
	return m.runner.Run(ctx, CommandSpec{Path: executable, Args: []string{"--version"}, Dir: "/", Timeout: HostVersionTimeout})
}

type hostQueriesView struct{ HostQueries }

func (e Environment) WithHost(queries HostQueries, metadata HostMetadata) Environment {
	if queries != nil {
		e.host = hostQueriesView{queries}
	}
	e.metadata = metadata
	return e
}
func (e Environment) Host() HostQueries          { return e.host }
func (e Environment) HostMetadata() HostMetadata { return e.metadata }
