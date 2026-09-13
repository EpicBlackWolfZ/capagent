package host

import (
	"encoding/binary"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"io/fs"
	"testing"
)

const helperDenied = "denied helper"

func TestMappingCapabilityFormats(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		version uint32
		size    int
	}{{1, 12}, {2, 20}, {3, 24}} {
		data := make([]byte, tc.size)
		binary.LittleEndian.PutUint32(data, tc.version<<24|1)
		binary.LittleEndian.PutUint32(data[4:], 1<<7|1<<6)
		caps, err := ParseMappingCapabilities(platform.CapabilityAttribute{Present: true, Bytes: data})
		if err != nil || !caps.SetUID || !caps.SetGID || !caps.Effective || caps.Revision != tc.version {
			t.Fatal(caps, err)
		}
	}
	for _, data := range [][]byte{nil, make([]byte, 11), make([]byte, 12), append([]byte{2, 0, 0, 2}, make([]byte, 16)...)} {
		if _, err := ParseMappingCapabilities(platform.CapabilityAttribute{Present: true, Bytes: data}); err == nil {
			t.Fatal("invalid capabilities")
		}
	}
	caps, err := ParseMappingCapabilities(platform.CapabilityAttribute{})
	if err != nil || caps.Present {
		t.Fatal(caps, err)
	}
}

func mappingEnvironment(t *testing.T, change func(*platform.MemPlatformReader)) platform.Environment {
	t.Helper()
	mem := platform.NewMemPlatformReader()
	for _, p := range []string{"/etc", "/usr", "/usr/bin", "/usr/local", "/usr/local/bin", "/bin"} {
		mem.AddDir(p, 0o755)
	}
	mem.AddFile("/etc/subuid", []byte("alice:100000:30\n1000:200000:5\n"), 0o644)
	mem.AddFile("/etc/subgid", []byte("alice:300000:12\n"), 0o644)
	mem.AddFile("/etc/nsswitch.conf", []byte("passwd: files\nsubid: files\n"), 0o644)
	for _, p := range []string{"/usr/bin/newuidmap", "/usr/bin/newgidmap"} {
		mem.AddFile(p, nil, 0o755|fs.ModeSetuid)
		if err := mem.SetOwnership(p, platform.FileOwnership{}); err != nil {
			t.Fatal(err)
		}
		if err := mem.SetExecutableAccess(p, true, nil); err != nil {
			t.Fatal(err)
		}
	}
	if change != nil {
		change(mem)
	}
	files := platform.NewScopedMemReaderWithFilesystems("/", mem, map[string]platform.FilesystemInfo{"/": {FlagsKnown: true}})
	t.Cleanup(func() { files.Close() })
	return platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(model.EvaluationScope{RunID: "subids", ContextID: "alice"}).
		WithHost(platform.HostSnapshot{}, platform.HostMetadata{})
}
func TestSubIDProbeAndHelperObstacles(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"ready", "absent", helperDenied, "wrong kind", "no privilege", "no execute", "capabilities", "cap error"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := mappingEnvironment(t, func(mem *platform.MemPlatformReader) {
				p := "/usr/bin/newuidmap"
				switch name {
				case "absent":
					mem.AddError(p, fs.ErrNotExist)
				case helperDenied:
					mem.AddError(p, fs.ErrPermission)
				case "wrong kind":
					mem.AddDir(p, 0o755)
				case "no privilege":
					mem.AddFile(p, nil, 0o755)
					mem.SetOwnership(p, platform.FileOwnership{})
				case "no execute":
					mem.SetExecutableAccess(p, false, nil)
				case "cap error":
					mem.SetFileCapabilities(p, platform.CapabilityAttribute{}, fs.ErrPermission)
				case "capabilities":
					mem.AddFile(p, nil, 0o755)
					mem.SetOwnership(p, platform.FileOwnership{})
					mem.SetExecutableAccess(p, true, nil)
					data := make([]byte, 20)
					binary.LittleEndian.PutUint32(data, 2<<24|1)
					binary.LittleEndian.PutUint32(data[4:], 1<<7)
					mem.SetFileCapabilities(p, platform.CapabilityAttribute{Present: true, Bytes: data}, nil)
				}
			})
			obs, err := (SubIDProbe{Target: model.UserIdentity{UID: 1000, Username: "alice"}, Now: testClock}).Run(t.Context(), env)
			if err != nil || *obs.SubIDs.UID.Total != 35 || *obs.SubIDs.GID.Total != 12 {
				t.Fatal(obs, err)
			}
			helper := obs.SubIDs.Helpers[0]
			switch name {
			case "ready", "capabilities", "no privilege":
				if helper.Usable != nil {
					t.Fatal(helper)
				}
			case helperDenied:
				if helper.Present != nil || helper.Path != "/usr/bin/newuidmap" {
					t.Fatal(helper)
				}
			case "cap error":
				if helper.Capabilities != nil {
					t.Fatal(helper)
				}
			default:
				if helper.Usable == nil || *helper.Usable {
					t.Fatal(helper)
				}
			}
		})
	}
}
func TestSubIDReadFailuresAndProviders(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"missing", helperDenied, "broken allocation", "external", "provider unknown", "account unknown"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := mappingEnvironment(t, func(mem *platform.MemPlatformReader) {
				switch name {
				case "missing":
					mem.AddError("/etc/subuid", fs.ErrNotExist)
				case helperDenied:
					mem.AddError("/etc/subuid", fs.ErrPermission)
				case "broken allocation":
					mem.AddFile("/etc/subuid", []byte("alice:xx:2"), 0o644)
				case "external":
					mem.AddFile("/etc/nsswitch.conf", []byte("subid: sss"), 0o644)
				case "provider unknown":
					mem.AddError("/etc/nsswitch.conf", fs.ErrPermission)
				}
			})
			target := model.UserIdentity{UID: 1000, Username: "alice"}
			if name == "account unknown" {
				target.Username = ""
			}
			obs, _ := (SubIDProbe{Target: target, Now: testClock}).Run(t.Context(), env)
			a := obs.SubIDs.UID
			switch name {
			case "missing":
				if a.Present == nil || *a.Present || a.Total == nil || *a.Total != 0 {
					t.Fatal(a)
				}
			case helperDenied, "broken allocation", "account unknown":
				if a.Total != nil || obs.Completeness != model.Partial {
					t.Fatal(a)
				}
			default:
				if obs.SubIDs.Provider == "files" || obs.Completeness != model.Partial {
					t.Fatal(obs.SubIDs)
				}
			}
		})
	}
	for _, data := range []string{"subid: files extra", "subid: files\nsubid: sss", "subid:"} {
		if subIDProvider([]byte(data), nil) != "unknown" {
			t.Fatal(data)
		}
	}
	if subIDProvider(nil, fs.ErrNotExist) != "files" {
		t.Fatal("default provider")
	}
}

func TestMappingExecutionRestrictions(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"nosuid", "noexec", "nnp"} {
		h := model.MappingHelper{}
		switch kind {
		case "nosuid":
			h.NoSUID = ptr(true)
		case "noexec":
			h.NoExec = ptr(true)
		case "nnp":
			h.NoNewPrivileges = ptr(true)
		}
		classifyMappingPrivilege(&h)
		if kind == "noexec" {
			if h.Usable == nil || *h.Usable {
				t.Fatal(h)
			}
		} else if h.PrivilegeBlocked == nil || !*h.PrivilegeBlocked {
			t.Fatal(h)
		}
	}
}

func TestMappingCapabilitiesPreserveInheritableAndRejectReservedRoot(t *testing.T) {
	t.Parallel()
	data := make([]byte, 24)
	binary.LittleEndian.PutUint32(data, 3<<24|1)
	binary.LittleEndian.PutUint32(data[8:], 1<<7|1<<6)
	caps, err := ParseMappingCapabilities(platform.CapabilityAttribute{Present: true, Bytes: data})
	if err != nil || !caps.InheritableSetUID || !caps.InheritableSetGID || caps.SetUID || caps.SetGID {
		t.Fatal(caps, err)
	}
	binary.LittleEndian.PutUint32(data[20:], ^uint32(0))
	if _, err := ParseMappingCapabilities(platform.CapabilityAttribute{Present: true, Bytes: data}); err == nil {
		t.Fatal("reserved root ID accepted")
	}
}
