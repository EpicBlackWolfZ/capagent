package platform

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
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

// size reports deterministic virtual metadata; directory sizes remain zero.
func (v *VirtualFile) size() int64 {
	if v.Kind == FileKindSymlink {
		return int64(len(v.Target))
	}
	return int64(len(v.Content))
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

// PlatformReader is the canonical filesystem abstraction for capagent probes.
//
// Implementations MUST emulate POSIX semantics for type mismatches
// (e.g. read on a directory returns EISDIR) and symlink loops (ELOOP).
// ReadDir returns sorted eager metadata; only disappearing children (ENOENT)
// may be skipped. Other metadata failures return no entries and a wrapped error.
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
	// Preserve kernel pathname resolution, including dot-dot within link targets.
	return os.ReadFile(path) //nolint:gosec // Trusted host paths; untrusted subpaths use ScopedReader.
}

// Stat returns the FileInfo for path, following symlinks.
func (r *OSPlatformReader) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

// ReadDir returns the sorted directory entries at path.
func (r *OSPlatformReader) ReadDir(path string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	byName := make(map[string]os.DirEntry, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
		byName[entry.Name()] = entry
	}
	return captureDirectory(names, func(name string) (os.FileInfo, error) { return byName[name].Info() })
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

// ReadFile returns the content of the file at path. Directories return
// syscall.EISDIR; symlinks are followed up to maxSymlinkDepth hops; cycles
// surface as syscall.ELOOP. ForcedErr at any node of the chain aborts with
// that error verbatim.
func (m *MemPlatformReader) ReadFile(path string) ([]byte, error) {
	if path == "" {
		return nil, os.ErrNotExist
	}
	entry, _, lerr := m.resolvePath(path, "", true, maxSymlinkDepth)
	if lerr != nil {
		return nil, lerr
	}
	if entry.Kind == FileKindDirectory {
		return nil, syscall.EISDIR
	}
	return append([]byte(nil), entry.Content...), nil
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
	return &memFileInfo{
		name:  filepath.Base(path),
		size:  entry.size(),
		mode:  entry.Mode,
		isDir: entry.Kind == FileKindDirectory,
	}, nil
}

// ReadDir enumerates sorted child entries of the directory at path. Calling
// ReadDir on a regular file or symlink-to-file returns syscall.ENOTDIR;
// missing entries return os.ErrNotExist. ForcedErr at any node of the chain
// aborts with that error verbatim.
func (m *MemPlatformReader) ReadDir(path string) ([]os.DirEntry, error) {
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
	return m.captureChildren(resolved)
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
		if v.Content != nil {
			copyVF.Content = append([]byte(nil), v.Content...)
		}
		out[k] = &copyVF
	}
	return out
}

// captureChildren holds the map lock until all child metadata has been copied.
func (m *MemPlatformReader) captureChildren(dir string) ([]os.DirEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	prefix := strings.TrimRight(dir, "/") + "/"
	if dir == "." {
		prefix = ""
	}
	names := make([]string, 0)
	for path := range m.files {
		if path == dir || !strings.HasPrefix(path, prefix) {
			continue
		}
		name := strings.TrimPrefix(path, prefix)
		if name != "" && !strings.Contains(name, "/") {
			names = append(names, name)
		}
	}
	return captureDirectory(names, func(name string) (os.FileInfo, error) {
		node := m.files[filepath.Join(dir, name)]
		if node.ForcedErr != nil {
			return nil, node.ForcedErr
		}
		return &memFileInfo{name: name, size: node.size(), mode: node.Mode, isDir: node.Kind == FileKindDirectory}, nil
	})
}
