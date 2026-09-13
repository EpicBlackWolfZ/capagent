package app

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"io"
	"strconv"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/host"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

const TargetWorkerArgument = platform.TargetWorkerArgument
const targetNamespaceCount = 7

type targetPayload struct {
	Options  Options               `json:"options"`
	Scope    model.EvaluationScope `json:"scope"`
	Launcher *model.UserIdentity   `json:"launcher"`
	Target   *model.UserIdentity   `json:"target"`
}

func selectLiveTarget(ctx context.Context, selector string, files platform.ScopedView,
	current *model.UserIdentity,
) (*model.UserIdentity, error) {
	passwd, err := files.ReadFile(ctx, "etc/passwd")
	if err != nil {
		return nil, err
	}
	groups, err := files.ReadFile(ctx, "etc/group")
	if err != nil {
		return nil, err
	}
	target, err := host.ResolveTarget(model.TargetSelector{Value: selector}, passwd, groups)
	// Explicit selectors denote a local deployment credential set. Only an exact
	// credential match can reuse the launching process without delegation.
	if err == nil && sameCredentials(current, target) {
		return copyIdentity(current), nil
	}
	return target, err
}

func sameCredentials(a, b *model.UserIdentity) bool {
	return a != nil && b != nil && (model.IdentityContext{Execution: a, Target: b}).IsValid() == nil
}

func evaluateTarget(ctx context.Context, opts Options, services currentServices, scope model.EvaluationScope,
	current model.EvaluationContext,
) (*output.Report, error) {
	payload := targetPayload{Options: opts, Scope: scope, Launcher: current.Identity.Current, Target: current.Identity.Target}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request := platform.TargetRequest{Version: 1, RunID: scope.RunID, ContextID: scope.ContextID,
		Target: *current.Identity.Target, Namespaces: current.Namespaces, Payload: data}
	result, err := services.target(ctx, request)
	if err != nil || result.ExitCode != 0 || result.StdoutTruncated || result.StderrTruncated {
		return nil, errors.New("target worker did not complete")
	}
	var report *output.Report
	if err := json.Unmarshal(result.Stdout, &report, json.RejectUnknownMembers(true)); err != nil || report == nil {
		return nil, errors.New("invalid target response")
	}
	if err := validateTargetResponsePresence(result.Stdout); err != nil {
		return nil, err
	}
	if err := report.Validate(); err != nil {
		return nil, err
	}
	trace := report.Evaluation
	if trace == nil || trace.Scope != output.ProjectScope(scope) || !sameOutputIdentity(trace.Current, current.Identity.Current) ||
		!sameOutputIdentity(trace.Target, current.Identity.Target) || !sameOutputIdentity(trace.Execution, current.Identity.Target) {
		return nil, errors.New("target response identity mismatch")
	}
	if trace.Selection != opts.Context || len(trace.Namespaces) != len(current.Namespaces) {
		return nil, errors.New("target namespace scope mismatch")
	}
	for _, expected := range current.Namespaces {
		found := false
		for _, actual := range trace.Namespaces {
			if actual.Kind == expected.Kind && actual.ID == expected.ID {
				found = true
			}
		}
		if !found {
			return nil, errors.New("target namespace scope mismatch")
		}
	}
	return report, nil
}

func validateTargetResponsePresence(data []byte) error {
	type identity struct {
		UID, GID    *uint32
		GroupsKnown *bool `json:"groups_known"`
	}
	var fields struct {
		Evaluation struct{ Current, Target, Execution *identity }
	}
	if err := json.Unmarshal(data, &fields, json.MatchCaseInsensitiveNames(true)); err != nil {
		return errors.New("invalid target response")
	}
	for _, identity := range []*identity{fields.Evaluation.Current, fields.Evaluation.Target, fields.Evaluation.Execution} {
		if identity == nil || identity.UID == nil || identity.GID == nil || identity.GroupsKnown == nil {
			return errors.New("target response requires explicit credentials")
		}
	}
	return nil
}

func sameOutputIdentity(actual *output.Identity, expected *model.UserIdentity) bool {
	if actual == nil {
		return false
	}
	return sameCredentials(&model.UserIdentity{UID: actual.UID, GID: actual.GID, GroupsKnown: actual.GroupsKnown,
		SupplementaryGroups: actual.SupplementaryGroups}, expected)
}

// ExecuteTargetWorker is a private bootstrap. Failure always terminates this process;
// no application collection runs until the platform verifies the dropped credentials.
func ExecuteTargetWorker(ctx context.Context, stdout, stderr io.Writer) int {
	ctx, cancel := platform.TargetContext(ctx)
	defer cancel()
	return executeTargetWorker(ctx, stdout, stderr, platform.ReceiveTarget)
}

func executeTargetWorker(ctx context.Context, stdout, stderr io.Writer,
	receive func(context.Context) (platform.TargetRequest, error),
) int {
	request, err := receive(ctx)
	if err != nil {
		return failure(stderr, ExitExecution, "target bootstrap failed")
	}
	var payload targetPayload
	if err := json.Unmarshal(request.Payload, &payload, json.RejectUnknownMembers(true)); err != nil || !validOptions(payload.Options) ||
		payload.Options.Fixture != "" || payload.Scope.RunID != request.RunID || payload.Scope.ContextID != request.ContextID ||
		payload.Launcher == nil || payload.Launcher.IsValid() != nil || !sameCredentials(payload.Target, &request.Target) {
		return failure(stderr, ExitExecution, "invalid target request")
	}
	files, err := platform.NewScopedOSReader("/")
	if err != nil {
		return failure(stderr, ExitExecution, "target filesystem unavailable")
	}
	credentials, groupErr := platform.CurrentCredentials()
	services := currentServices{files: files, credentials: credentials, groupErr: groupErr, now: time.Now, worker: &payload,
		manager: platform.NewUserManager(platform.NewOSCommandRunner(platform.UserManagerTimeout)),
		host:    platform.LinuxHostQueries{}, metadata: platform.NewHostMetadata(platform.NewOSCommandRunner(platform.HostVersionTimeout)),
		runner:  func() platform.CommandRunner { return platform.NewOSCommandRunner(podman.InfoTimeout) },
		capture: func() (platform.EnvPolicy, error) { return targetEnvironment(request.Target) }}
	report, evalErr := evaluateCurrentServices(ctx, payload.Options, services)
	err = errors.Join(evalErr, files.Close())
	if err != nil {
		return failure(stderr, ExitExecution, "target evaluation failed")
	}
	data, err := output.MarshalCompact(report)
	if err != nil || len(data) > platform.TargetMessageBytes {
		return failure(stderr, ExitExecution, "target response unavailable")
	}
	if _, err := stdout.Write(data); err != nil {
		return ExitExecution
	}
	return ExitSatisfied
}

func targetEnvironment(target model.UserIdentity) (platform.EnvPolicy, error) {
	if target.HomeDir == "" {
		return platform.EnvPolicy{}, errors.New("target home unavailable")
	}
	return platform.NewEnvPolicy(nil, map[string]string{"HOME": target.HomeDir,
		"XDG_RUNTIME_DIR": fmtRuntimeDir(target.UID)})
}

func fmtRuntimeDir(uid uint32) string { return "/run/user/" + strconv.FormatUint(uint64(uid), 10) }

func prepareLiveIdentity(ctx context.Context, opts Options, services currentServices, current model.EvaluationContext,
	identity model.Observation,
) (model.EvaluationContext, model.Observation, bool, error) {
	current.Identity.Selection = opts.Context
	if current.Identity.Selection == "" {
		current.Identity.Selection = "current"
	}
	identity.Identity.Selection = current.Identity.Selection
	if services.worker != nil {
		current.Identity.Current = copyIdentity(services.worker.Launcher)
		current.Identity.Target = copyIdentity(services.worker.Target)
		if current.Identity.IsValid() != nil {
			return current, identity, false, platform.ErrTargetCredentials
		}
	} else if opts.Context != "" && opts.Context != "current" {
		selected, selectErr := selectLiveTarget(ctx, opts.Context, services.files, current.Identity.Current)
		current.Identity.Target = selected
		if selectErr != nil || !sameCredentials(current.Identity.Execution, selected) {
			current.Identity.Execution = nil
			if selectErr == nil && services.credentials.IsValid() == nil && services.credentials.EUID == 0 &&
				services.credentials.GroupsKnown && services.target != nil && len(current.Namespaces) == targetNamespaceCount {
				return current, identity, true, nil
			}
			identity.Completeness = model.Partial
			identity.Identity.Resolved = selectErr == nil
			identity.Diagnostics = append(identity.Diagnostics, model.Diagnostic{Code: "target_unavailable",
				Message: "target resolution or credential authority is unavailable"})
		}
	}
	if current.Identity.Execution == nil {
		identity.Authority = "current"
	}
	return current, identity, false, nil
}
