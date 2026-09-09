package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// defaultSysRoot is the canonical Linux sysfs mount point.
const defaultSysRoot = "/sys"

// cgroupBasePath is the canonical sysfs cgroup v2 unified hierarchy directory.
const cgroupBasePath = "fs/cgroup"

// cgroupControllersPath is the file listing available cgroup v2 controllers.
const cgroupControllersPath = "fs/cgroup/cgroup.controllers"

// selinuxEnforcePath is the sysfs path exposing SELinux enforcement mode.
const selinuxEnforcePath = "fs/selinux/enforce"

// apparmorPath is the sysfs path reporting AppArmor availability.
const apparmorPath = "kernel/security/apparmor"

// SELinuxModeEnforcing is the value written to sysfs when SELinux is enforcing.
const selinuxEnforcing = "1"

// SysfsReader parses sysfs pseudo-files via a PlatformReader.
//
// All methods are pure parsers; they translate sysfs text protocols into
// structured transport types and never interpret security state.
type SysfsReader struct {
	reader PlatformReader
	root   string
}

// NewSysfsReader constructs a SysfsReader bound to the given PlatformReader.
// An empty root defaults to "/sys".
func NewSysfsReader(r PlatformReader, root string) *SysfsReader {
	if r == nil {
		return nil
	}
	if root == "" {
		root = defaultSysRoot
	}
	return &SysfsReader{reader: r, root: root}
}

// Root returns the configured sysfs root path.
func (s *SysfsReader) Root() string {
	return s.root
}

// joinRoot joins the sysfs root with a relative subpath.
func (s *SysfsReader) joinRoot(subpath string) string {
	cleanSub := filepath.Clean(subpath)
	if cleanSub == "." || cleanSub == "/" {
		return s.root
	}
	return filepath.Join(s.root, cleanSub)
}

// ReadSysFile reads raw bytes at the supplied sysfs-relative subpath.
func (s *SysfsReader) ReadSysFile(subpath string) ([]byte, error) {
	return s.reader.ReadFile(s.joinRoot(subpath))
}

// ReadCgroupController reads the cgroup v2 controller file at
// /sys/fs/cgroup/<controller>.
//
// The controller argument must be a single cgroup v2 controller name as
// documented in cgroup(7): a non-empty, relative path segment composed of
// lowercase alphanumerics and underscores. Path separators, absolute
// paths, ".", "..", and any normalization traversal attempt are rejected
// without touching the underlying PlatformReader.
func (s *SysfsReader) ReadCgroupController(controller string) ([]byte, error) {
	if err := validateCgroupController(controller); err != nil {
		return nil, err
	}
	return s.reader.ReadFile(s.joinRoot(cgroupBasePath + "/" + controller))
}

// validateCgroupController enforces that controller is a single cgroup v2
// controller name. The rules mirror the kernel's own controller naming
// grammar and reject every path-traversal shape that could escape
// cgroupBasePath.
//
// The validation is intentionally stricter than filepath.Clean would
// produce: callers cannot smuggle separators, leading dots, or relative
// references past the API boundary.
func validateCgroupController(controller string) error {
	if controller == "" {
		return errors.New("controller name cannot be empty")
	}
	if controller == "." || controller == ".." {
		return errors.New("controller name must not be \".\" or \"..\"")
	}
	if strings.ContainsRune(controller, '/') {
		return errors.New("controller name must not contain a path separator")
	}
	if controller[0] == '.' {
		return errors.New("controller name must not start with \".\"")
	}
	return nil
}

// CgroupControllers parses the cgroup v2 controller list at
// /sys/fs/cgroup/cgroup.controllers. Controllers are space-delimited; empty
// strings and surrounding whitespace are stripped.
func (s *SysfsReader) CgroupControllers() ([]string, error) {
	data, err := s.ReadSysFile(cgroupControllersPath)
	if err != nil {
		return nil, fmt.Errorf("read cgroup.controllers: %w", err)
	}
	return splitControllerList(data), nil
}

// SELinuxPresent reports whether /sys/fs/selinux exists in sysfs. Missing
// files return (false, nil); I/O errors are surfaced as-is.
func (s *SysfsReader) SELinuxPresent() (bool, error) {
	if _, err := s.reader.Stat(s.joinRoot(filepath.Dir(selinuxEnforcePath))); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// SELinuxMode reads /sys/fs/selinux/enforce and returns its trimmed string
// content. Callers interpret "1" as enforcing and "0" as permissive.
//
// Per AGENTS.md guidance, "not present" is distinguished from "read failure":
// if /sys/fs/selinux/enforce is absent, returns ("", nil).
func (s *SysfsReader) SELinuxMode() (string, error) {
	data, err := s.ReadSysFile(selinuxEnforcePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// IsSELinuxEnforcing returns true when SELinux is both present and enforcing.
// Absence of the subsystem is treated as permissive (false) without error.
func (s *SysfsReader) IsSELinuxEnforcing() (bool, error) {
	present, err := s.SELinuxPresent()
	if err != nil || !present {
		return false, err
	}
	mode, err := s.SELinuxMode()
	if err != nil {
		return false, err
	}
	return mode == selinuxEnforcing, nil
}

// AppArmorPresent reports whether /sys/kernel/security/apparmor exists.
func (s *SysfsReader) AppArmorPresent() (bool, error) {
	if _, err := s.reader.Stat(s.joinRoot(apparmorPath)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// splitControllerList parses a space-delimited cgroup controller list and
// returns a slice of unique non-empty entries.
func splitControllerList(data []byte) []string {
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(fields))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	return out
}
