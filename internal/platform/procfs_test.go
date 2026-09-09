package platform_test

import (
	"errors"
	"os"
	"reflect"
	"syscall"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

// Filesystem names used repeatedly across procfs parser tests; declared
// as named constants so the goconst linter does not flag repeated literals.
const (
	fsTmpfs = "tmpfs"
	fsProc  = "proc"
	fsSysfs = "sysfs"
	fsExt4  = "ext4"
	fsXfs   = "xfs"

	cgroupCPUController = "cpu"
)

// mustOSProcfsReader constructs a kernel-confined ProcfsReader rooted at
// root. It skips the surrounding test if openat2(2) is unavailable.
func mustOSProcfsReader(t *testing.T, root string) *platform.ProcfsReader {
	t.Helper()
	scoped, err := platform.NewScopedOSReader(root)
	if err != nil {
		t.Skipf("ScopedOSReader unavailable: %v", err)
	}
	return platform.NewProcfsReader(scoped)
}

func TestProcfsReader_Filesystems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []platform.FilesystemEntry
	}{
		{
			name:  "mixed nodev and regular filesystems",
			input: "nodev\t" + fsTmpfs + "\nnodev\t" + fsProc + "\nnodev\t" + fsSysfs + "\n\t" + fsExt4 + "\n\t" + fsXfs + "\n",
			want: []platform.FilesystemEntry{
				{Name: fsTmpfs, NoDev: true},
				{Name: fsProc, NoDev: true},
				{Name: fsSysfs, NoDev: true},
				{Name: fsExt4, NoDev: false},
				{Name: fsXfs, NoDev: false},
			},
		},
		{
			name:  "empty file yields no entries",
			input: "",
			want:  nil,
		},
		{
			name:  "blank lines are skipped",
			input: "\nnodev\t" + fsTmpfs + "\n\n\t" + fsExt4 + "\n",
			want: []platform.FilesystemEntry{
				{Name: fsTmpfs, NoDev: true},
				{Name: fsExt4, NoDev: false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mem := platform.NewMemPlatformReader()
			fixtureParents(mem, "/proc/filesystems")
			mem.AddFile("/proc/filesystems", []byte(tt.input), 0o644)

			r := platform.NewProcfsReader(platform.NewScopedMemReader("/proc", mem))
			got, err := r.Filesystems()
			if err != nil {
				t.Fatalf("Filesystems: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Filesystems = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestProcfsReader_Cgroups(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []platform.CgroupEntry
	}{
		{
			name: "cgroup v2 unified hierarchy",
			input: `0::/user.slice/user-1000.slice/session-1.scope
`,
			want: []platform.CgroupEntry{
				{HierarchyID: "0", Controllers: "", Path: "/user.slice/user-1000.slice/session-1.scope"},
			},
		},
		{
			name:  "cgroup v1 multi-hierarchy",
			input: "12:" + cgroupCPUController + ",cpuacct:/user.slice\n\t11:memory:/user.slice\n\t0::/user.slice/user-1000.slice/session-1.scope\n",
			want: []platform.CgroupEntry{
				{HierarchyID: "12", Controllers: cgroupCPUController + ",cpuacct", Path: "/user.slice"},
				{HierarchyID: "11", Controllers: "memory", Path: "/user.slice"},
				{HierarchyID: "0", Controllers: "", Path: "/user.slice/user-1000.slice/session-1.scope"},
			},
		},
		{
			name:  "empty file yields no entries",
			input: "",
			want:  nil,
		},
		{
			name:  "malformed line is skipped",
			input: "12:cpu:/path\nthis-is-garbage\n0::/unified\n",
			want: []platform.CgroupEntry{
				{HierarchyID: "12", Controllers: cgroupCPUController, Path: "/path"},
				{HierarchyID: "0", Controllers: "", Path: "/unified"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mem := platform.NewMemPlatformReader()
			fixtureParents(mem, "/proc/self/cgroup")
			mem.AddFile("/proc/self/cgroup", []byte(tt.input), 0o644)

			r := platform.NewProcfsReader(platform.NewScopedMemReader("/proc", mem))
			got, err := r.Cgroups()
			if err != nil {
				t.Fatalf("Cgroups: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Cgroups = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCgroupEntry_IsUnified(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		entry platform.CgroupEntry
		want  bool
	}{
		{
			name:  "v2 unified",
			entry: platform.CgroupEntry{HierarchyID: "0", Controllers: "", Path: "/x"},
			want:  true,
		},
		{
			name:  "v1 has controllers",
			entry: platform.CgroupEntry{HierarchyID: "1", Controllers: cgroupCPUController, Path: "/x"},
			want:  false,
		},
		{
			name:  "non-zero hierarchy id is not unified",
			entry: platform.CgroupEntry{HierarchyID: "5", Controllers: "", Path: "/x"},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.entry.IsUnified(); got != tt.want {
				t.Errorf("IsUnified = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProcfsReader_Mounts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []platform.MountEntry
	}{
		{
			name: "mountinfo with optional master field",
			input: `36 35 98:0 /mnt1 /mnt rw,noatime master:1 - ext3 /dev/root rw,errors=continue
37 30 0:27 / /sys rw,nosuid,nodev,noexec,relatime shared:7 - sysfs sysfs rw
`,
			want: []platform.MountEntry{
				{
					MountID:        "36",
					ParentID:       "35",
					MajorMinor:     "98:0",
					Root:           "/mnt1",
					MountPoint:     "/mnt",
					Options:        "rw,noatime",
					OptionalFields: []string{"master:1"},
					FSType:         "ext3",
					MountSource:    "/dev/root",
					SuperOptions:   "rw,errors=continue",
				},
				{
					MountID:        "37",
					ParentID:       "30",
					MajorMinor:     "0:27",
					Root:           "/",
					MountPoint:     "/sys",
					Options:        "rw,nosuid,nodev,noexec,relatime",
					OptionalFields: []string{"shared:7"},
					FSType:         "sysfs",
					MountSource:    "sysfs",
					SuperOptions:   "rw",
				},
			},
		},
		{
			name: "mountinfo without optional fields",
			input: `22 21 0:21 / /proc rw,nosuid,nodev,noexec,relatime - proc proc rw
`,
			want: []platform.MountEntry{
				{
					MountID:        "22",
					ParentID:       "21",
					MajorMinor:     "0:21",
					Root:           "/",
					MountPoint:     "/proc",
					Options:        "rw,nosuid,nodev,noexec,relatime",
					OptionalFields: []string{},
					FSType:         "proc",
					MountSource:    "proc",
					SuperOptions:   "rw",
				},
			},
		},
		{
			name: "mountinfo with multiple optional fields",
			input: `99 1 0:99 / /foo rw shared:1 master:2 propagate_from:3 - tmpfs tmpfs rw
`,
			want: []platform.MountEntry{
				{
					MountID:        "99",
					ParentID:       "1",
					MajorMinor:     "0:99",
					Root:           "/",
					MountPoint:     "/foo",
					Options:        "rw",
					OptionalFields: []string{"shared:1", "master:2", "propagate_from:3"},
					FSType:         "tmpfs",
					MountSource:    "tmpfs",
					SuperOptions:   "rw",
				},
			},
		},
		{
			name:  "empty mountinfo",
			input: "",
			want:  nil,
		},
		{
			name: "malformed line missing separator is skipped",
			input: `22 21 0:21 / /proc rw
12 0 0:1 / /good rw - ext4 /dev/sda rw
`,
			want: []platform.MountEntry{
				{
					MountID:        "12",
					ParentID:       "0",
					MajorMinor:     "0:1",
					Root:           "/",
					MountPoint:     "/good",
					Options:        "rw",
					OptionalFields: []string{},
					FSType:         "ext4",
					MountSource:    "/dev/sda",
					SuperOptions:   "rw",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mem := platform.NewMemPlatformReader()
			fixtureParents(mem, "/proc/self/mountinfo")
			mem.AddFile("/proc/self/mountinfo", []byte(tt.input), 0o644)

			r := platform.NewProcfsReader(platform.NewScopedMemReader("/proc", mem))
			got, err := r.Mounts()
			if err != nil {
				t.Fatalf("Mounts: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Mounts mismatch:\ngot  %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestProcfsReader_ReadProcFileAndReadSelf(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	fixtureParents(mem, "/proc/version")
	mem.AddFile("/proc/version", []byte("Linux 6.0.0"), 0o644)
	fixtureParents(mem, "/proc/self/status")
	mem.AddFile("/proc/self/status", []byte("Name:\tbash"), 0o644)

	r := platform.NewProcfsReader(platform.NewScopedMemReader("/proc", mem))

	v, err := r.ReadProcFile("version")
	if err != nil {
		t.Fatalf("ReadProcFile: %v", err)
	}
	if string(v) != "Linux 6.0.0" {
		t.Errorf("ReadProcFile content = %q, want %q", string(v), "Linux 6.0.0")
	}

	s, err := r.ReadSelf("status")
	if err != nil {
		t.Fatalf("ReadSelf: %v", err)
	}
	if string(s) != "Name:\tbash" {
		t.Errorf("ReadSelf content = %q, want %q", string(s), "Name:\tbash")
	}
}

func TestProcfsReader_CustomRoot(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	fixtureParents(mem, "/testdata/proc/filesystems")
	mem.AddFile("/testdata/proc/filesystems", []byte("nodev\ttmpfs\n"), 0o644)

	r := platform.NewProcfsReader(platform.NewScopedMemReader("/testdata/proc", mem))
	got, err := r.Filesystems()
	if err != nil {
		t.Fatalf("Filesystems: %v", err)
	}
	if len(got) != 1 || got[0].Name != "tmpfs" || !got[0].NoDev {
		t.Errorf("Filesystems = %+v, want one tmpfs entry", got)
	}
	if r.Root() != "/testdata/proc" {
		t.Errorf("Root = %q, want %q", r.Root(), "/testdata/proc")
	}
}

func TestProcfsReader_DefaultRootIsProc(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	fixtureParents(mem, "/proc/filesystems")
	mem.AddFile("/proc/filesystems", []byte("nodev\ttmpfs\n"), 0o644)

	r := platform.NewProcfsReader(platform.NewScopedMemReader("/proc", mem))
	if r.Root() != "/proc" {
		t.Errorf("Root = %q, want /proc", r.Root())
	}
}

func TestProcfsReader_NilReaderRejected(t *testing.T) {
	t.Parallel()

	if got := platform.NewProcfsReader(nil); got != nil {
		t.Errorf("NewProcfsReader(nil) = %v, want nil", got)
	}
}

func TestProcfsReader_MissingFilePropagates(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	r := platform.NewProcfsReader(platform.NewScopedMemReader("/proc", mem))

	if _, err := r.Filesystems(); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Filesystems missing-file error = %v, want ErrNotExist", err)
	}
	if _, err := r.Cgroups(); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Cgroups missing-file error = %v, want ErrNotExist", err)
	}
	if _, err := r.Mounts(); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Mounts missing-file error = %v, want ErrNotExist", err)
	}
}

func TestProcfsReader_PermissionErrorPropagates(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	fixtureParents(mem, "/proc/filesystems")
	mem.AddFile("/proc/filesystems", []byte("nodev\ttmpfs\n"), 0o644)
	fixtureParents(mem, "/proc/self/cgroup")
	mem.AddError("/proc/self/cgroup", syscall.EACCES)

	r := platform.NewProcfsReader(platform.NewScopedMemReader("/proc", mem))

	if _, err := r.Filesystems(); err != nil {
		t.Errorf("Filesystems error = %v, want nil", err)
	}
	if _, err := r.Cgroups(); !errors.Is(err, syscall.EACCES) {
		t.Errorf("Cgroups error = %v, want EACCES", err)
	}
}

func TestProcfsReader_RealProcfs(t *testing.T) {
	t.Parallel()

	if _, err := os.Stat("/proc/filesystems"); err != nil {
		t.Skipf("/proc/filesystems not available: %v", err)
	}

	r := mustOSProcfsReader(t, "/proc")
	entries, err := r.Filesystems()
	if err != nil {
		t.Fatalf("real Filesystems: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected at least one filesystem on host /proc/filesystems")
	}
}
