package platform

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvalidCommandSpec = errors.New("invalid command specification")
	ErrInvalidEnvPolicy   = errors.New("invalid environment policy")
)

// EnvPolicy is an immutable snapshot. Its zero value supplies only PATH and
// LC_ALL defaults. It never inherits ambient variables during command execution.
type EnvPolicy struct{ variables []string }

// NewEnvPolicy snapshots named inherited variables and applies explicit overrides
// after fixed defaults. Absent inherited keys are omitted; empty values are kept.
// Runtime endpoints, configuration and proxy settings require explicit selection.
// Callers must not mutate inputs during this call. No input aliases are retained.
func NewEnvPolicy(inherit []string, overrides map[string]string) (EnvPolicy, error) {
	values := map[string]string{"PATH": "/usr/bin:/bin", "LC_ALL": "C"}
	for _, name := range inherit {
		if !validEnvName(name) {
			return EnvPolicy{}, policyError(ErrInvalidEnvPolicy, "invalid inherited variable name")
		}
		if value, ok := os.LookupEnv(name); ok {
			values[name] = value
		}
	}
	for name, value := range overrides {
		if !validEnvName(name) || strings.ContainsRune(value, 0) {
			return EnvPolicy{}, policyError(ErrInvalidEnvPolicy, "invalid variable name or value")
		}
		values[name] = value
	}
	variables := make([]string, 0, len(values))
	for name, value := range values {
		variables = append(variables, name+"="+value)
	}
	sort.Strings(variables)
	return EnvPolicy{variables: variables}, nil
}

func validEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, c := range name {
		letter := c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
		digit := c >= '0' && c <= '9'
		if c == '_' || letter || i > 0 && digit {
			continue
		}
		return false
	}
	return true
}

// Variables returns a caller-owned copy of the effective environment. Values are
// sensitive; this explicit inspection surface is not a diagnostic representation.
func (p EnvPolicy) Variables() []string {
	if p.variables == nil {
		return []string{"LC_ALL=C", "PATH=/usr/bin:/bin"}
	}
	return append([]string(nil), p.variables...)
}

// Format redacts all environment material, including with %#v.
func (p EnvPolicy) Format(s fmt.State, _ rune) { writePolicyLabel(s, "EnvPolicy[redacted]") }

// CommandSpec declares an absolute executable and literal argument vector. Empty
// Dir means /; zero Timeout selects the runner default; a positive value overrides
// it. Negative timeouts are invalid. Credentials/namespaces remain those of the
// current process: this specification is not target-user switching or a sandbox.
// Args must not be mutated while Run is using the specification.
type CommandSpec struct {
	Path    string
	Args    []string
	Env     EnvPolicy
	Dir     string
	Timeout time.Duration
}

// Format deliberately omits paths, arguments and environment values.
func (s CommandSpec) Format(state fmt.State, _ rune) {
	writePolicyLabel(state, "CommandSpec[redacted]")
}

func (s CommandSpec) normalized() (CommandSpec, error) {
	if !filepath.IsAbs(s.Path) || strings.ContainsRune(s.Path, 0) {
		return CommandSpec{}, policyError(ErrInvalidCommandSpec, "executable must be absolute and NUL-free")
	}
	for _, arg := range s.Args {
		if strings.ContainsRune(arg, 0) {
			return CommandSpec{}, policyError(ErrInvalidCommandSpec, "argument contains NUL")
		}
	}
	if s.Dir == "" {
		s.Dir = "/"
	}
	if !filepath.IsAbs(s.Dir) || strings.ContainsRune(s.Dir, 0) {
		return CommandSpec{}, policyError(ErrInvalidCommandSpec, "directory must be absolute and NUL-free")
	}
	if s.Timeout < 0 {
		return CommandSpec{}, policyError(ErrInvalidCommandSpec, "negative timeout")
	}
	return cloneCommandSpec(s), nil
}

func cloneCommandSpec(s CommandSpec) CommandSpec {
	s.Args = append([]string(nil), s.Args...)
	// EnvPolicy is immutable, so sharing its private backing slice is safe.
	return s
}

// commandError retains machine-readable error identity without formatting raw
// command material. Explicitly unwrapping it may expose sensitive OS error data.
type commandError struct {
	message string
	cause   error
}

func (e *commandError) Error() string              { return e.message }
func (e *commandError) Unwrap() error              { return e.cause }
func (e *commandError) Format(s fmt.State, _ rune) { writePolicyLabel(s, e.message) }

func policyError(cause error, reason string) error {
	return &commandError{message: cause.Error() + ": " + reason, cause: cause}
}

func safeCommandError(err error) error {
	if err == nil {
		return nil
	}
	return &commandError{message: "command execution failed", cause: err}
}

func writePolicyLabel(s fmt.State, label string) {
	// fmt.Formatter has no error return; fmt owns the destination writer.
	_, _ = io.WriteString(s, label)
}
