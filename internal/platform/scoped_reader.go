package platform

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
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
// root. All file methods accept subpaths relative to the root; the
// reader validates its own subpath arguments via ValidateSubpath before
// acquiring any file descriptor. ReadDir returns sorted eager metadata; only
// disappearing children (ENOENT) may be skipped. Other metadata failures
// return no entries and a wrapped error. Limits and cooperative cancellation may
// return partial bytes/entries with an error; non-nil contexts are required for reads.
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
//   - ReadFile, Stat, ReadDir, Readlink, FileCapabilities may run concurrently with each
//     other and with operations on other ScopedReader instances.
//   - Close must NOT be called concurrently with any other method on the
//     same reader.
//   - After Close returns, all file methods return ErrClosed.
//   - Root() remains valid after Close.
//   - Close is idempotent: subsequent calls return nil.
type ScopedReader interface {
	ReadFile(ctx context.Context, subpath string) ([]byte, error)
	Stat(subpath string) (os.FileInfo, error)
	ReadDir(ctx context.Context, subpath string) ([]os.DirEntry, error)
	Readlink(subpath string) (string, error)
	FileCapabilities(context.Context, string) (CapabilityAttribute, error)
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
	name     string
	size     int64
	mode     os.FileMode
	modTime  time.Time
	isDir    bool
	metadata fileMetadata
}

func (fi *scopedFileInfo) Name() string       { return fi.name }
func (fi *scopedFileInfo) Size() int64        { return fi.size }
func (fi *scopedFileInfo) Mode() os.FileMode  { return fi.mode }
func (fi *scopedFileInfo) ModTime() time.Time { return fi.modTime }
func (fi *scopedFileInfo) IsDir() bool        { return fi.isDir }
func (fi *scopedFileInfo) Sys() any           { return fi.metadata }

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
	fd     atomic.Int64
	root   string
	limits ReadLimits
}

// NewScopedOSReader opens root once via openat2(AT_FDCWD, root, O_PATH|O_DIRECTORY|O_CLOEXEC,
// RESOLVE_NO_MAGICLINKS) and returns a ScopedReader rooted at the resulting
// inode. The rootfd is held until Close is called.
//
// On pre-5.6 kernels or where openat2(2) is unavailable, NewScopedOSReader
// returns ErrSymlinkUnsupported so the caller can fail fast.
func NewScopedOSReader(root string) (ScopedReader, error) {
	return NewScopedOSReaderWithLimits(root, DefaultReadLimits())
}

// NewScopedOSReaderWithLimits validates limits before opening the root.
func NewScopedOSReaderWithLimits(root string, limits ReadLimits) (ScopedReader, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}
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
	r := &ScopedOSReader{root: root, limits: limits}
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
// After Close, all file methods return ErrClosed; Root() remains valid.
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

// ReadFile checks the target type before data-open and retains a bounded prefix.
// Per-operation FD is owned and closed by readSubpath; the callback
// performs fd-relative reads and returns the assembled buffer.
func (r *ScopedOSReader) ReadFile(ctx context.Context, subpath string) ([]byte, error) {
	if err := r.validateRead(ctx, subpath); err != nil {
		return nil, err
	}
	var data []byte
	err := checkedRegular(ctx, func(flags uint64, fn func(int) error) error { return r.readSubpath(subpath, flags, fn) }, func(fd int) error {
		var err error
		data, err = readAllFD(ctx, fd, r.limits.FileBytes)
		return err
	})
	return data, pathError("read", subpath, err)
}

func (r *ScopedOSReader) validateRead(ctx context.Context, path string) error {
	if err := r.checkOpen(); err != nil {
		return err
	}
	if err := ValidateSubpath(path); err != nil {
		return err
	}
	return ctx.Err()
}

// Stat opens subpath with O_PATH and returns FileInfo built from fstat(2).
func (r *ScopedOSReader) Stat(subpath string) (os.FileInfo, error) {
	var info os.FileInfo
	err := r.readSubpath(subpath, unix.O_PATH, func(fd int) error {
		var st unix.Stat_t
		if lerr := unix.Fstat(fd, &st); lerr != nil {
			return lerr
		}
		info = statInfo(filepath.Base(subpath), &st)
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
func (r *ScopedOSReader) ReadDir(ctx context.Context, subpath string) ([]os.DirEntry, error) {
	if err := r.validateRead(ctx, subpath); err != nil {
		return nil, err
	}
	var entries []os.DirEntry
	err := r.readSubpath(subpath, unix.O_RDONLY|unix.O_DIRECTORY, func(fd int) error {
		var err error
		entries, err = readDirectoryFD(ctx, fd, r.limits)
		return err
	})
	return entries, pathError("readdir", subpath, err)
}

// Readlink opens subpath with O_PATH|O_NOFOLLOW (which yields an FD
// referring to the symlink itself, per open(2)), then readlinkat(fd, "")
// returns the raw stored. The target is not validated; callers
// interpret it.
func (r *ScopedOSReader) Readlink(subpath string) (string, error) {
	var target string
	err := r.readSubpath(subpath, unix.O_PATH|unix.O_NOFOLLOW, func(fd int) error {
		// readlinkat(fd, "") reports ENOENT for a non-link descriptor.
		// Normalize it to Readlink's EINVAL contract without a pathname lookup.
		var st unix.Stat_t
		if err := unix.Fstat(fd, &st); err != nil {
			return err
		}
		if st.Mode&unix.S_IFMT != unix.S_IFLNK {
			return syscall.EINVAL
		}

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
	if errors.Is(err, unix.EXDEV) {
		err = errors.Join(ErrSubpathEscape, err)
	}
	if errors.Is(err, unix.ENOSYS) {
		err = errors.Join(ErrSymlinkUnsupported, err)
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
//   - component-order traversal with explicit per-hop containment checks;
//   - maxSymlinkDepthMemory-hop application-level counter;
//   - ForcedErr semantics: if an entry encountered during traversal has
//     ForcedErr set, that error is returned verbatim;
//   - the documented OS-vs-memory discrepancy for absolute symlink
//     targets: memory uses backing-tree paths and rejects paths outside root.
//
// The memory reader does NOT model Linux magic links; /proc/self is just a
// regular symlink with a stored target string.
type ScopedMemReader struct {
	root   string
	mem    *MemPlatformReader
	limits ReadLimits
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
	return &ScopedMemReader{root: filepath.Clean(root), mem: mem, limits: mem.limits}
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

// ReadFile returns the content of the file at subpath.
func (r *ScopedMemReader) ReadFile(ctx context.Context, subpath string) ([]byte, error) {
	if err := r.validateRead(ctx, subpath); err != nil {
		return nil, err
	}
	entry, _, err := r.mem.resolvePath(subpath, r.root, true, maxSymlinkDepthMemory)
	if err != nil {
		return nil, err
	}
	return readVirtual(ctx, entry, r.limits.FileBytes)
}

func (r *ScopedMemReader) validateRead(ctx context.Context, path string) error {
	if err := r.checkOpen(); err != nil {
		return err
	}
	if err := ValidateSubpath(path); err != nil {
		return err
	}
	return ctx.Err()
}

// NewScopedMemReaderWithLimits overrides the backing reader's copied limits.
func NewScopedMemReaderWithLimits(root string, mem *MemPlatformReader, limits ReadLimits) (ScopedReader, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}
	r := NewScopedMemReader(root, mem).(*ScopedMemReader)
	r.limits = limits
	return r, nil
}

// Stat returns the FileInfo of the file at subpath.
func (r *ScopedMemReader) Stat(subpath string) (os.FileInfo, error) {
	if err := r.checkOpen(); err != nil {
		return nil, err
	}
	if err := ValidateSubpath(subpath); err != nil {
		return nil, err
	}
	entry, _, rerr := r.mem.resolvePath(subpath, r.root, true, maxSymlinkDepthMemory)
	if rerr != nil {
		return nil, rerr
	}
	return virtualInfo(filepath.Base(subpath), entry), nil
}

// ReadDir enumerates the directory at subpath, eagerly capturing each
// child's metadata.
func (r *ScopedMemReader) ReadDir(ctx context.Context, subpath string) ([]os.DirEntry, error) {
	if err := r.validateRead(ctx, subpath); err != nil {
		return nil, err
	}

	entry, resolved, rerr := r.mem.resolvePath(subpath, r.root, true, maxSymlinkDepthMemory)
	if rerr != nil {
		return nil, rerr
	}
	if entry.Kind != FileKindDirectory {
		return nil, syscall.ENOTDIR
	}

	return r.mem.captureChildren(ctx, resolved, r.limits)
}

// Readlink returns the raw symlink target stored at subpath.
func (r *ScopedMemReader) Readlink(subpath string) (string, error) {
	if err := r.checkOpen(); err != nil {
		return "", err
	}
	if err := ValidateSubpath(subpath); err != nil {
		return "", err
	}
	entry, _, err := r.mem.resolvePath(subpath, r.root, false, maxSymlinkDepthMemory)
	if err != nil {
		return "", err
	}
	if entry.Kind != FileKindSymlink {
		return "", syscall.EINVAL
	}
	return entry.Target, nil
}

// Compile-time guarantee for the FileInfo interface.
var _ fs.FileInfo = (*scopedFileInfo)(nil)
