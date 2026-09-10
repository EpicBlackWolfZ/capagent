package platform

import (
	"context"
	"errors"
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

// SysfsReader parses sysfs pseudo-files via a ScopedReader.
//
// All methods are pure parsers; they translate sysfs text protocols into
// structured transport types and never interpret security state.
//
// Path containment is delegated to the supplied ScopedReader.
type SysfsReader struct {
	reader ScopedReader
}

// NewSysfsReader constructs a SysfsReader bound to the given ScopedReader.
// The supplied reader is the sole authority for the root.
func NewSysfsReader(r ScopedReader) *SysfsReader {
	if r == nil {
		return nil
	}
	return &SysfsReader{reader: r}
}

// Root returns the configured sysfs root path.
func (s *SysfsReader) Root() string {
	return s.reader.Root()
}

// ReadSysFile reads raw bytes at the supplied sysfs-relative subpath.
func (s *SysfsReader) ReadSysFile(ctx context.Context, subpath string) ([]byte, error) {
	if err := ValidateSubpath(subpath); err != nil {
		return nil, err
	}
	return s.reader.ReadFile(ctx, subpath)
}

// ReadCgroupFile reads a single cgroup v2 filename, such as cpu.max or
// cgroup.controllers. Grammar is [a-z][a-z0-9_]*(\.[a-z0-9_]+)*; it does
// not assert that a named controller exists. Subdirectories are not accepted.
func (s *SysfsReader) ReadCgroupFile(ctx context.Context, filename string) ([]byte, error) {
	if err := validateCgroupFilename(filename); err != nil {
		return nil, err
	}
	return s.ReadSysFile(ctx, cgroupBasePath+"/"+filename)
}

func validateCgroupFilename(filename string) error {
	first := true
	for part := range strings.SplitSeq(filename, ".") {
		if first {
			if !identifier(part) {
				return ErrMalformed
			}
			first = false
			continue
		}
		if part == "" {
			return ErrMalformed
		}
		for _, ch := range part {
			if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '_' {
				return ErrMalformed
			}
		}
	}
	return nil
}

// CgroupControllers parses the cgroup v2 controller list at
// /sys/fs/cgroup/cgroup.controllers. Controllers are space-delimited; empty
// strings and surrounding whitespace are stripped.
func (s *SysfsReader) CgroupControllers(ctx context.Context) ([]string, error) {
	data, err := s.ReadSysFile(ctx, cgroupControllersPath)
	return parseControllers(ctx, data, err)
}

// SELinuxPresent reports whether /sys/fs/selinux exists in sysfs. Missing
// files return (false, nil); I/O errors are surfaced as-is.
func (s *SysfsReader) SELinuxPresent() (bool, error) {
	if _, err := s.reader.Stat(filepath.Dir(selinuxEnforcePath)); err != nil {
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
func (s *SysfsReader) SELinuxMode(ctx context.Context) (string, error) {
	data, err := s.ReadSysFile(ctx, selinuxEnforcePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return parseSELinux(data)
}

// IsSELinuxEnforcing returns true when SELinux is both present and enforcing.
// Absence returns false without error; false alone does not establish permissive mode.
func (s *SysfsReader) IsSELinuxEnforcing(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	present, err := s.SELinuxPresent()
	if err != nil || !present {
		return false, err
	}
	mode, err := s.SELinuxMode(ctx)
	if err != nil {
		return false, err
	}
	if mode == "" {
		return false, pathError("read", selinuxEnforcePath, os.ErrNotExist)
	}
	return mode == selinuxEnforcing, nil
}

// AppArmorPresent reports whether /sys/kernel/security/apparmor exists.
func (s *SysfsReader) AppArmorPresent() (bool, error) {
	if _, err := s.reader.Stat(apparmorPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func parseSELinux(data []byte) (string, error) {
	mode := strings.TrimSpace(string(data))
	if mode != "0" && mode != "1" {
		return "", incomplete(ErrMalformed)
	}
	return mode, nil
}

func parseControllers(ctx context.Context, data []byte, readErr error) ([]string, error) {
	if len(data) > defaultFileBytes {
		data = data[:defaultFileBytes]
		readErr = errors.Join(readErr, &LimitError{Resource: "parser input bytes", Limit: defaultFileBytes})
	}
	// Only whitespace-terminated tokens survive incomplete transport.
	if readErr != nil {
		end := len(data)
		for end > 0 && !strings.ContainsRune(" \t\n\r\v\f", rune(data[end-1])) {
			end--
		}
		data = data[:end]
	}
	out := make([]string, 0)
	seen := make(map[string]bool)
	diagnostics := &ParseError{Source: cgroupControllersPath}
	token := 0
	for field := range strings.FieldsSeq(string(data)) {
		if err := ctx.Err(); err != nil {
			readErr = errors.Join(readErr, err)
			break
		}
		token++
		if len(field) > defaultLineBytes {
			diagnostics.add(token, &LimitError{Resource: "controller bytes", Limit: defaultLineBytes}, defaultDiagnostics)
			continue
		}
		if !identifier(field) {
			diagnostics.add(token, ErrMalformed, defaultDiagnostics)
			continue
		}
		if seen[field] {
			continue
		}
		if len(out) == defaultRecords {
			diagnostics.add(token, &LimitError{Resource: "records", Limit: defaultRecords}, defaultDiagnostics)
			break
		}
		seen[field] = true
		out = append(out, field)
	}
	var parseErr error
	if len(diagnostics.Diagnostics) > 0 {
		parseErr = diagnostics
	}
	return out, errors.Join(parseErr, incomplete(readErr), incomplete(ctx.Err()))
}
