package platform

import (
	"context"
	"strings"
)

// defaultProcRoot is the canonical Linux procfs mount point.
const defaultProcRoot = "/proc"

// selfSubpath is the conventional procfs subdirectory describing the current process.
const selfSubpath = "self"

// cgroupEntryFields is the canonical colon-delimited field count for a
// /proc/self/cgroup line (hierarchy-id:controllers:path).
const cgroupEntryFields = 3

// mountinfoMaxLineBytes is the maximum /proc/self/mountinfo line length accepted by the parser.
const mountinfoMaxLineBytes = 1024 * 1024

// mountinfoMinFields is the minimum number of whitespace-separated fields in a
// mountinfo record, including optional-field placeholders.
const mountinfoMinFields = 10

// mountinfoOptionalStart is the field index at which optional "tag:value"
// fields appear in a mountinfo record (immediately before the '-' separator).
const mountinfoOptionalStart = 6

// mountinfoFixedTail is the number of trailing fields in a mountinfo record
// after the '-' separator (FSType, MountSource, SuperOptions).
const mountinfoFixedTail = 3

// MountEntry represents a single record from /proc/self/mountinfo.
//
// mountinfo(5) line grammar (fields separated by spaces, with optional
// whitespace-separated optional fields between separator '-'):
//
//	36 35 98:0 /mnt1 /mnt rw,noatime master:1 - ext3 /dev/root rw,errors=continue
//
// Fields:
//   - MountID: unique mount identifier.
//   - ParentID: parent mount identifier (or 0 for the root mount).
//   - Major:Minor: device major:minor numbers.
//   - Root: root of the mount within the filesystem.
//   - MountPoint: mount point in the process view.
//   - Options: per-mount options (comma-separated).
//   - OptionalFields: zero or more "tag:value" optional fields.
//   - FSType: filesystem type (after the '-' separator).
//   - MountSource: textual mount source description.
//   - SuperOptions: per-superblock options.
type MountEntry struct {
	MountID        string
	ParentID       string
	MajorMinor     string
	Root           string // Raw escaped mountinfo field; not decoded for filesystem access.
	MountPoint     string // Raw escaped mountinfo field.
	Options        string
	OptionalFields []string
	FSType         string
	MountSource    string // Raw escaped mountinfo field.
	SuperOptions   string
}

// FilesystemEntry represents a single record from /proc/filesystems.
//
// The "nodev" flag indicates the filesystem type does not require a block
// device (e.g. tmpfs, proc, sysfs).
type FilesystemEntry struct {
	Name  string
	NoDev bool
}

// CgroupEntry represents a single line from /proc/self/cgroup.
//
// Cgroup v2 unified hierarchy lines have the form "0::/path".
// Cgroup v1 multi-hierarchy lines have the form "ID:controllers:path".
type CgroupEntry struct {
	HierarchyID string
	Controllers string
	Path        string
}

// IsUnified reports whether the cgroup entry belongs to the cgroup v2
// unified hierarchy (HierarchyID == 0 and Controllers empty).
func (e CgroupEntry) IsUnified() bool {
	return e.HierarchyID == "0" && e.Controllers == ""
}

// ProcfsReader parses procfs pseudo-files via a ScopedReader.
//
// All methods are pure parsers: they translate Linux text protocols into
// structured transport types and never interpret runtime, container, or
// host semantics.
//
// Path containment is delegated to the supplied ScopedReader. The reader
// is the security boundary; adapters also reject malformed input before I/O.
type ProcfsReader struct {
	reader ScopedReader
	limits ParserLimits
}

// NewProcfsReader constructs a ProcfsReader bound to the given ScopedReader.
// The supplied reader is the sole authority for the root.
func NewProcfsReader(r ScopedReader) *ProcfsReader {
	if r == nil {
		return nil
	}
	return &ProcfsReader{reader: r, limits: DefaultParserLimits()}
}

// Root returns the configured procfs root path.
func (p *ProcfsReader) Root() string {
	return p.reader.Root()
}

// ReadProcFile reads raw bytes at the supplied procfs-relative subpath.
func (p *ProcfsReader) ReadProcFile(ctx context.Context, subpath string) ([]byte, error) {
	if err := ValidateSubpath(subpath); err != nil {
		return nil, err
	}
	return p.reader.ReadFile(ctx, subpath)
}

// ReadSelf prefixes a validated subpath with self/ without cleaning it.
// Containment is the proc reader root, not a separate self subtree.
func (p *ProcfsReader) ReadSelf(ctx context.Context, subpath string) ([]byte, error) {
	if err := ValidateSubpath(subpath); err != nil {
		return nil, err
	}
	return p.reader.ReadFile(ctx, selfSubpath+"/"+subpath)
}

// NewProcfsReaderWithLimits validates explicit per-parser resource limits.
func NewProcfsReaderWithLimits(r ScopedReader, limits ParserLimits) (*ProcfsReader, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}
	p := NewProcfsReader(r)
	if p == nil {
		return nil, ErrMalformed
	}
	p.limits = limits
	return p, nil
}

// Mounts returns raw escaped pathname fields. Partial results always carry an error.
func (p *ProcfsReader) Mounts(ctx context.Context) ([]MountEntry, error) {
	data, err := p.ReadSelf(ctx, "mountinfo")
	limits := p.limits
	limits.LineBytes = limits.MountLineBytes
	return parseRecords(ctx, data, err, "mountinfo", limits, parseMountLine)
}

// Filesystems returns parsed records and any completeness diagnostics.
func (p *ProcfsReader) Filesystems(ctx context.Context) ([]FilesystemEntry, error) {
	data, err := p.ReadProcFile(ctx, "filesystems")
	return parseRecords(ctx, data, err, "filesystems", p.limits, parseFilesystemLine)
}

// Cgroups preserves measured complete records even when a later read/parse fails.
func (p *ProcfsReader) Cgroups(ctx context.Context) ([]CgroupEntry, error) {
	data, err := p.ReadSelf(ctx, "cgroup")
	return parseRecords(ctx, data, err, "cgroup", p.limits, parseCgroupLine)
}

func parseFilesystemLine(line string) (FilesystemEntry, error) {
	if strings.ContainsRune(line, 0) {
		return FilesystemEntry{}, ErrMalformed
	}
	fields := strings.Fields(line)
	switch {
	case len(fields) == 1:
		return FilesystemEntry{Name: fields[0]}, nil
	case len(fields) == 2 && fields[0] == "nodev":
		return FilesystemEntry{Name: fields[1], NoDev: true}, nil
	default:
		return FilesystemEntry{}, ErrMalformed
	}
}

func parseCgroupLine(line string) (CgroupEntry, error) {
	fields := strings.SplitN(line, ":", cgroupEntryFields)
	if len(fields) != cgroupEntryFields || !decimal(fields[0]) || !strings.HasPrefix(fields[2], "/") || strings.ContainsRune(line, 0) {
		return CgroupEntry{}, ErrMalformed
	}
	if fields[1] != "" {
		for controller := range strings.SplitSeq(fields[1], ",") {
			// v1 named hierarchies use name=..., unlike v2 controller identifiers.
			if !validCgroupController(controller) {
				return CgroupEntry{}, ErrMalformed
			}
		}
	}
	return CgroupEntry{HierarchyID: fields[0], Controllers: fields[1], Path: fields[2]}, nil
}

func parseMountLine(line string) (MountEntry, error) {
	fields := strings.Fields(line)
	if len(fields) < mountinfoMinFields || !decimal(fields[0]) || !decimal(fields[1]) {
		return MountEntry{}, ErrMalformed
	}
	major, minor, ok := strings.Cut(fields[2], ":")
	if !ok || !decimal(major) || !decimal(minor) || !strings.HasPrefix(fields[3], "/") || !strings.HasPrefix(fields[4], "/") {
		return MountEntry{}, ErrMalformed
	}
	sep := -1
	for i := mountinfoOptionalStart; i < len(fields); i++ {
		if fields[i] == "-" {
			sep = i
			break
		}
	}
	if sep < 0 || len(fields)-sep-1 != mountinfoFixedTail || strings.ContainsRune(line, 0) {
		return MountEntry{}, ErrMalformed
	}
	optional := append([]string{}, fields[mountinfoOptionalStart:sep]...)
	return MountEntry{MountID: fields[0], ParentID: fields[1], MajorMinor: fields[2], Root: fields[3], MountPoint: fields[4],
		Options: fields[5], OptionalFields: optional, FSType: fields[sep+1], MountSource: fields[sep+2], SuperOptions: fields[sep+3]}, nil
}

// Named v1 hierarchies accept ASCII word characters, dots and hyphens;
// their grammar is broader than the kernel controller identifiers.
func validCgroupController(controller string) bool {
	name, named := strings.CutPrefix(controller, "name=")
	if !named {
		return identifier(controller)
	}
	if name == "" {
		return false
	}
	for _, ch := range name {
		if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') && ch != '_' && ch != '.' && ch != '-' {
			return false
		}
	}
	return true
}
