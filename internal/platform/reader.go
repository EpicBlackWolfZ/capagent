package platform

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// maxSymlinkDepth bounds the number of symlink hops ReadFile and Stat may
// traverse before reporting syscall.ELOOP. The value mirrors the POSIX
// SYMLOOP_MAX convention used by the Linux kernel.
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
)

// VirtualFile is an in-memory filesystem node used exclusively by MemPlatformReader.
type VirtualFile struct {
	Kind    VirtualFileKind
	Content []byte
	Mode    os.FileMode
	Target  string
	// ForcedErr, when non-nil, is returned verbatim by ReadFile/Stat/ReadDir/Readlink.
	// This is used by tests to simulate EACCES, EIO, or any other POSIX failure.
	ForcedErr error
}

// memDirEntry is a lightweight os.DirEntry implementation backed by a VirtualFile.
type memDirEntry struct {
	name  string
	mode  os.FileMode
	kind  VirtualFileKind
	isDir bool
}

func (e *memDirEntry) Name() string      { return e.name }
func (e *memDirEntry) IsDir() bool       { return e.isDir }
func (e *memDirEntry) Type() os.FileMode { return e.kind.modeType() }
func (e *memDirEntry) Info() (os.FileInfo, error) {
	return nil, errors.New("memDirEntry.Info not implemented")
}

// memFileInfo is a lightweight os.FileInfo implementation backed by a VirtualFile.
type memFileInfo struct {
	name  string
	size  int64
	mode  os.FileMode
	isDir bool
}

func (i *memFileInfo) Name() string       { return i.name }
func (i *memFileInfo) Size() int64        { return i.size }
func (i *memFileInfo) Mode() os.FileMode  { return i.mode }
func (i *memFileInfo) ModTime() time.Time { return time.Time{} }
func (i *memFileInfo) IsDir() bool        { return i.isDir }
func (i *memFileInfo) Sys() any           { return nil }

func (k VirtualFileKind) modeType() os.FileMode {
	switch k {
	case FileKindDirectory:
		return os.ModeDir
	case FileKindSymlink:
		return os.ModeSymlink
	default:
		return 0
	}
}

// PlatformReader is the canonical filesystem abstraction for capagent probes.
//
// Implementations MUST emulate POSIX semantics for type mismatches
// (e.g. read on a directory returns EISDIR) and symlink loops (ELOOP).
// Implementations are NOT required to be safe for concurrent use; callers
// that need concurrent access must serialize calls externally.
type PlatformReader interface {
	ReadFile(path string) ([]byte, error)
	Stat(path string) (os.FileInfo, error)
	ReadDir(path string) ([]os.DirEntry, error)
	Readlink(path string) (string, error)
}

// OSPlatformReader is the production PlatformReader that delegates directly to
// the os package. Path arguments are resolved relative to the current process
// working directory.
type OSPlatformReader struct{}

// NewOSPlatformReader constructs a real OS-backed PlatformReader.
func NewOSPlatformReader() *OSPlatformReader {
	return &OSPlatformReader{}
}

// ReadFile reads the entire file at path and returns its bytes.
func (r *OSPlatformReader) ReadFile(path string) ([]byte, error) {
	return readFileFollowing(path, 0)
}

// Stat returns the FileInfo for path, following symlinks.
func (r *OSPlatformReader) Stat(path string) (os.FileInfo, error) {
	return statFollowing(path, 0)
}

// ReadDir returns the sorted directory entries at path.
func (r *OSPlatformReader) ReadDir(path string) ([]os.DirEntry, error) {
	return os.ReadDir(path)
}

// Readlink returns the symlink target without resolving it.
func (r *OSPlatformReader) Readlink(path string) (string, error) {
	return os.Readlink(path)
}

// readFileFollowing performs os.Stat-then-ReadFile while resolving symlinks
// up to maxSymlinkDepth hops. A loop or excessive depth surfaces as syscall.ELOOP.
func readFileFollowing(path string, depth int) ([]byte, error) {
	if depth > maxSymlinkDepth {
		return nil, syscall.ELOOP
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, lerr := os.Readlink(path)
		if lerr != nil {
			return nil, lerr
		}
		return readFileFollowing(resolveAgainstDir(path, target), depth+1)
	}
	if info.IsDir() {
		return nil, syscall.EISDIR
	}
	// PlatformReader abstracts arbitrary path reads by design; callers are
	// responsible for input sanitisation.
	return os.ReadFile(path) //nolint:gosec // G304: PlatformReader is a path abstraction layer.
}

// statFollowing resolves symlinks and returns the FileInfo of the target.
func statFollowing(path string, depth int) (os.FileInfo, error) {
	if depth > maxSymlinkDepth {
		return nil, syscall.ELOOP
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, lerr := os.Readlink(path)
		if lerr != nil {
			return nil, lerr
		}
		return statFollowing(resolveAgainstDir(path, target), depth+1)
	}
	return info, nil
}

// resolveAgainstDir resolves a symlink target against the directory of path.
// Absolute targets are returned verbatim; relative targets are joined with
// filepath.Dir(path) to honor POSIX semantics.
func resolveAgainstDir(path, target string) string {
	if filepath.IsAbs(target) {
		return filepath.Clean(target)
	}
	return filepath.Join(filepath.Dir(path), target)
}

// MemPlatformReader is an in-memory PlatformReader implementation used as a
// deterministic test double. It stores a flat map keyed by filepath.Clean
// absolute or relative paths to VirtualFile entries.
//
// MemPlatformReader is safe for concurrent use.
type MemPlatformReader struct {
	mu    sync.RWMutex
	files map[string]*VirtualFile
}

// NewMemPlatformReader constructs an empty in-memory PlatformReader.
func NewMemPlatformReader() *MemPlatformReader {
	return &MemPlatformReader{files: make(map[string]*VirtualFile)}
}

// normalize canonicalizes a path for storage and lookup. Empty input is
// rejected; otherwise filepath.Clean is applied.
func normalize(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path cannot be empty")
	}
	return filepath.Clean(path), nil
}

// AddFile inserts a regular file at path with the provided content and mode.
func (m *MemPlatformReader) AddFile(path string, content []byte, mode os.FileMode) {
	clean, err := normalize(path)
	if err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[clean] = &VirtualFile{
		Kind:    FileKindRegular,
		Content: content,
		Mode:    mode.Perm(),
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
		Mode: mode.Perm() | os.ModeDir,
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

// resolveSymlinkChain walks the symlink chain at clean, returning the final
// resolved entry plus a "found" boolean. The traversal applies ForcedErr
// semantics at every node it visits: an entry with ForcedErr set aborts
// resolution with that error before any further hop is attempted.
//
// The boolean indicates whether the final resolved path was located in the
// virtual tree. When depth exceeds maxSymlinkDepth, syscall.ELOOP is returned.
//
// Callers MUST treat ForcedErr as authoritative: even if the entry is itself
// a symlink, the forced error is reported rather than being masked by the
// chain's final target.
func (m *MemPlatformReader) resolveSymlinkChain(clean string, depth int) (*VirtualFile, string, bool, error) {
	if depth > maxSymlinkDepth {
		return nil, "", false, syscall.ELOOP
	}
	m.mu.RLock()
	entry, ok := m.files[clean]
	m.mu.RUnlock()
	if !ok {
		return nil, clean, false, nil
	}
	if entry.ForcedErr != nil {
		return nil, "", false, entry.ForcedErr
	}
	if entry.Kind != FileKindSymlink {
		return entry, clean, true, nil
	}
	next := filepath.Clean(filepath.Join(filepath.Dir(clean), entry.Target))
	return m.resolveSymlinkChain(next, depth+1)
}

// ReadFile returns the content of the file at path. Directories return
// syscall.EISDIR; symlinks are followed up to maxSymlinkDepth hops; cycles
// surface as syscall.ELOOP. ForcedErr at any node of the chain aborts with
// that error verbatim.
func (m *MemPlatformReader) ReadFile(path string) ([]byte, error) {
	clean, err := normalize(path)
	if err != nil {
		return nil, os.ErrNotExist
	}
	entry, resolved, exists, lerr := m.resolveSymlinkChain(clean, 0)
	if lerr != nil {
		return nil, lerr
	}
	if !exists {
		return nil, os.ErrNotExist
	}
	_ = resolved
	if entry.Kind == FileKindDirectory {
		return nil, syscall.EISDIR
	}
	return append([]byte(nil), entry.Content...), nil
}

// Stat returns FileInfo for path following symlinks. Symlink loops return
// syscall.ELOOP; non-symlink entries use the stored mode and content size.
// ForcedErr at any node of the chain aborts with that error verbatim.
func (m *MemPlatformReader) Stat(path string) (os.FileInfo, error) {
	clean, err := normalize(path)
	if err != nil {
		return nil, os.ErrNotExist
	}
	entry, resolved, exists, lerr := m.resolveSymlinkChain(clean, 0)
	if lerr != nil {
		return nil, lerr
	}
	if !exists {
		return nil, os.ErrNotExist
	}
	return &memFileInfo{
		name:  filepath.Base(resolved),
		size:  int64(len(entry.Content)),
		mode:  entry.Mode,
		isDir: entry.Kind == FileKindDirectory,
	}, nil
}

// ReadDir enumerates sorted child entries of the directory at path. Calling
// ReadDir on a regular file or symlink-to-file returns syscall.ENOTDIR;
// missing entries return os.ErrNotExist. ForcedErr at any node of the chain
// aborts with that error verbatim.
func (m *MemPlatformReader) ReadDir(path string) ([]os.DirEntry, error) {
	clean, err := normalize(path)
	if err != nil {
		return nil, os.ErrNotExist
	}
	entry, resolved, exists, lerr := m.resolveSymlinkChain(clean, 0)
	if lerr != nil {
		return nil, lerr
	}
	if !exists {
		return nil, os.ErrNotExist
	}
	if entry.Kind != FileKindDirectory {
		return nil, syscall.ENOTDIR
	}
	prefix := resolved
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make(map[string]struct{})
	for stored := range m.files {
		if stored == resolved {
			continue
		}
		if !strings.HasPrefix(stored, prefix) {
			continue
		}
		rel := strings.TrimPrefix(stored, prefix)
		if rel == "" || strings.Contains(rel, string(filepath.Separator)) {
			continue
		}
		names[rel] = struct{}{}
	}
	out := make([]os.DirEntry, 0, len(names))
	for n := range names {
		childPath := filepath.Join(resolved, n)
		child := m.files[childPath]
		if child == nil {
			continue
		}
		out = append(out, &memDirEntry{
			name:  n,
			mode:  child.Mode,
			kind:  child.Kind,
			isDir: child.Kind == FileKindDirectory,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

// Readlink returns the raw symlink target stored at path without dereferencing
// it. Calling Readlink on a non-symlink returns syscall.EINVAL. ForcedErr on
// the symlink entry itself aborts with that error verbatim.
func (m *MemPlatformReader) Readlink(path string) (string, error) {
	clean, err := normalize(path)
	if err != nil {
		return "", os.ErrNotExist
	}
	m.mu.RLock()
	entry, ok := m.files[clean]
	m.mu.RUnlock()
	if !ok {
		return "", os.ErrNotExist
	}
	if entry.ForcedErr != nil {
		return "", entry.ForcedErr
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
