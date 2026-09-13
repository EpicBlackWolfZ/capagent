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
	Path    string  `json:"path"`
	Kind    string  `json:"kind"`
	Mode    uint32  `json:"mode"`
	UID     *uint32 `json:"uid,omitempty"`
	GID     *uint32 `json:"gid,omitempty"`
	Content string  `json:"content,omitempty"`
	Target  string  `json:"target,omitempty"`
	Failure string  `json:"failure,omitempty"`
}
type Command struct {
	Path            string            `json:"path"`
	Args            []string          `json:"args"`
	Environment     map[string]string `json:"environment,omitempty"`
	Directory       string            `json:"directory,omitempty"`
	TimeoutMillis   uint32            `json:"timeout_ms,omitempty"`
	Stdout          string            `json:"stdout"`
	Stderr          string            `json:"stderr,omitempty"`
	ExitCode        int               `json:"exit_code,omitempty"`
	Failure         string            `json:"failure,omitempty"`
	StdoutTruncated bool              `json:"stdout_truncated,omitempty"`
	StderrTruncated bool              `json:"stderr_truncated,omitempty"`
}
type Document struct {
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
			Identity struct{ Current, Target *identity }
		}
	}
	if err := json.Unmarshal(data, &presence, json.MatchCaseInsensitiveNames(true)); err != nil {
		return errors.New("invalid identity metadata")
	}
	for _, user := range []*identity{presence.Context.Identity.Current, presence.Context.Identity.Target} {
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
	if d.Runtime != "podman" || d.Endpoint != "local" {
		return errors.New("fixture runtime must be local Podman")
	}
	if (d.Provenance.Kind != "synthetic" && d.Provenance.Kind != "captured") || d.Provenance.Description == "" {
		return errors.New("fixture requires explicit captured or synthetic provenance")
	}
	switch d.Probe {
	case "", "info", "version", "inspection":
	default:
		return errors.New("unknown fixture probe")
	}
	commands := 1
	if d.Probe == "inspection" {
		commands = inspectionCommandCount
	}
	if len(d.Files) > maxFiles || len(d.Commands) != commands {
		return errors.New("fixture file or command count exceeds its probe contract")
	}
	if _, err := config.ParseRequirement(d.Requirement); err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, file := range d.Files {
		if platform.ValidateSubpath(file.Path) != nil || file.Path == "." || path.Clean(file.Path) != file.Path || seen[file.Path] {
			return errors.New("invalid or duplicate fixture file path")
		}
		seen[file.Path] = true
		if (file.UID == nil) != (file.GID == nil) {
			return errors.New("fixture ownership requires both UID and GID")
		}
		switch file.Kind {
		case "file", "directory", "symlink", "error":
		default:
			return errors.New("invalid fixture file kind")
		}
		if _, err := failure(file.Failure); err != nil {
			return err
		}
	}
	return nil
}

type Services struct {
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
	node, err := config.ParseRequirement(d.Requirement)
	if err != nil {
		return nil, err
	}
	files := platform.NewScopedMemReader("/", mem)
	env := platform.NewEnvironment(nil, nil, nil, runner).WithFiles(files).WithScope(d.Scope())
	services := &Services{Environment: env, Command: specs[0], Requirement: node, files: files}
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
		return mem.SetOwnership(name, platform.FileOwnership{UID: *file.UID, GID: *file.GID})
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
		StdoutTruncated: command.StdoutTruncated, StderrTruncated: command.StderrTruncated, TimedOut: command.Failure == "timeout"}
	if err := runner.RegisterWithError(spec, result, commandErr); err != nil {
		return platform.CommandSpec{}, err
	}
	return spec, nil
}
