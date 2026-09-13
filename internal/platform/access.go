package platform

import (
	"context"
	"errors"
	"golang.org/x/sys/unix"
)

// ExecutableAccess asks the kernel about the current effective credential set and
// mount/ACL policy on the confined inode. ENOSYS stays unknown on Linux 5.6/5.7.
func (r *ScopedOSReader) ExecutableAccess(ctx context.Context, subpath string) (bool, error) {
	if err := r.validateRead(ctx, subpath); err != nil {
		return false, err
	}
	err := r.readSubpath(subpath, unix.O_PATH, func(fd int) error {
		return unix.Faccessat2(fd, "", unix.X_OK, unix.AT_EMPTY_PATH|unix.AT_EACCESS)
	})
	if errors.Is(err, unix.EACCES) {
		return false, nil
	}
	return err == nil, err
}
func (r *ScopedMemReader) ExecutableAccess(ctx context.Context, subpath string) (bool, error) {
	if err := r.validateRead(ctx, subpath); err != nil {
		return false, err
	}
	node, _, err := r.mem.resolvePath(subpath, r.root, true, maxSymlinkDepthMemory)
	if err != nil {
		return false, err
	}
	if node.AccessErr != nil {
		return false, node.AccessErr
	}
	if node.ExecutableAccess == nil {
		return false, ErrIncomplete
	}
	return *node.ExecutableAccess, nil
}

// SetExecutableAccess supplies an explicit fixture result; mode bits never fabricate ACL access.
func (m *MemPlatformReader) SetExecutableAccess(path string, allowed bool, err error) error {
	return m.updateNode(path, func(node *VirtualFile) { node.ExecutableAccess = &allowed; node.AccessErr = err })
}
