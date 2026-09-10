package platform

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// maxSymlinkDepth bounds the number of symlink hops ReadFile and Stat may
// traverse before reporting syscall.ELOOP. This memory limit is lower
// than the Linux kernel pathname resolution limit of 40 hops.
const maxSymlinkDepth = 16

// defaultSymlinkMode is the mode bitmask used for newly created symlink
// entries. POSIX specifies 0o777 (rwxrwxrwx) as the symbolic-link mode
// because actual permissions are governed by the target's mode.
const defaultSymlinkMode = 0o777

// VirtualFileKind classifies an entry in the MemPlatformReader virtual tree.
type VirtualFileKind uint8

const (
	// FileKindRegular represents a regular file with byte content and a POSIX mode.
	FileKindRegular VirtualFileKind = iota
	// FileKindDirectory represents a directory entry whose content is
	// enumerated as child names.
	FileKindDirectory
	// FileKindSymlink represents a symbolic link holding an unevaluated target path.
	FileKindSymlink
	// FileKindSpecial represents a non-readable special node.
	FileKindSpecial
)

// VirtualFile is an in-memory filesystem node used exclusively by MemPlatformReader.
type VirtualFile struct {
	Ownership      FileOwnership
	OwnershipKnown bool
	Capabilities   CapabilityAttribute
	CapabilityErr  error
	Kind           VirtualFileKind
	Content        []byte
	Mode           os.FileMode
	Target         string
	// ForcedErr, when non-nil, is returned verbatim by ReadFile/Stat/ReadDir/Readlink.
	// This is used by tests to simulate EACCES, EIO, or any other POSIX failure.
	ForcedErr error
}

// size reports deterministic virtual metadata; directory sizes remain zero.
func (v *VirtualFile) size() int64 {
	if v.Kind == FileKindSymlink {
		return int64(len(v.Target))
	}
	return int64(len(v.Content))
}

// memFileInfo is a lightweight os.FileInfo implementation backed by a VirtualFile.
type memFileInfo struct {
	name     string
	size     int64
	mode     os.FileMode
	isDir    bool
	metadata fileMetadata
}

func (i *memFileInfo) Name() string       { return i.name }
func (i *memFileInfo) Size() int64        { return i.size }
func (i *memFileInfo) Mode() os.FileMode  { return i.mode }
func (i *memFileInfo) ModTime() time.Time { return time.Time{} }
func (i *memFileInfo) IsDir() bool        { return i.isDir }
func (i *memFileInfo) Sys() any           { return i.metadata }

// PlatformReader is the canonical filesystem abstraction for capagent probes.
//
// Implementations MUST emulate POSIX semantics for type mismatches
// (e.g. read on a directory returns EISDIR) and symlink loops (ELOOP).
// ReadDir returns sorted eager metadata; only disappearing children (ENOENT)
// may be skipped. Other metadata failures return no entries and a wrapped error.
// ReadFile and ReadDir accept non-nil contexts and may return partial data with
// an error. Cancellation is cooperative between syscalls, not a kernel deadline.
// FileCapabilities returns bounded raw xattr data, not an effective-privilege claim.
// Implementations are NOT required to be safe for concurrent use; callers
// that need concurrent access must serialize calls externally.
type PlatformReader interface {
	ReadFile(ctx context.Context, path string) ([]byte, error)
	Stat(path string) (os.FileInfo, error)
	ReadDir(ctx context.Context, path string) ([]os.DirEntry, error)
	Readlink(path string) (string, error)
	FileCapabilities(context.Context, string) (CapabilityAttribute, error)
}

// OSPlatformReader reads explicitly trusted host paths through bounded descriptor
// operations. Relative paths use the current working directory. Untrusted subpaths
// must use ScopedReader instead. Construct readers with NewOSPlatformReader.
type OSPlatformReader struct{ limits ReadLimits }

// NewOSPlatformReader constructs a real OS-backed PlatformReader.
func NewOSPlatformReader() *OSPlatformReader {
	return &OSPlatformReader{limits: DefaultReadLimits()}
}

// NewOSPlatformReaderWithLimits constructs a reader with validated immutable limits.
func NewOSPlatformReaderWithLimits(limits ReadLimits) (*OSPlatformReader, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}
	return &OSPlatformReader{limits: limits}, nil
}

// ReadFile returns a bounded prefix and an error if the source is incomplete.
func (r *OSPlatformReader) ReadFile(ctx context.Context, path string) ([]byte, error) {
	var data []byte
	err := checkedRegular(ctx, func(flags uint64, fn func(int) error) error { return withHostFD(path, flags, fn) }, func(fd int) error {
		var err error
		data, err = readAllFD(ctx, fd, r.limits.FileBytes)
		return err
	})
	return data, pathError("read", path, err)
}

// Stat returns the FileInfo for path, following symlinks.
func (r *OSPlatformReader) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

// ReadDir returns the sorted directory entries at path.
func (r *OSPlatformReader) ReadDir(ctx context.Context, path string) ([]os.DirEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var entries []os.DirEntry
	err := withHostFD(path, unix.O_RDONLY|unix.O_DIRECTORY, func(fd int) error {
		var err error
		entries, err = readDirectoryFD(ctx, fd, r.limits)
		return err
	})
	return entries, pathError("readdir", path, err)
}

// Readlink returns the symlink target without resolving it.
func (r *OSPlatformReader) Readlink(path string) (string, error) {
	return os.Readlink(path)
}

// MemPlatformReader is an in-memory PlatformReader implementation used as a
// deterministic test double. It stores a flat map keyed by filepath.Clean
// absolute or relative paths to VirtualFile entries.
//
// Lookup traverses components without cleaning away symlink semantics. Parent
// directories must be present. Relative keys form a virtual namespace and never
// consult the process working directory. MemPlatformReader is safe for concurrent use.
type MemPlatformReader struct {
	mu     sync.RWMutex
	files  map[string]*VirtualFile
	limits ReadLimits
}

// NewMemPlatformReader constructs an empty in-memory PlatformReader.
func NewMemPlatformReader() *MemPlatformReader {
	return &MemPlatformReader{files: make(map[string]*VirtualFile), limits: DefaultReadLimits()}
}

// normalize canonicalizes a path for storage and lookup. Empty input is
// rejected; otherwise filepath.Clean is applied.
func normalize(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path cannot be empty")
	}
	return filepath.Clean(path), nil
}

// AddFile inserts a regular file at path with a copy of content and the provided mode.
func (m *MemPlatformReader) AddFile(path string, content []byte, mode os.FileMode) {
	clean, err := normalize(path)
	if err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[clean] = &VirtualFile{
		Kind:    FileKindRegular,
		Content: append([]byte(nil), content...),
		Mode:    mode & (os.ModePerm | privilegeBits),
	}
}

// AddDir inserts a directory entry at path with the provided mode. Directories
// are virtual only: their child entries are discovered by scanning the flat map.
func (m *MemPlatformReader) AddDir(path string, mode os.FileMode) {
	clean, err := normalize(path)
	if err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[clean] = &VirtualFile{
		Kind: FileKindDirectory,
		Mode: mode&(os.ModePerm|privilegeBits) | os.ModeDir,
	}
}

// AddSymlink inserts a symbolic link at path pointing at target. The target is
// stored verbatim and only evaluated on dereferencing operations.
//
// The default symlink mode follows POSIX convention (0o777, world-rwx
// subject to umask); the symbolic-link type bits are always set.
func (m *MemPlatformReader) AddSymlink(path, target string) {
	clean, err := normalize(path)
	if err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[clean] = &VirtualFile{
		Kind:   FileKindSymlink,
		Mode:   defaultSymlinkMode | os.ModeSymlink,
		Target: target,
	}
}

// AddError injects a forced error at path, simulating permission denials or
// I/O failures for any subsequent ReadFile/Stat/ReadDir/Readlink invocation.
func (m *MemPlatformReader) AddError(path string, err error) {
	clean, perr := normalize(path)
	if perr != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[clean] = &VirtualFile{
		Kind:      FileKindRegular,
		ForcedErr: err,
	}
}

// NewMemPlatformReaderWithLimits constructs a fixture reader with immutable limits.
func NewMemPlatformReaderWithLimits(limits ReadLimits) (*MemPlatformReader, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}
	return &MemPlatformReader{files: make(map[string]*VirtualFile), limits: limits}, nil
}

// ReadFile follows fixture links and returns bounded copied data.
func (m *MemPlatformReader) ReadFile(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if path == "" {
		return nil, os.ErrNotExist
	}
	entry, _, err := m.resolvePath(path, "", true, maxSymlinkDepth)
	if err != nil {
		return nil, err
	}
	return readVirtual(ctx, entry, m.limits.FileBytes)
}

// Stat returns FileInfo for path following symlinks. Symlink loops return
// syscall.ELOOP; non-symlink entries use the stored mode and content size.
// ForcedErr at any node of the chain aborts with that error verbatim.
func (m *MemPlatformReader) Stat(path string) (os.FileInfo, error) {
	if path == "" {
		return nil, os.ErrNotExist
	}
	entry, _, lerr := m.resolvePath(path, "", true, maxSymlinkDepth)
	if lerr != nil {
		return nil, lerr
	}
	return virtualInfo(filepath.Base(path), entry), nil
}

// ReadDir enumerates sorted child entries of the directory at path. Calling
// ReadDir on a regular file or symlink-to-file returns syscall.ENOTDIR;
// missing entries return os.ErrNotExist. ForcedErr at any node of the chain
// aborts with that error verbatim.
func (m *MemPlatformReader) ReadDir(ctx context.Context, path string) ([]os.DirEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if path == "" {
		return nil, os.ErrNotExist
	}
	entry, resolved, lerr := m.resolvePath(path, "", true, maxSymlinkDepth)
	if lerr != nil {
		return nil, lerr
	}
	if entry.Kind != FileKindDirectory {
		return nil, syscall.ENOTDIR
	}
	return m.captureChildren(ctx, resolved, m.limits)
}

// Readlink returns the raw symlink target stored at path without dereferencing
// it. Calling Readlink on a non-symlink returns syscall.EINVAL. ForcedErr on
// the symlink entry itself aborts with that error verbatim.
func (m *MemPlatformReader) Readlink(path string) (string, error) {
	if path == "" {
		return "", os.ErrNotExist
	}
	entry, _, err := m.resolvePath(path, "", false, maxSymlinkDepth)
	if err != nil {
		return "", err
	}
	if entry.Kind != FileKindSymlink {
		return "", syscall.EINVAL
	}
	return entry.Target, nil
}

// Compile-time guarantees that both readers satisfy the interface.
var (
	_ PlatformReader = (*OSPlatformReader)(nil)
	_ PlatformReader = (*MemPlatformReader)(nil)
	_ fs.FileInfo    = (*memFileInfo)(nil)
)

// Snapshot returns a copy of the in-memory file tree. The returned map is
// safe for the caller to mutate without affecting the underlying state.
// Tests use this to materialize the tree onto a temporary directory for
// the OS-backed ScopedReader parity path.
func (m *MemPlatformReader) Snapshot() map[string]*VirtualFile {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]*VirtualFile, len(m.files))
	for k, v := range m.files {
		copyVF := *v
		copyVF.Capabilities.Bytes = append([]byte(nil), v.Capabilities.Bytes...)
		if v.Content != nil {
			copyVF.Content = append([]byte(nil), v.Content...)
		}
		out[k] = &copyVF
	}
	return out
}
