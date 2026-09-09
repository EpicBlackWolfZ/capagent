package platform

// Environment is the unified injection struct supplied to every Probe.Run
// invocation. It bundles the OS abstractions probes need to perform their
// measurements: a PlatformReader for raw filesystem access, a ProcfsReader
// for /proc parsing, a SysfsReader for /sys parsing, and a CommandRunner
// for bounded subprocess execution.
//
// Environment values are immutable once constructed; probes MUST NOT mutate
// the readers or runner they receive.
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
// It is intended exclusively for tests. Production code must use NewEnvironment
// with explicit OS-backed components.
func NewTestEnvironment(mem *MemPlatformReader, runner CommandRunner) Environment {
	var reader PlatformReader
	if mem == nil {
		reader = NewMemPlatformReader()
	} else {
		reader = mem
	}
	procfs := NewProcfsReader(reader, defaultProcRoot)
	sysfs := NewSysfsReader(reader, defaultSysRoot)
	return NewEnvironment(reader, procfs, sysfs, runner)
}