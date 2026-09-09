package platform

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// maxSymlinkDepthMemory is the per-op application-level symlink hop counter
// used by ScopedMemReader. It bounds explicit symlink-chain walks before
// reporting syscall.ELOOP. The OS reader delegates traversal to the kernel,
// which uses its own SYMLOOP_MAX (40 hops on Linux 4.2+).
const maxSymlinkDepthMemory = 16

// permMask is the POSIX permission bitmask (rwx for owner/group/other)
// applied when converting unix.Stat_t mode to os.FileMode.
const permMask = 0o777

// ioChunkBytes is the buffer size for unix.Read / unix.Getdents loops.
// 4 KiB matches the typical page size and balances syscall overhead
// against transient buffer allocation cost.
const ioChunkBytes = 4096

// ErrSymlinkUnsupported is returned by NewScopedOSReader when openat2(2)
// with RESOLVE_IN_ROOT is unavailable. This happens on pre-5.6 kernels
// (the syscall returns ENOSYS) and on hosts where the underlying security
// boundary cannot be established.
var ErrSymlinkUnsupported = errors.New("platform: openat2 with RESOLVE_IN_ROOT requires Linux 5.6+")

// ErrClosed is returned by ScopedReader methods after the reader has been
// closed. Root() continues to work after Close.
var ErrClosed = errors.New("platform: ScopedReader is closed")

// sentinelClosedRootFD is the value of ScopedOSReader.fd once Close has
// run. Any other value (including zero) is a valid file descriptor.
//
// FD 0 is a legal kernel-returned descriptor; a process with stdin
// closed can legitimately receive FD 0 from openat2(2). The sentinel
// therefore MUST NOT be 0.
const sentinelClosedRootFD = int64(-1)

// ScopedReader is the filesystem abstraction that operates under a declared
// root. All four file methods accept subpaths relative to the root; the
// reader validates its own subpath arguments via ValidateSubpath before
// acquiring any file descriptor.
//
// Implementations:
//
//   - ScopedOSReader: kernel-backed via openat2(2) on Linux 5.6+.
//   - ScopedMemReader: in-memory with explicit escape checks and a
//     maxSymlinkDepthMemory-hop application-level counter.
//
// A ScopedReader is the security boundary: callers cannot bypass containment
// by forgetting to validate, because validation lives inside the interface.
//
// Concurrency contract:
//
//   - ReadFile, Stat, ReadDir, Readlink may run concurrently with each
//     other and with operations on other ScopedReader instances.
//   - Close must NOT be called concurrently with any other method on the
//     same reader.
//   - After Close returns, all four file methods return ErrClosed.
//   - Root() remains valid after Close.
//   - Close is idempotent: subsequent calls return nil.
type ScopedReader interface {
	ReadFile(subpath string) ([]byte, error)
	Stat(subpath string) (os.FileInfo, error)
	ReadDir(subpath string) ([]os.DirEntry, error)
	Readlink(subpath string) (string, error)
	Root() string
	Close() error
}

// Compile-time check that the helpers below satisfy the interface.
var (
	_ ScopedReader = (*ScopedOSReader)(nil)
	_ ScopedReader = (*ScopedMemReader)(nil)
)

// scopedFileInfo is an immutable, eagerly-captured os.FileInfo used by
// ScopedReader.ReadDir. Once constructed, no host I/O is required to
// satisfy the FileInfo methods; this keeps DirEntry.Info() host-I/O free.
type scopedFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	isDir   bool
}

func (fi *scopedFileInfo) Name() string       { return fi.name }
func (fi *scopedFileInfo) Size() int64        { return fi.size }
func (fi *scopedFileInfo) Mode() os.FileMode  { return fi.mode }
func (fi *scopedFileInfo) ModTime() time.Time { return fi.modTime }
func (fi *scopedFileInfo) IsDir() bool        { return fi.isDir }
func (fi *scopedFileInfo) Sys() any           { return nil }

// scopedDirEntry is the os.DirEntry returned by ScopedReader.ReadDir.
// Info() returns the eagerly-captured FileInfo without performing host I/O.
type scopedDirEntry struct {
	name string
	info *scopedFileInfo
}

func (de *scopedDirEntry) Name() string { return de.name }
func (de *scopedDirEntry) IsDir() bool  { return de.info.IsDir() }
func (de *scopedDirEntry) Type() os.FileMode {
	return de.info.Mode().Type()
}
func (de *scopedDirEntry) Info() (os.FileInfo, error) {
	return de.info, nil
}

// scopedStatMode converts a unix.Stat_t mode field to an os.FileMode with
// POSIX type bits preserved. Permissions are limited to the low 9 bits
// (rwx for owner/group/other); the file type bits come from the high bits.
func scopedStatMode(mode uint32) os.FileMode {
	out := os.FileMode(mode & permMask)
	switch mode & unix.S_IFMT {
	case unix.S_IFDIR:
		out |= os.ModeDir
	case unix.S_IFLNK:
		out |= os.ModeSymlink
	}
	return out
}

// -----------------------------------------------------------------------------
// Linux implementation: ScopedOSReader
// -----------------------------------------------------------------------------

// ScopedOSReader is the production ScopedReader backed by openat2(2) with
// RESOLVE_IN_ROOT | RESOLVE_NO_MAGICLINKS.
//
// FD-ownership model:
//
//   - rootfd: stored as int64 in atomic.Int64 with sentinel -1 meaning
//     "closed". Any non-sentinel value (including 0) is a valid
//     descriptor returned by the kernel. The constructor stores the
//     kernel-returned value verbatim; Close performs a CAS swap to
//     the sentinel and closes exactly once.
//
//   - per-operation FD: openat2(rootfd, subpath, flags) inside
//     readSubpath; ownership is anchored to the readSubpath scope via a
//     defer that calls unix.Close. Inner callbacks MUST NOT close the
//     FD and MUST NOT wrap it in *os.File (whose Close would compete
//     with the defer). Callbacks perform fd-relative I/O via the
//     unix.* syscalls directly.
//
// Exactly one close path per FD is therefore mechanically guaranteed;
// double-close is impossible by construction.
type ScopedOSReader struct {
	fd   atomic.Int64
	root string
}

// NewScopedOSReader opens root once via openat2(AT_FDCWD, root, O_PATH|O_DIRECTORY|O_CLOEXEC,
// RESOLVE_NO_MAGICLINKS) and returns a ScopedReader rooted at the resulting
// inode. The rootfd is held until Close is called.
//
// On pre-5.6 kernels or where openat2(2) is unavailable, NewScopedOSReader
// returns ErrSymlinkUnsupported so the caller can fail fast.
func NewScopedOSReader(root string) (ScopedReader, error) {
	if root == "" {
		return nil, errors.New("platform: root path cannot be empty")
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, root, &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return nil, mapOpenError(err, root)
	}
	r := &ScopedOSReader{root: root}
	// Store the kernel-returned FD verbatim. FD 0 is valid; the
	// sentinel -1 is reserved for the closed state.
	r.fd.Store(int64(fd))
	return r, nil
}

// Root returns the configured root path string.
func (r *ScopedOSReader) Root() string { return r.root }

// rootFD returns the live root FD or ErrClosed if Close has run.
// The returned FD is owned by the ScopedOSReader; callers MUST NOT
// close it.
func (r *ScopedOSReader) rootFD() (int, error) {
	fd := r.fd.Load()
	if fd == sentinelClosedRootFD {
		return 0, ErrClosed
	}
	return int(fd), nil
}

// checkOpen returns ErrClosed if Close has been called.
func (r *ScopedOSReader) checkOpen() error {
	if r.fd.Load() == sentinelClosedRootFD {
		return ErrClosed
	}
	return nil
}

// Close releases the root FD. Idempotent: subsequent calls return nil.
// After Close, all four file methods return ErrClosed; Root() remains valid.
//
// The CAS loop ensures exactly one Close wins the FD-release race; the
// losing concurrent Close observes the sentinel and returns nil without
// calling unix.Close on the already-closed descriptor.
func (r *ScopedOSReader) Close() error {
	for {
		old := r.fd.Load()
		if old == sentinelClosedRootFD {
			return nil
		}
		if r.fd.CompareAndSwap(old, sentinelClosedRootFD) {
			_ = unix.Close(int(old))
			return nil
		}
	}
}

// readSubpath opens a per-op FD via openat2(rootfd, subpath, flags) and
// invokes fn(fd). The flag set is supplied by the caller (O_RDONLY,
// O_PATH, O_RDONLY|O_DIRECTORY, etc.) with O_CLOEXEC added atomically;
// Resolve is always RESOLVE_IN_ROOT |
// RESOLVE_NO_MAGICLINKS.
//
// FD ownership: readSubpath is the SOLE owner of the per-operation FD.
// The defer in this function is the ONLY close path; callbacks MUST NOT
// close the FD and MUST NOT assign it to *os.File. Callbacks perform
// fd-relative I/O via direct unix syscalls (read, fstat, fstatat,
// readlinkat, getdents).
func (r *ScopedOSReader) readSubpath(subpath string, flags uint64, fn func(fd int) error) error {
	if err := r.checkOpen(); err != nil {
		return err
	}
	if err := ValidateSubpath(subpath); err != nil {
		return err
	}
	rootfd, err := r.rootFD()
	if err != nil {
		return err
	}
	fd, err := unix.Openat2(rootfd, subpath, &unix.OpenHow{
		Flags:   flags | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_IN_ROOT | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return mapOpenError(err, subpath)
	}
	defer func() { _ = unix.Close(fd) }()
	return fn(fd)
}

// ReadFile opens subpath with O_RDONLY and reads all bytes via unix.Read.
// Per-operation FD is owned and closed by readSubpath; the callback
// performs fd-relative reads and returns the assembled buffer.
func (r *ScopedOSReader) ReadFile(subpath string) ([]byte, error) {
	var data []byte
	err := r.readSubpath(subpath, unix.O_RDONLY, func(fd int) error {
		d, ferr := readAllFD(fd)
		if ferr != nil {
			return ferr
		}
		data = d
		return nil
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

// readAllFD drains fd via repeated unix.Read until EOF. The caller owns
// fd and is responsible for closing it.
//
// ScopedReader never opens FDs in non-blocking mode, so unix.Read
// cannot return EAGAIN for these descriptors; the loop terminates
// only on EOF (n == 0) or on a hard error.
func readAllFD(fd int) ([]byte, error) {
	var buf []byte
	tmp := make([]byte, ioChunkBytes)
	for {
		n, err := unix.Read(fd, tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			return buf, err
		}
		if n == 0 {
			return buf, nil
		}
	}
}

// Stat opens subpath with O_PATH and returns FileInfo built from fstat(2).
func (r *ScopedOSReader) Stat(subpath string) (os.FileInfo, error) {
	var info os.FileInfo
	err := r.readSubpath(subpath, unix.O_PATH, func(fd int) error {
		var st unix.Stat_t
		if lerr := unix.Fstat(fd, &st); lerr != nil {
			return lerr
		}
		info = &scopedFileInfo{
			name:    filepath.Base(subpath),
			size:    st.Size,
			mode:    scopedStatMode(st.Mode),
			modTime: time.Unix(st.Mtim.Sec, st.Mtim.Nsec),
			isDir:   st.Mode&unix.S_IFMT == unix.S_IFDIR,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return info, nil
}

// ReadDir opens subpath with O_RDONLY|O_DIRECTORY, enumerates children via
// unix.Getdents + unix.ParseDirent, and re-stats each child with
// AT_SYMLINK_NOFOLLOW so child symlinks are not followed.
//
// Per-operation FD is owned and closed by readSubpath; the callback
// performs fd-relative enumeration via direct syscalls. No *os.File is
// constructed.
func (r *ScopedOSReader) ReadDir(subpath string) ([]os.DirEntry, error) {
	var entries []os.DirEntry
	err := r.readSubpath(subpath, unix.O_RDONLY|unix.O_DIRECTORY, func(fd int) error {
		names, gerr := readDirNames(fd)
		if gerr != nil {
			return gerr
		}
		out := make([]os.DirEntry, 0, len(names))
		for _, name := range names {
			// RESOLVE_IN_ROOT is per-open; filtering "." and ".."
			// before any fstatat prevents accidentally reaching
			// dirFd's parent (which is outside root).
			if name == "." || name == ".." {
				continue
			}
			var st unix.Stat_t
			if serr := unix.Fstatat(fd, name, &st, unix.AT_SYMLINK_NOFOLLOW); serr != nil {
				// Entry renamed or deleted between enumeration
				// and stat. Skip silently; the security invariant
				// holds because AT_SYMLINK_NOFOLLOW prevents
				// following the entry's symlink target even if it
				// was atomically replaced.
				continue
			}
			out = append(out, &scopedDirEntry{
				name: name,
				info: &scopedFileInfo{
					name:    name,
					size:    st.Size,
					mode:    scopedStatMode(st.Mode),
					modTime: time.Unix(st.Mtim.Sec, st.Mtim.Nsec),
					isDir:   st.Mode&unix.S_IFMT == unix.S_IFDIR,
				},
			})
		}
		entries = out
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// readDirNames enumerates the directory entries reachable via fd.
// The caller owns fd and is responsible for closing it.
//
// ScopedReader never opens FDs in non-blocking mode, so unix.Getdents
// cannot return EAGAIN for these descriptors.
func readDirNames(fd int) ([]string, error) {
	var names []string
	tmp := make([]byte, ioChunkBytes)
	for {
		n, err := unix.Getdents(fd, tmp)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return names, nil
		}
		_, _, parsed := unix.ParseDirent(tmp[:n], -1, names)
		names = parsed
	}
}

// Readlink opens subpath with O_PATH|O_NOFOLLOW (which yields an FD
// referring to the symlink itself, per open(2)), then readlinkat(fd, "")
// returns the raw stored. The target is not validated; callers
// interpret it.
func (r *ScopedOSReader) Readlink(subpath string) (string, error) {
	var target string
	err := r.readSubpath(subpath, unix.O_PATH|unix.O_NOFOLLOW, func(fd int) error {
		buf := make([]byte, unix.PathMax)
		n, lerr := unix.Readlinkat(fd, "", buf)
		if lerr != nil {
			return lerr
		}
		target = string(buf[:n])
		return nil
	})
	if err != nil {
		return "", err
	}
	return target, nil
}

// mapOpenError converts a unix.Openat2 error into a Go error suitable for
// the ScopedReader contract. EXDEV is mapped to ErrSubpathEscape; ELOOP
// (magic-link rejection via RESOLVE_NO_MAGICLINKS, or kernel symlink cap)
// is preserved as syscall.ELOOP; ENOSYS maps to ErrSymlinkUnsupported.
//
// Per the kickoff prompt's semantic correction, magic-link rejection under
// RESOLVE_NO_MAGICLINKS is surfaced as syscall.ELOOP, not ErrSubpathEscape.
func mapOpenError(err error, subpath string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, unix.ENOENT) {
		return &os.PathError{Op: "openat2", Path: subpath, Err: os.ErrNotExist}
	}
	if errors.Is(err, unix.EACCES) {
		return &os.PathError{Op: "openat2", Path: subpath, Err: os.ErrPermission}
	}
	if errors.Is(err, unix.EEXIST) {
		return &os.PathError{Op: "openat2", Path: subpath, Err: os.ErrExist}
	}
	if errors.Is(err, unix.EXDEV) {
		return ErrSubpathEscape
	}
	if errors.Is(err, unix.ELOOP) {
		return syscall.ELOOP
	}
	if errors.Is(err, unix.ENOSYS) {
		return ErrSymlinkUnsupported
	}
	if errors.Is(err, unix.ENOTDIR) {
		return syscall.ENOTDIR
	}
	if errors.Is(err, unix.EISDIR) {
		return syscall.EISDIR
	}
	if errors.Is(err, unix.EINVAL) {
		return syscall.EINVAL
	}
	return &os.PathError{Op: "openat2", Path: subpath, Err: err}
}

// -----------------------------------------------------------------------------
// Memory implementation: ScopedMemReader
// -----------------------------------------------------------------------------

// ScopedMemReader is the in-memory ScopedReader implementation. It mirrors
// ScopedOSReader's API over MemPlatformReader's flat map of cleaned absolute
// paths and applies explicit per-hop containment checks during symlink
// traversal.
//
// ScopedMemReader preserves:
//
//   - explicit per-hop containment checks (resolving symlink targets
//     lexically against root);
//   - maxSymlinkDepthMemory-hop application-level counter;
//   - ForcedErr semantics: if an entry encountered during traversal has
//     ForcedErr set, that error is returned verbatim;
//   - the documented OS-vs-memory discrepancy for absolute symlink
//     targets outside root (the memory reader is stricter).
//
// The memory reader does NOT model Linux magic links; /proc/self is just a
// regular symlink with a stored target string.
type ScopedMemReader struct {
	root string
	mem  *MemPlatformReader
	// closed is set to 1 by Close; reads are non-atomic on zero-value
	// struct initialization so a closed flag protects accidental misuse.
	closed atomic.Bool
}

// NewScopedMemReader constructs a ScopedMemReader rooted at root, backed
// by the supplied MemPlatformReader.
func NewScopedMemReader(root string, mem *MemPlatformReader) ScopedReader {
	if mem == nil {
		mem = NewMemPlatformReader()
	}
	return &ScopedMemReader{root: filepath.Clean(root), mem: mem}
}

// Root returns the configured root path string.
func (r *ScopedMemReader) Root() string { return r.root }

// checkOpen returns ErrClosed if Close has been called.
func (r *ScopedMemReader) checkOpen() error {
	if r.closed.Load() {
		return ErrClosed
	}
	return nil
}

// Close is a no-op for the memory reader; provided for interface parity.
// Subsequent file methods return ErrClosed. Idempotent.
func (r *ScopedMemReader) Close() error {
	r.closed.Store(true)
	return nil
}

// joinAndValidate joins root and subpath, validates the lexically-cleaned
// result is inside root, and returns the cleaned joined path. The lexical
// containment check is the first defense; the per-hop symlink walk below
// is the second.
func (r *ScopedMemReader) joinAndValidate(subpath string) (string, error) {
	cleaned := filepath.Clean(subpath)
	if cleaned == "." || cleaned == "/" {
		return r.root, nil
	}
	joined := filepath.Join(r.root, cleaned)
	// Belt-and-braces: verify joined is inside root (lexical).
	if joined != r.root && !strings.HasPrefix(joined, r.root+string(filepath.Separator)) {
		return "", ErrSubpathEscape
	}
	return joined, nil
}

// resolveMemory walks the symlink chain starting at cleanPath, returning
// the final resolved entry and the path that entry was looked up under.
//
//   - depth: current hop count (start at 0).
//   - maxDepth: app-level ceiling (maxSymlinkDepthMemory).
//
// The walk applies ForcedErr semantics at every node. Symlink targets are
// containment-checked: an absolute target outside root returns
// ErrSubpathEscape (the documented OS-vs-memory discrepancy); a relative
// target is joined with the parent and the same check is applied.
//
// errAtMaxDepth is returned when depth exceeds maxDepth.
func (r *ScopedMemReader) resolveMemory(cleanPath string, depth int) (*VirtualFile, string, error) {
	if depth > maxSymlinkDepthMemory {
		return nil, "", syscall.ELOOP
	}
	r.mem.mu.RLock()
	entry, ok := r.mem.files[cleanPath]
	r.mem.mu.RUnlock()
	if !ok {
		return nil, cleanPath, nil
	}
	if entry.ForcedErr != nil {
		return nil, "", entry.ForcedErr
	}
	if entry.Kind != FileKindSymlink {
		return entry, cleanPath, nil
	}
	target := entry.Target
	if filepath.IsAbs(target) {
		cleaned := filepath.Clean(target)
		if cleaned != r.root && !strings.HasPrefix(cleaned, r.root+string(filepath.Separator)) {
			return nil, "", ErrSubpathEscape
		}
		return r.resolveMemory(cleaned, depth+1)
	}
	next := filepath.Clean(filepath.Join(filepath.Dir(cleanPath), target))
	if next != r.root && !strings.HasPrefix(next, r.root+string(filepath.Separator)) {
		return nil, "", ErrSubpathEscape
	}
	return r.resolveMemory(next, depth+1)
}

// ReadFile returns the content of the file at subpath.
func (r *ScopedMemReader) ReadFile(subpath string) ([]byte, error) {
	if err := r.checkOpen(); err != nil {
		return nil, err
	}
	if err := ValidateSubpath(subpath); err != nil {
		return nil, err
	}
	cleaned, err := r.joinAndValidate(subpath)
	if err != nil {
		return nil, err
	}
	entry, _, rerr := r.resolveMemory(cleaned, 0)
	if rerr != nil {
		return nil, rerr
	}
	if entry == nil {
		return nil, os.ErrNotExist
	}
	if entry.Kind == FileKindDirectory {
		return nil, syscall.EISDIR
	}
	return append([]byte(nil), entry.Content...), nil
}

// Stat returns the FileInfo of the file at subpath.
func (r *ScopedMemReader) Stat(subpath string) (os.FileInfo, error) {
	if err := r.checkOpen(); err != nil {
		return nil, err
	}
	if err := ValidateSubpath(subpath); err != nil {
		return nil, err
	}
	cleaned, err := r.joinAndValidate(subpath)
	if err != nil {
		return nil, err
	}
	entry, resolved, rerr := r.resolveMemory(cleaned, 0)
	if rerr != nil {
		return nil, rerr
	}
	if entry == nil {
		return nil, os.ErrNotExist
	}
	return &scopedFileInfo{
		name:    filepath.Base(resolved),
		size:    int64(len(entry.Content)),
		mode:    entry.Mode,
		modTime: time.Time{},
		isDir:   entry.Kind == FileKindDirectory,
	}, nil
}

// ReadDir enumerates the directory at subpath, eagerly capturing each
// child's metadata.
func (r *ScopedMemReader) ReadDir(subpath string) ([]os.DirEntry, error) {
	if err := r.checkOpen(); err != nil {
		return nil, err
	}
	if err := ValidateSubpath(subpath); err != nil {
		return nil, err
	}
	cleaned, err := r.joinAndValidate(subpath)
	if err != nil {
		return nil, err
	}
	entry, resolved, rerr := r.resolveMemory(cleaned, 0)
	if rerr != nil {
		return nil, rerr
	}
	if entry == nil {
		return nil, os.ErrNotExist
	}
	if entry.Kind != FileKindDirectory {
		return nil, syscall.ENOTDIR
	}

	prefix := resolved
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}

	r.mem.mu.RLock()
	defer r.mem.mu.RUnlock()

	seen := make(map[string]*VirtualFile)
	for stored, vf := range r.mem.files {
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
		if rel == "." || rel == ".." {
			continue
		}
		seen[rel] = vf
	}

	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]os.DirEntry, 0, len(names))
	for _, n := range names {
		child := seen[n]
		out = append(out, &scopedDirEntry{
			name: n,
			info: &scopedFileInfo{
				name:    n,
				size:    int64(len(child.Content)),
				mode:    child.Mode,
				modTime: time.Time{},
				isDir:   child.Kind == FileKindDirectory,
			},
		})
	}
	return out, nil
}

// Readlink returns the raw symlink target stored at subpath.
func (r *ScopedMemReader) Readlink(subpath string) (string, error) {
	if err := r.checkOpen(); err != nil {
		return "", err
	}
	if err := ValidateSubpath(subpath); err != nil {
		return "", err
	}
	cleaned, err := r.joinAndValidate(subpath)
	if err != nil {
		return "", err
	}
	r.mem.mu.RLock()
	entry, ok := r.mem.files[cleaned]
	r.mem.mu.RUnlock()
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

// Compile-time guarantee for the FileInfo interface.
var _ fs.FileInfo = (*scopedFileInfo)(nil)
