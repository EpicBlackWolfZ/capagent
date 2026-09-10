package platform

import (
	"context"
	"errors"

	"golang.org/x/sys/unix"
)

// CapabilityAttribute is raw security.capability data. Present does not assert
// valid encoding or effective privileges. Bytes belong to the receiving caller.
type CapabilityAttribute struct {
	Bytes   []byte
	Present bool
}

func readCapabilityAttribute(ctx context.Context, read func([]byte) (int, error)) (CapabilityAttribute, error) {
	if err := ctx.Err(); err != nil {
		return CapabilityAttribute{}, err
	}
	buf := make([]byte, capabilityBytes)
	n, err := read(buf)
	if errors.Is(err, unix.ENODATA) {
		return CapabilityAttribute{}, ctx.Err()
	}
	if errors.Is(err, unix.ERANGE) {
		err = errors.Join(err, &LimitError{Resource: "capability bytes", Limit: capabilityBytes})
	}
	if err != nil {
		return CapabilityAttribute{}, errors.Join(err, ctx.Err())
	}
	return CapabilityAttribute{Present: true, Bytes: buf[:n:n]}, incomplete(ctx.Err())
}

func inspectCapabilities(ctx context.Context, withFD descriptorOperation) (CapabilityAttribute, error) {
	var result CapabilityAttribute
	err := checkedRegular(ctx, withFD, func(fd int) error {
		var err error
		result, err = readCapabilityAttribute(ctx, func(buf []byte) (int, error) { return unix.Fgetxattr(fd, "security.capability", buf) })
		return err
	})
	return result, err
}

func (r *ScopedOSReader) FileCapabilities(ctx context.Context, path string) (CapabilityAttribute, error) {
	if err := r.validateRead(ctx, path); err != nil {
		return CapabilityAttribute{}, err
	}
	result, err := inspectCapabilities(ctx, func(flags uint64, fn func(int) error) error { return r.readSubpath(path, flags, fn) })
	return result, pathError("file capabilities", path, err)
}
func (r *OSPlatformReader) FileCapabilities(ctx context.Context, path string) (CapabilityAttribute, error) {
	result, err := inspectCapabilities(ctx, func(flags uint64, fn func(int) error) error { return withHostFD(path, flags, fn) })
	return result, pathError("file capabilities", path, err)
}

// SetFileCapabilities replaces a fixture's raw attribute and optional read error.
func (m *MemPlatformReader) SetFileCapabilities(path string, value CapabilityAttribute, err error) error {
	if !value.Present && len(value.Bytes) > 0 {
		return ErrMalformed
	}
	value.Bytes = append([]byte(nil), value.Bytes...)
	return m.updateNode(path, func(node *VirtualFile) { node.Capabilities = value; node.CapabilityErr = err })
}
func virtualCapabilities(ctx context.Context, node *VirtualFile) (CapabilityAttribute, error) {
	if err := regularMode(node.Mode); err != nil {
		return CapabilityAttribute{}, err
	}
	return readCapabilityAttribute(ctx, func(buf []byte) (int, error) {
		if node.CapabilityErr != nil {
			return 0, node.CapabilityErr
		}
		if !node.Capabilities.Present {
			return 0, unix.ENODATA
		}
		if len(node.Capabilities.Bytes) > len(buf) {
			return 0, unix.ERANGE
		}
		return copy(buf, node.Capabilities.Bytes), nil
	})
}
func (m *MemPlatformReader) FileCapabilities(ctx context.Context, path string) (CapabilityAttribute, error) {
	if err := ctx.Err(); err != nil {
		return CapabilityAttribute{}, err
	}
	node, _, err := m.resolvePath(path, "", true, maxSymlinkDepth)
	if err != nil {
		return CapabilityAttribute{}, err
	}
	return virtualCapabilities(ctx, node)
}
func (r *ScopedMemReader) FileCapabilities(ctx context.Context, path string) (CapabilityAttribute, error) {
	if err := r.validateRead(ctx, path); err != nil {
		return CapabilityAttribute{}, err
	}
	node, _, err := r.mem.resolvePath(path, r.root, true, maxSymlinkDepthMemory)
	if err != nil {
		return CapabilityAttribute{}, err
	}
	return virtualCapabilities(ctx, node)
}
