package platform

// Environment is the unified injection struct supplied to every Probe.Run
// invocation. It bundles the OS abstractions probes need to perform their
// measurements: a PlatformReader for raw filesystem access, a ProcfsReader
// for /proc parsing, a SysfsReader for /sys parsing, and a CommandRunner
// for bounded subprocess execution.
//
// Environment is a value type whose fields are reference-typed pointers
// and interfaces. The Environment value itself is not deeply immutable:
// the fields it carries (e.g. *ProcfsReader, *SysfsReader) are shared
// references that may be observed concurrently by sibling probes executed
// in parallel by the orchestrator.
//
// Probes MUST NOT mutate shared dependencies unless those dependencies
// explicitly document that they are safe for concurrent mutation. The
// canonical concurrent-safe implementations in this package are:
//
//   - PlatformReader: MemPlatformReader (RWMutex-protected); OSPlatformReader
//     is safe to call from multiple goroutines because each os.* call is
//     independent and the underlying file descriptors are independent.
//   - ScopedReader: ScopedMemReader (RWMutex-protected); ScopedOSReader
//     is safe to call from multiple goroutines because each openat2 / fd
//     operation is independent and Linux fds are safe for concurrent use.
//   - ProcfsReader / SysfsReader: stateless wrappers; their methods only
//     forward to a ScopedReader and do not retain state between calls.
//   - CommandRunner: FakeCommandRunner is RWMutex-safe; OSCommandRunner
//     spawns independent subprocesses per call.
//
// Probe implementations are responsible for honoring the supplied
// context.Context and for not retaining references to Environment fields
// past the lifetime of Run.
type Environment struct {
	Reader PlatformReader
	Procfs *ProcfsReader
	Sysfs  *SysfsReader
	Runner CommandRunner
}

// NewEnvironment constructs an Environment from explicit components. Any
// nil component is preserved as nil so callers can detect missing
// dependencies rather than silently substituting defaults.
func NewEnvironment(reader PlatformReader, procfs *ProcfsReader, sysfs *SysfsReader, runner CommandRunner) Environment {
	return Environment{
		Reader: reader,
		Procfs: procfs,
		Sysfs:  sysfs,
		Runner: runner,
	}
}

// NewTestEnvironment is a convenience constructor that wires a MemPlatformReader
// to fresh ProcfsReader and SysfsReader instances rooted at "/proc" and "/sys"
// respectively, paired with the supplied CommandRunner.
//
// The PlatformReader field is set to the supplied mem so probe code that
// still uses Reader can interact with the same in-memory tree. The
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
	procfs := NewProcfsReader(procScoped, defaultProcRoot)
	sysfs := NewSysfsReader(sysScoped, defaultSysRoot)
	return NewEnvironment(backing, procfs, sysfs, runner)
}
