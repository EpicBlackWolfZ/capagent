// Package fixture constructs deterministic offline services from versioned
// documents. No fixture command is ever sent to an OS command runner.
package fixture

import (
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/config"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/requirement"
)

const MaxBytes = 2 << 20
const maxDepth = 64
const maxFiles = 4096
const maxTimeoutMillis = 30000

type Provenance struct {
	Kind        string `json:"kind"`
	Description string `json:"description"`
}
type File struct {
	DirectoryRead         *bool                         `json:"directory_read,omitempty"`
	DirectoryWrite        *bool                         `json:"directory_write,omitempty"`
	DirectoryReadFailure  string                        `json:"directory_read_failure,omitempty"`
	DirectoryWriteFailure string                        `json:"directory_write_failure,omitempty"`
	ExecutableAccess      *bool                         `json:"executable_access,omitempty"`
	AccessFailure         string                        `json:"access_failure,omitempty"`
	Capabilities          *platform.CapabilityAttribute `json:"capabilities,omitempty"`
	CapabilityFailure     string                        `json:"capability_failure,omitempty"`
	Path                  string                        `json:"path"`
	Kind                  string                        `json:"kind"`
	Mode                  uint32                        `json:"mode"`
	UID                   *uint32                       `json:"uid,omitempty"`
	GID                   *uint32                       `json:"gid,omitempty"`
	Content               string                        `json:"content,omitempty"`
	Target                string                        `json:"target,omitempty"`
	Failure               string                        `json:"failure,omitempty"`
}
type Command struct {
	Path             string            `json:"path"`
	Args             []string          `json:"args"`
	Environment      map[string]string `json:"environment,omitempty"`
	Directory        string            `json:"directory,omitempty"`
	TimeoutMillis    uint32            `json:"timeout_ms,omitempty"`
	Stdout           string            `json:"stdout"`
	Stderr           string            `json:"stderr,omitempty"`
	ExitCode         int               `json:"exit_code,omitempty"`
	Failure          string            `json:"failure,omitempty"`
	StdoutTruncated  bool              `json:"stdout_truncated,omitempty"`
	StderrTruncated  bool              `json:"stderr_truncated,omitempty"`
	OutputIncomplete bool              `json:"output_incomplete,omitempty"`
}
type Document struct {
	Active        bool                    `json:"active,omitempty"`
	PodmanPath    string                  `json:"podman_path,omitempty"`
	Environment   map[string]string       `json:"environment,omitempty"`
	UserQuery     bool                    `json:"user_query,omitempty"`
	Host          *HostCalls              `json:"host,omitempty"`
	SchemaVersion int                     `json:"schema_version"`
	Probe         string                  `json:"probe,omitempty"`
	RunID         string                  `json:"run_id"`
	Timestamp     time.Time               `json:"timestamp"`
	Provenance    Provenance              `json:"provenance"`
	Context       model.EvaluationContext `json:"context"`
	Runtime       string                  `json:"runtime"`
	Endpoint      string                  `json:"endpoint"`
	Files         []File                  `json:"files"`
	Commands      []Command               `json:"commands"`
	Requirement   jsontext.Value          `json:"requirement"`
}

func (d Document) Scope() model.EvaluationScope {
	return model.EvaluationScope{RunID: d.RunID, ContextID: d.Context.ID, Runtime: d.Runtime, Endpoint: d.Endpoint}
}

func Parse(data []byte) (*Document, error) {
	if err := config.CheckJSON(data, MaxBytes, maxDepth); err != nil {
		return nil, err
	}
	var doc *Document
	if err := json.Unmarshal(data, &doc, json.RejectUnknownMembers(true)); err != nil || doc == nil {
		return nil, errors.New("invalid fixture document")
	}
	if err := validateIdentityPresence(data); err != nil {
		return nil, err
	}
	if err := validate(doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// Domain identities use uint32 values once observed. At the JSON input boundary,
// missing/null UID or GID must not silently create an observed root identity.
func validateIdentityPresence(data []byte) error {
	type identity struct {
		UID *uint32
		GID *uint32
	}
	var presence struct {
		Context struct {
			Identity struct{ Current, Target, Execution *identity }
		}
	}
	if err := json.Unmarshal(data, &presence, json.MatchCaseInsensitiveNames(true)); err != nil {
		return errors.New("invalid identity metadata")
	}
	for _, user := range []*identity{presence.Context.Identity.Current, presence.Context.Identity.Target,
		presence.Context.Identity.Execution} {
		if user != nil && (user.UID == nil || user.GID == nil) {
			return errors.New("observed identity requires explicit UID and GID")
		}
	}
	return nil
}

func validate(d *Document) error {
	if d == nil || d.SchemaVersion != 1 || d.Timestamp.IsZero() || d.Scope().IsValid() != nil {
		return errors.New("invalid fixture identity or version")
	}
	if err := d.Context.Identity.IsValid(); err != nil {
		return err
	}
	if err := validateProbeOptions(d); err != nil {
		return err
	}
	if !validFixtureRuntime(d) {
		return errors.New("fixture runtime must be local Podman")
	}
	if (d.Provenance.Kind != "synthetic" && d.Provenance.Kind != "captured") || d.Provenance.Description == "" {
		return errors.New("fixture requires explicit captured or synthetic provenance")
	}
	commands := 1
	if d.Probe == assessmentProbe {
		commands = len(d.Commands)
		if err := validateAssessment(d); err != nil {
			return err
		}
	}
	if d.Probe == hostProbe || d.Probe == contextProbe {
		commands = len(d.Commands)
		if err := validateHost(d); err != nil {
			return err
		}
	}
	if d.Probe == "inspection" {
		commands = inspectionCommandCount
	}
	if len(d.Files) > maxFiles || len(d.Commands) != commands {
		return errors.New("fixture file or command count exceeds its probe contract")
	}
	if _, err := parseRequirement(d); err != nil {
		return err
	}
	return validateFiles(d.Files)
}

func validateProbeOptions(d *Document) error {
	if d.UserQuery && d.Probe != contextProbe && d.Probe != assessmentProbe {
		return errors.New("user query requires context or assessment fixture")
	}
	if d.Probe != assessmentProbe && (d.Active || d.PodmanPath != "" || len(d.Environment) != 0) {
		return errors.New("assessment fields require assessment fixture")
	}
	switch d.Probe {
	case "", "info", "version", "inspection", hostProbe, contextProbe, assessmentProbe:
		return nil
	default:
		return errors.New("unknown fixture probe")
	}
}

func validateFiles(files []File) error {
	seen := make(map[string]bool)
	for _, file := range files {
		if platform.ValidateSubpath(file.Path) != nil || file.Path == "." || path.Clean(file.Path) != file.Path || seen[file.Path] {
			return errors.New("invalid or duplicate fixture file path")
		}
		seen[file.Path] = true
		if (file.UID == nil) != (file.GID == nil) {
			return errors.New("fixture ownership requires both UID and GID")
		}
		switch file.Kind {
		case "file", "directory", "symlink", "socket", "error":
		default:
			return errors.New("invalid fixture file kind")
		}
		for _, code := range []string{file.Failure,
			file.AccessFailure,
			file.CapabilityFailure,
			file.DirectoryReadFailure,
			file.DirectoryWriteFailure} {
			if _, err := failure(code); err != nil {
				return err
			}
		}
		if file.Capabilities != nil && len(file.Capabilities.Bytes) > 256 {
			return errors.New("fixture capability attribute too long")
		}
	}
	return nil
}

type Services struct {
	Policy      platform.EnvPolicy
	Environment platform.Environment
	Command     platform.CommandSpec
	InfoCommand platform.CommandSpec
	Requirement *requirement.Node
	files       platform.ScopedReader
}

func (s *Services) Close() error { return s.files.Close() }

// Open snapshots file/command inputs into the existing M1.1 test doubles. The
// caller retains Document ownership and must not mutate it concurrently.
func Open(d *Document) (*Services, error) {
	if err := validate(d); err != nil {
		return nil, err
	}
	mem := platform.NewMemPlatformReader()
	for _, file := range d.Files {
		if err := addFile(mem, file); err != nil {
			return nil, err
		}
	}
	runner := platform.NewFakeCommandRunner()
	specs := make([]platform.CommandSpec, 0, len(d.Commands))
	for _, command := range d.Commands {
		spec, err := registerCommand(runner, command)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	node, err := parseRequirement(d)
	if err != nil {
		return nil, err
	}
	files := platform.NewScopedMemReader("/", mem)
	if d.Probe == hostProbe || d.Probe == contextProbe || d.Probe == assessmentProbe {
		files = platform.NewScopedMemReaderWithFilesystems("/", mem, hostFilesystems(d))
	}
	env := platform.NewEnvironment(nil, nil, nil, runner).WithFiles(files).WithScope(d.Scope())
	if d.Probe == hostProbe || d.Probe == contextProbe || d.Probe == assessmentProbe {
		env = env.WithHost(hostSnapshot(d), platform.NewHostMetadata(runner)).WithUserManager(platform.NewUserManager(runner))
	}
	services := &Services{Environment: env, Requirement: node, files: files}
	if d.Probe == assessmentProbe {
		policy, err := assessmentPolicy(d)
		if err != nil {
			return nil, errors.Join(err, files.Close())
		}
		services.Policy = policy
		if err := validateAssessmentCommands(d, specs, policy); err != nil {
			return nil, errors.Join(err, files.Close())
		}
		return services, nil
	}
	if len(specs) > 0 {
		services.Command = specs[0]
	}
	if d.Probe == "inspection" {
		services.InfoCommand = specs[1]
		if err := validateInspection(d, services); err != nil {
			return nil, errors.Join(err, files.Close())
		}
	}
	return services, nil
}

func addFile(mem *platform.MemPlatformReader, file File) error {
	name := "/" + file.Path
	mode := os.FileMode(file.Mode)
	switch file.Kind {
	case "file":
		mem.AddFile(name, []byte(file.Content), mode)
	case "directory":
		mem.AddDir(name, mode)
	case "socket":
		if err := mem.AddSpecial(name, mode|os.ModeSocket); err != nil {
			return err
		}
	case "symlink":
		mem.AddSymlink(name, file.Target)
	case "error":
		err, _ := failure(file.Failure)
		if err == nil {
			return errors.New("error fixture needs a failure")
		}
		mem.AddError(name, err)
	}
	if file.UID != nil {
		if err := mem.SetOwnership(name, platform.FileOwnership{UID: *file.UID, GID: *file.GID}); err != nil {
			return err
		}
	}
	if file.ExecutableAccess != nil || file.AccessFailure != "" {
		allowed := file.ExecutableAccess != nil && *file.ExecutableAccess
		err, _ := failure(file.AccessFailure)
		if err := mem.SetExecutableAccess(name, allowed, err); err != nil {
			return err
		}
	}
	if err := addDirectoryAccess(mem, name, file); err != nil {
		return err
	}
	if file.Capabilities != nil || file.CapabilityFailure != "" {
		var attribute platform.CapabilityAttribute
		if file.Capabilities != nil {
			attribute = *file.Capabilities
		}
		err, _ := failure(file.CapabilityFailure)
		if err := mem.SetFileCapabilities(name, attribute, err); err != nil {
			return err
		}
	}
	return nil
}

func failure(code string) (error, error) {
	switch code {
	case "":
		return nil, nil
	case "unavailable":
		return errors.New("fixture runtime unavailable"), nil
	case "not_found":
		return fs.ErrNotExist, nil
	case "permission":
		return fs.ErrPermission, nil
	case "timeout":
		return context.DeadlineExceeded, nil
	case "cancelled":
		return context.Canceled, nil
	default:
		return nil, errors.New("unknown fixture failure code")
	}
}

func registerCommand(runner *platform.FakeCommandRunner, command Command) (platform.CommandSpec, error) {
	if command.TimeoutMillis > maxTimeoutMillis {
		return platform.CommandSpec{}, errors.New("fixture timeout exceeds command budget")
	}
	policy, err := platform.NewEnvPolicy(nil, command.Environment)
	if err != nil {
		return platform.CommandSpec{}, err
	}
	spec := platform.CommandSpec{Path: command.Path, Args: append([]string(nil), command.Args...), Env: policy,
		Dir: command.Directory, Timeout: time.Duration(command.TimeoutMillis) * time.Millisecond}
	commandErr, err := failure(command.Failure)
	if err != nil {
		return platform.CommandSpec{}, err
	}
	result := platform.ExecResult{Stdout: []byte(command.Stdout), Stderr: []byte(command.Stderr), ExitCode: command.ExitCode,
		OutputIncomplete: command.OutputIncomplete,
		StdoutTruncated:  command.StdoutTruncated, StderrTruncated: command.StderrTruncated, TimedOut: command.Failure == "timeout"}
	if err := runner.RegisterWithError(spec, result, commandErr); err != nil {
		return platform.CommandSpec{}, err
	}
	return spec, nil
}

func validFixtureRuntime(d *Document) bool {
	return (d.Probe == hostProbe || d.Probe == contextProbe) || (d.Runtime == "podman" && d.Endpoint == "local")
}

func addDirectoryAccess(mem *platform.MemPlatformReader, name string, file File) error {
	for _, row := range []struct {
		writable bool
		allowed  *bool
		code     string
	}{
		{false, file.DirectoryRead, file.DirectoryReadFailure}, {true, file.DirectoryWrite, file.DirectoryWriteFailure},
	} {
		if row.allowed == nil && row.code == "" {
			continue
		}
		err, _ := failure(row.code)
		if err := mem.SetDirectoryAccess(name, row.writable, row.allowed != nil && *row.allowed, err); err != nil {
			return err
		}
	}
	return nil
}
