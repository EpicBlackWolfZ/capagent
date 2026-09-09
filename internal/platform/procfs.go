package platform

import (
	"bufio"
	"bytes"
	"fmt"
	"path/filepath"
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

// mountinfoInitialBufferBytes is the initial scanner buffer size for mountinfo lines.
const mountinfoInitialBufferBytes = 64 * 1024

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
	Root           string
	MountPoint     string
	Options        string
	OptionalFields []string
	FSType         string
	MountSource    string
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

// ProcfsReader parses procfs pseudo-files via a PlatformReader.
//
// All methods are pure parsers: they translate Linux text protocols into
// structured transport types and never interpret runtime, container, or
// host semantics.
type ProcfsReader struct {
	reader PlatformReader
	root   string
}

// NewProcfsReader constructs a ProcfsReader bound to the given PlatformReader.
// An empty root defaults to "/proc".
func NewProcfsReader(r PlatformReader, root string) *ProcfsReader {
	if r == nil {
		return nil
	}
	if root == "" {
		root = defaultProcRoot
	}
	return &ProcfsReader{reader: r, root: root}
}

// Root returns the configured procfs root path.
func (p *ProcfsReader) Root() string {
	return p.root
}

// joinRoot returns filepath.Join(root, subpath) or returns the canonical
// separator-free path for the default root when subpath is empty.
func (p *ProcfsReader) joinRoot(subpath string) string {
	cleanSub := filepath.Clean(subpath)
	if cleanSub == "." || cleanSub == "/" {
		return p.root
	}
	return filepath.Join(p.root, cleanSub)
}

// ReadProcFile reads raw bytes at the supplied procfs-relative subpath.
func (p *ProcfsReader) ReadProcFile(subpath string) ([]byte, error) {
	return p.reader.ReadFile(p.joinRoot(subpath))
}

// ReadSelf reads raw bytes at /proc/self/<subpath>.
func (p *ProcfsReader) ReadSelf(subpath string) ([]byte, error) {
	return p.reader.ReadFile(p.joinRoot(filepath.Join(selfSubpath, subpath)))
}

// Mounts parses /proc/self/mountinfo into structured entries.
//
// A malformed line is skipped; only fully-parseable lines are returned.
// An empty result means the file was missing or contained no records.
func (p *ProcfsReader) Mounts() ([]MountEntry, error) {
	data, err := p.ReadSelf("mountinfo")
	if err != nil {
		return nil, fmt.Errorf("read mountinfo: %w", err)
	}
	return parseMountInfo(data), nil
}

// Filesystems parses /proc/filesystems into structured entries.
//
// A malformed line is skipped; only fully-parseable lines are returned.
func (p *ProcfsReader) Filesystems() ([]FilesystemEntry, error) {
	data, err := p.ReadProcFile("filesystems")
	if err != nil {
		return nil, fmt.Errorf("read filesystems: %w", err)
	}
	return parseFilesystems(data), nil
}

// Cgroups parses /proc/self/cgroup into structured entries.
func (p *ProcfsReader) Cgroups() ([]CgroupEntry, error) {
	data, err := p.ReadSelf("cgroup")
	if err != nil {
		return nil, fmt.Errorf("read cgroup: %w", err)
	}
	return parseCgroup(data), nil
}

// parseFilesystems reads /proc/filesystems format:
//
//	nodev	proc
//	nodev	tmpfs
//	        ext4
//
// Each non-empty line is "<flags>\t<name>" where <flags> is optional
// (omitted when the filesystem requires a block device).
func parseFilesystems(data []byte) []FilesystemEntry {
	var out []FilesystemEntry
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}
		name := fields[len(fields)-1]
		noDev := false
		for _, flag := range fields[:len(fields)-1] {
			if flag == "nodev" {
				noDev = true
				break
			}
		}
		out = append(out, FilesystemEntry{Name: name, NoDev: noDev})
	}
	return out
}

// parseCgroup reads /proc/self/cgroup format (v1 and v2 mixed):
//
//	v1: hierarchy-ID:controller-list:cgroup-path
//	v2: 0::cgroup-path
func parseCgroup(data []byte) []CgroupEntry {
	var out []CgroupEntry
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, ":", cgroupEntryFields)
		if len(fields) != cgroupEntryFields {
			continue
		}
		entry := CgroupEntry{
			HierarchyID: fields[0],
			Controllers: fields[1],
			Path:        fields[2],
		}
		out = append(out, entry)
	}
	return out
}

// parseMountInfo reads /proc/self/mountinfo format. Each line contains 10
// whitespace-separated fields with an arbitrary number of optional "tag:value"
// fields appearing between field 6 (Options) and the '-' separator:
//
//	36 35 98:0 /mnt1 /mnt rw,noatime master:1 - ext3 /dev/root rw,errors=continue
//
// Returns successfully parsed entries; unparseable lines are skipped.
func parseMountInfo(data []byte) []MountEntry {
	var out []MountEntry
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, mountinfoInitialBufferBytes), mountinfoMaxLineBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < mountinfoMinFields {
			continue
		}

		// Locate the '-' separator that delimits optional fields from the
		// tail (FSType, MountSource, SuperOptions).
		upperBound := len(fields) - mountinfoFixedTail
		sepIdx := -1
		for i := mountinfoOptionalStart; i < upperBound; i++ {
			if fields[i] == "-" {
				sepIdx = i
				break
			}
		}
		if sepIdx == -1 {
			continue
		}

		optional := make([]string, 0, sepIdx-mountinfoOptionalStart)
		optional = append(optional, fields[mountinfoOptionalStart:sepIdx]...)

		entry := MountEntry{
			MountID:        fields[0],
			ParentID:       fields[1],
			MajorMinor:     fields[2],
			Root:           fields[3],
			MountPoint:     fields[4],
			Options:        fields[5],
			OptionalFields: optional,
			FSType:         fields[sepIdx+1],
			MountSource:    fields[sepIdx+2],
			SuperOptions:   fields[sepIdx+3],
		}
		out = append(out, entry)
	}
	return out
}
