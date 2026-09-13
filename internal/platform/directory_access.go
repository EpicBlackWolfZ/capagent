package platform

import (
	"context"
	"errors"
	"syscall"

	"golang.org/x/sys/unix"
)

// DirectoryAccess queries the effective target credentials against the confined
// directory inode. Writable requests W_OK|X_OK; read-only requests R_OK|X_OK.
// It opens no writable descriptor and never creates or modifies directory data.
// ACL/mount denial is false; unsupported syscalls and incomplete queries are errors.
func (r *ScopedOSReader) DirectoryAccess(ctx context.Context, subpath string, writable bool) (bool, error) {
	if err := r.validateRead(ctx, subpath); err != nil {
		return false, err
	}
	mode := uint32(unix.R_OK | unix.X_OK)
	if writable {
		mode = unix.W_OK | unix.X_OK
	}
	err := r.readSubpath(subpath, unix.O_PATH|unix.O_DIRECTORY, func(fd int) error {
		return unix.Faccessat2(fd, "", mode, unix.AT_EMPTY_PATH|unix.AT_EACCESS)
	})
	if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EROFS) {
		return false, nil
	}
	return err == nil, err
}

// DirectoryAccessResult is a value-owned explicit fixture response, independent
// of mode bits. The zero value is unmeasured.
type DirectoryAccessResult struct {
	Known   bool
	Allowed bool
	Err     error
}

func (r *ScopedMemReader) DirectoryAccess(ctx context.Context, subpath string, writable bool) (bool, error) {
	if err := r.validateRead(ctx, subpath); err != nil {
		return false, err
	}
	node, _, err := r.mem.resolvePath(subpath, r.root, true, maxSymlinkDepthMemory)
	if err != nil {
		return false, err
	}
	if node.Kind != FileKindDirectory {
		return false, syscall.ENOTDIR
	}
	result := node.DirectoryRead
	if writable {
		result = node.DirectoryWrite
	}
	if result.Err != nil {
		return false, result.Err
	}
	if !result.Known {
		return false, ErrIncomplete
	}
	return result.Allowed, nil
}

func (m *MemPlatformReader) SetDirectoryAccess(path string, writable, allowed bool, err error) error {
	return m.updateNode(path, func(node *VirtualFile) {
		result := DirectoryAccessResult{Known: true, Allowed: allowed, Err: err}
		if writable {
			node.DirectoryWrite = result
		} else {
			node.DirectoryRead = result
		}
	})
}
