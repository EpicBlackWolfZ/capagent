package platform_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

// Cgroup controller names used repeatedly in sysfs tests.
const (
	cgroupCPU     = "cpu"
	cgroupMemory  = "memory"
	cgroupPids    = "pids"
	cgroupIO      = "io"
	cgroupCpuset  = "cpuset"
	cgroupRdma    = "rdma"
)

func TestSysfsReader_CgroupControllers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "space-delimited controller list",
			input: cgroupCpuset + " " + cgroupCPU + " " + cgroupIO + " " + cgroupMemory + " " + cgroupPids + " " + cgroupRdma + "\n",
			want:  []string{cgroupCpuset, cgroupCPU, cgroupIO, cgroupMemory, cgroupPids, cgroupRdma},
		},
		{
			name:  "tab and multi-space delimiters",
			input: cgroupCPU + "\t" + cgroupMemory + "   " + cgroupPids + "\n",
			want:  []string{cgroupCPU, cgroupMemory, cgroupPids},
		},
		{
			name:  "empty list returns empty slice",
			input: "",
			want:  []string{},
		},
		{
			name:  "whitespace-only yields empty slice",
			input: "  \n  \t\n",
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mem := platform.NewMemPlatformReader()
			mem.AddFile("/sys/fs/cgroup/cgroup.controllers", []byte(tt.input), 0o644)

			r := platform.NewSysfsReader(mem, "/sys")
			got, err := r.CgroupControllers()
			if err != nil {
				t.Fatalf("CgroupControllers: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CgroupControllers = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSysfsReader_CgroupControllersMissingFile(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	r := platform.NewSysfsReader(mem, "/sys")

	if _, err := r.CgroupControllers(); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing-file error = %v, want ErrNotExist", err)
	}
}

func TestSysfsReader_ReadCgroupController(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	mem.AddFile("/sys/fs/cgroup/cpu", []byte("cpu cgroup controller"), 0o644)
	mem.AddFile("/sys/fs/cgroup/memory", []byte("memory cgroup controller"), 0o644)

	r := platform.NewSysfsReader(mem, "/sys")

	data, err := r.ReadCgroupController(cgroupCPU)
	if err != nil {
		t.Fatalf("ReadCgroupController(cpu): %v", err)
	}
	if string(data) != "cpu cgroup controller" {
		t.Errorf("ReadCgroupController(cpu) = %q", string(data))
	}

	data, err = r.ReadCgroupController(cgroupMemory)
	if err != nil {
		t.Fatalf("ReadCgroupController(memory): %v", err)
	}
	if string(data) != "memory cgroup controller" {
		t.Errorf("ReadCgroupController(memory) = %q", string(data))
	}
}

func TestSysfsReader_ReadCgroupController_Empty(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	r := platform.NewSysfsReader(mem, "/sys")

	if _, err := r.ReadCgroupController(""); err == nil {
		t.Error("expected error for empty controller name")
	}
}

func TestSysfsReader_ReadCgroupController_Missing(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	r := platform.NewSysfsReader(mem, "/sys")

	if _, err := r.ReadCgroupController("nonexistent"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing-controller error = %v, want ErrNotExist", err)
	}
}

func TestSysfsReader_SELinux(t *testing.T) {
	t.Parallel()

	t.Run("present and enforcing", func(t *testing.T) {
		t.Parallel()

		mem := platform.NewMemPlatformReader()
		mem.AddDir("/sys/fs/selinux", 0o755)
		mem.AddFile("/sys/fs/selinux/enforce", []byte("1\n"), 0o644)

		r := platform.NewSysfsReader(mem, "/sys")

		present, err := r.SELinuxPresent()
		if err != nil {
			t.Fatalf("SELinuxPresent: %v", err)
		}
		if !present {
			t.Error("SELinuxPresent = false, want true")
		}

		mode, err := r.SELinuxMode()
		if err != nil {
			t.Fatalf("SELinuxMode: %v", err)
		}
		if mode != "1" {
			t.Errorf("SELinuxMode = %q, want \"1\"", mode)
		}

		enforcing, err := r.IsSELinuxEnforcing()
		if err != nil {
			t.Fatalf("IsSELinuxEnforcing: %v", err)
		}
		if !enforcing {
			t.Error("IsSELinuxEnforcing = false, want true")
		}
	})

	t.Run("present and permissive", func(t *testing.T) {
		t.Parallel()

		mem := platform.NewMemPlatformReader()
		mem.AddDir("/sys/fs/selinux", 0o755)
		mem.AddFile("/sys/fs/selinux/enforce", []byte("0\n"), 0o644)

		r := platform.NewSysfsReader(mem, "/sys")

		enforcing, err := r.IsSELinuxEnforcing()
		if err != nil {
			t.Fatalf("IsSELinuxEnforcing: %v", err)
		}
		if enforcing {
			t.Error("IsSELinuxEnforcing = true, want false")
		}
	})

	t.Run("absent subsystem is not error", func(t *testing.T) {
		t.Parallel()

		mem := platform.NewMemPlatformReader()

		r := platform.NewSysfsReader(mem, "/sys")

		present, err := r.SELinuxPresent()
		if err != nil {
			t.Fatalf("SELinuxPresent: %v", err)
		}
		if present {
			t.Error("SELinuxPresent = true, want false")
		}

		mode, err := r.SELinuxMode()
		if err != nil {
			t.Fatalf("SELinuxMode on absent: %v", err)
		}
		if mode != "" {
			t.Errorf("SELinuxMode on absent = %q, want empty", mode)
		}

		enforcing, err := r.IsSELinuxEnforcing()
		if err != nil {
			t.Fatalf("IsSELinuxEnforcing on absent: %v", err)
		}
		if enforcing {
			t.Error("IsSELinuxEnforcing on absent = true, want false")
		}
	})

	t.Run("permission error surfaces", func(t *testing.T) {
		t.Parallel()

		mem := platform.NewMemPlatformReader()
		mem.AddError("/sys/fs/selinux", syscall.EACCES)

		r := platform.NewSysfsReader(mem, "/sys")

		if _, err := r.SELinuxPresent(); !errors.Is(err, syscall.EACCES) {
			t.Errorf("SELinuxPresent error = %v, want EACCES", err)
		}
	})
}

func TestSysfsReader_AppArmor(t *testing.T) {
	t.Parallel()

	t.Run("present", func(t *testing.T) {
		t.Parallel()

		mem := platform.NewMemPlatformReader()
		mem.AddDir("/sys/kernel/security/apparmor", 0o755)

		r := platform.NewSysfsReader(mem, "/sys")

		got, err := r.AppArmorPresent()
		if err != nil {
			t.Fatalf("AppArmorPresent: %v", err)
		}
		if !got {
			t.Error("AppArmorPresent = false, want true")
		}
	})

	t.Run("absent is not error", func(t *testing.T) {
		t.Parallel()

		mem := platform.NewMemPlatformReader()

		r := platform.NewSysfsReader(mem, "/sys")

		got, err := r.AppArmorPresent()
		if err != nil {
			t.Fatalf("AppArmorPresent: %v", err)
		}
		if got {
			t.Error("AppArmorPresent = true, want false")
		}
	})

	t.Run("permission error surfaces", func(t *testing.T) {
		t.Parallel()

		mem := platform.NewMemPlatformReader()
		mem.AddError("/sys/kernel/security/apparmor", syscall.EACCES)

		r := platform.NewSysfsReader(mem, "/sys")

		if _, err := r.AppArmorPresent(); !errors.Is(err, syscall.EACCES) {
			t.Errorf("AppArmorPresent error = %v, want EACCES", err)
		}
	})
}

func TestSysfsReader_CustomRoot(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	mem.AddFile("/fixtures/sys/fs/cgroup/cgroup.controllers", []byte(cgroupCPU+" "+cgroupMemory+"\n"), 0o644)

	r := platform.NewSysfsReader(mem, "/fixtures/sys")
	got, err := r.CgroupControllers()
	if err != nil {
		t.Fatalf("CgroupControllers: %v", err)
	}
	want := []string{cgroupCPU, cgroupMemory}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CgroupControllers = %+v, want %+v", got, want)
	}

	if r.Root() != "/fixtures/sys" {
		t.Errorf("Root = %q, want /fixtures/sys", r.Root())
	}
}

func TestSysfsReader_DefaultRoot(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	r := platform.NewSysfsReader(mem, "")
	if r.Root() != "/sys" {
		t.Errorf("Root = %q, want /sys", r.Root())
	}
}

func TestSysfsReader_NilReaderRejected(t *testing.T) {
	t.Parallel()

	if got := platform.NewSysfsReader(nil, "/sys"); got != nil {
		t.Errorf("NewSysfsReader(nil) = %v, want nil", got)
	}
}

func TestSysfsReader_RealSysfs(t *testing.T) {
	t.Parallel()

	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		t.Skipf("/sys/fs/cgroup/cgroup.controllers not available: %v", err)
	}

	r := platform.NewSysfsReader(platform.NewOSPlatformReader(), "/sys")
	controllers, err := r.CgroupControllers()
	if err != nil {
		t.Fatalf("real CgroupControllers: %v", err)
	}
	// Real hosts always expose at least "cpu" and "memory" but we just
	// require a non-empty result.
	if len(controllers) == 0 {
		t.Error("expected at least one cgroup v2 controller on host")
	}

	// Sanity check that path joining uses platform separators.
	want := filepath.Join("/sys", "fs/cgroup/cgroup.controllers")
	if want != "/sys/fs/cgroup/cgroup.controllers" {
		t.Skipf("test environment has non-POSIX separators: %v", want)
	}
}

// TestSysfsReader_RootOrDotSubpath exercises the joinRoot short-circuit
// branches when the supplied subpath normalises to "." or "/".
func TestSysfsReader_RootOrDotSubpath(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	// Seed a file at the configured root so joinRoot returns it.
	mem.AddFile("/sys", []byte("data"), 0o644)

	r := platform.NewSysfsReader(mem, "/sys")

	for _, sub := range []string{".", "/", ""} {
		got, err := r.ReadSysFile(sub)
		if err != nil {
			t.Errorf("ReadSysFile(%q): %v", sub, err)
		}
		if string(got) != "data" {
			t.Errorf("ReadSysFile(%q) = %q, want data", sub, string(got))
		}
	}
}

// TestSysfsReader_SELinuxModeOnPresent ensures SELinuxMode returns the
// file content when the enforce file is present.
func TestSysfsReader_SELinuxModeOnPresent(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	mem.AddDir("/sys/fs/selinux", 0o755)
	mem.AddFile("/sys/fs/selinux/enforce", []byte("0"), 0o644)

	r := platform.NewSysfsReader(mem, "/sys")
	mode, err := r.SELinuxMode()
	if err != nil {
		t.Fatalf("SELinuxMode: %v", err)
	}
	if mode != "0" {
		t.Errorf("SELinuxMode = %q, want 0", mode)
	}
}

// TestSysfsReader_IsSELinuxEnforcingWhenNotPresent verifies the absent
// subsystem path returns (false, nil) without raising an error.
func TestSysfsReader_IsSELinuxEnforcingWhenNotPresent(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	r := platform.NewSysfsReader(mem, "/sys")

	enforcing, err := r.IsSELinuxEnforcing()
	if err != nil {
		t.Fatalf("IsSELinuxEnforcing on absent: %v", err)
	}
	if enforcing {
		t.Error("IsSELinuxEnforcing = true, want false")
	}
}

// TestSysfsReader_CgroupControllersDeduplicates exercises the dedup branch
// of splitControllerList via the public CgroupControllers entry point.
func TestSysfsReader_CgroupControllersDeduplicates(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	mem.AddFile("/sys/fs/cgroup/cgroup.controllers", []byte("cpu cpu memory cpu memory\n"), 0o644)

	r := platform.NewSysfsReader(mem, "/sys")
	got, err := r.CgroupControllers()
	if err != nil {
		t.Fatalf("CgroupControllers: %v", err)
	}
	want := []string{"cpu", "memory"}
	if len(got) != len(want) {
		t.Fatalf("CgroupControllers = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}