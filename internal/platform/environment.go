package platform

import (
	"context"
	"os"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

// Environment is an owner-constructed, read-only view of shared probe services.
// Copies share the services, which must support concurrent operations. Owners
// must not replace or reconfigure services during Run and must close resources
// only after all runs join. Probes must not retain services beyond Run.
// Accessors hide concrete setup/teardown handles; operational state such as
// synchronized command-call recording may still change. Custom providers must
// honor the same ownership and concurrency contract.
type Environment struct {
	reader PlatformReader
	procfs ProcfsView
	sysfs  SysfsView
	runner CommandRunner
	scope  model.EvaluationScope
	files  ScopedView
}

// ScopedView retains the kernel/memory containment boundary without exposing
// the owner's Close method to probes.
type ScopedView interface {
	ReadFile(context.Context, string) ([]byte, error)
	Stat(string) (os.FileInfo, error)
	ReadDir(context.Context, string) ([]os.DirEntry, error)
	Readlink(string) (string, error)
	FileCapabilities(context.Context, string) (CapabilityAttribute, error)
	Root() string
}

type scopedView struct{ ScopedView }

func (e Environment) WithFiles(files ScopedReader) Environment {
	if files == nil {
		e.files = nil
	} else {
		e.files = scopedView{files}
	}
	return e
}
func (e Environment) Files() ScopedView { return e.files }

// WithScope returns a new service view bound to an explicit evaluation candidate.
// Scope is a value of immutable strings; no caller-owned aliases are retained.
func (e Environment) WithScope(scope model.EvaluationScope) Environment { e.scope = scope; return e }
func (e Environment) Scope() model.EvaluationScope                      { return e.scope }

// ProcfsView exposes measurements without access to the shared wrapper itself.
type ProcfsView interface {
	Root() string
	ReadProcFile(context.Context, string) ([]byte, error)
	ReadSelf(context.Context, string) ([]byte, error)
	Mounts(context.Context) ([]MountEntry, error)
	Filesystems(context.Context) ([]FilesystemEntry, error)
	Cgroups(context.Context) ([]CgroupEntry, error)
}

// SysfsView exposes measurements without setup or resource ownership.
type SysfsView interface {
	Root() string
	ReadSysFile(context.Context, string) ([]byte, error)
	ReadCgroupFile(context.Context, string) ([]byte, error)
	CgroupControllers(context.Context) ([]string, error)
	SELinuxPresent() (bool, error)
	SELinuxMode(context.Context) (string, error)
	IsSELinuxEnforcing(context.Context) (bool, error)
	AppArmorPresent() (bool, error)
}

// Private forwarding values prevent downcasts to mutable owner-held services.
type readerView struct{ PlatformReader }
type runnerView struct{ CommandRunner }
type procfsView struct{ ProcfsView }
type sysfsView struct{ SysfsView }

func (e Environment) Reader() PlatformReader { return e.reader }
func (e Environment) Procfs() ProcfsView     { return e.procfs }
func (e Environment) Sysfs() SysfsView       { return e.sysfs }
func (e Environment) Runner() CommandRunner  { return e.runner }

// NewEnvironment wraps explicit services, preserving absent components as nil.
func NewEnvironment(reader PlatformReader, procfs *ProcfsReader, sysfs *SysfsReader, runner CommandRunner) Environment {
	var env Environment
	if reader != nil {
		env.reader = readerView{reader}
	}
	if procfs != nil {
		env.procfs = procfsView{procfs}
	}
	if sysfs != nil {
		env.sysfs = sysfsView{sysfs}
	}
	if runner != nil {
		env.runner = runnerView{runner}
	}
	return env
}

// NewTestEnvironment is a convenience constructor that wires a MemPlatformReader
// to fresh ProcfsReader and SysfsReader instances rooted at "/proc" and "/sys"
// respectively, paired with the supplied CommandRunner.
//
// The Reader accessor delegates to mem so probes can read the same tree. The
// ProcfsReader/SysfsReader are wired through ScopedMemReader instances
// that share the mem backing store; this preserves backward-compatible
// test ergonomics while routing the file methods through the new
// containment boundary.
//
// It is intended exclusively for tests. Production code must use NewEnvironment
// with explicit OS-backed components.
func NewTestEnvironment(mem *MemPlatformReader, runner CommandRunner) Environment {
	var backing *MemPlatformReader
	if mem == nil {
		backing = NewMemPlatformReader()
	} else {
		backing = mem
	}
	procScoped := NewScopedMemReader(defaultProcRoot, backing)
	sysScoped := NewScopedMemReader(defaultSysRoot, backing)
	procfs := NewProcfsReader(procScoped)
	sysfs := NewSysfsReader(sysScoped)
	return NewEnvironment(backing, procfs, sysfs, runner)
}
