package platform

import (
	"io/fs"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// FileOwnership is measured ownership, not an assertion about target-user access.
type FileOwnership struct{ UID, GID uint32 }

type fileMetadata struct {
	Ownership FileOwnership
	Known     bool
}

// OwnershipOf returns a value copy. Unknown fixture ownership is distinct from root.
func OwnershipOf(info fs.FileInfo) (FileOwnership, bool) {
	if info == nil {
		return FileOwnership{}, false
	}
	switch meta := info.Sys().(type) {
	case fileMetadata:
		return meta.Ownership, meta.Known
	case *syscall.Stat_t:
		return FileOwnership{UID: meta.Uid, GID: meta.Gid}, true
	default:
		return FileOwnership{}, false
	}
}

const privilegeBits = os.ModeSetuid | os.ModeSetgid | os.ModeSticky

func scopedStatMode(mode uint32) os.FileMode {
	out := os.FileMode(mode & permMask)
	switch mode & unix.S_IFMT {
	case unix.S_IFREG:
	case unix.S_IFDIR:
		out |= os.ModeDir
	case unix.S_IFLNK:
		out |= os.ModeSymlink
	case unix.S_IFIFO:
		out |= os.ModeNamedPipe
	case unix.S_IFSOCK:
		out |= os.ModeSocket
	case unix.S_IFCHR:
		out |= os.ModeDevice | os.ModeCharDevice
	case unix.S_IFBLK:
		out |= os.ModeDevice
	default:
		out |= os.ModeIrregular
	}
	if mode&unix.S_ISUID != 0 {
		out |= os.ModeSetuid
	}
	if mode&unix.S_ISGID != 0 {
		out |= os.ModeSetgid
	}
	if mode&unix.S_ISVTX != 0 {
		out |= os.ModeSticky
	}
	return out
}

func statInfo(name string, st *unix.Stat_t) os.FileInfo {
	return &scopedFileInfo{
		name: name, size: st.Size, mode: scopedStatMode(st.Mode),
		modTime: time.Unix(st.Mtim.Sec, st.Mtim.Nsec), isDir: st.Mode&unix.S_IFMT == unix.S_IFDIR,
		metadata: fileMetadata{Ownership: FileOwnership{UID: st.Uid, GID: st.Gid}, Known: true},
	}
}

// SetOwnership replaces a fixture node; existing snapshots retain their ownership.
func (m *MemPlatformReader) SetOwnership(path string, owner FileOwnership) error {
	return m.updateNode(path, func(node *VirtualFile) { node.Ownership = owner; node.OwnershipKnown = true })
}

func (m *MemPlatformReader) updateNode(path string, update func(*VirtualFile)) error {
	clean, err := normalize(path)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	node := m.files[clean]
	if node == nil {
		return os.ErrNotExist
	}
	replacement := *node
	update(&replacement)
	m.files[clean] = &replacement
	return nil
}

// AddSpecial inserts an explicit special node. It never opens a real device.
func (m *MemPlatformReader) AddSpecial(path string, mode os.FileMode) error {
	switch mode.Type() {
	case os.ModeNamedPipe, os.ModeSocket, os.ModeDevice, os.ModeDevice | os.ModeCharDevice, os.ModeIrregular:
	default:
		return &os.PathError{Op: "add special", Path: path, Err: syscall.EINVAL}
	}
	clean, err := normalize(path)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[clean] = &VirtualFile{Kind: FileKindSpecial, Mode: mode}
	return nil
}

func virtualInfo(name string, node *VirtualFile) os.FileInfo {
	return &memFileInfo{name: name, size: node.size(), mode: node.Mode, isDir: node.Kind == FileKindDirectory,
		metadata: fileMetadata{Ownership: node.Ownership, Known: node.OwnershipKnown}}
}
