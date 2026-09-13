package platform

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"io"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

const TargetWorkerArgument = "--internal-target-worker"
const TargetWorkerTimeout = 45 * time.Second
const TargetMessageBytes = 1 << 20
const targetExecutableFD = 3

var ErrTargetCredentials = errors.New("target credentials could not be established")

func TargetContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx, syscall.SIGTERM, os.Interrupt)
}

// TargetRequest is an internal transport, never a general command or remote protocol.
// Payload belongs to the application and is interpreted only after the worker drops credentials.
type TargetRequest struct {
	Version    int                `json:"version"`
	RunID      string             `json:"run_id"`
	ContextID  string             `json:"context_id"`
	Target     model.UserIdentity `json:"target"`
	Namespaces []model.Namespace  `json:"namespaces"`
	Payload    []byte             `json:"payload"`
}

func (r TargetRequest) validate() error {
	if r.Version != 1 || r.RunID == "" || r.ContextID == "" || !r.Target.GroupsKnown || r.Target.IsValid() != nil ||
		len(r.Payload) == 0 || len(r.Payload) > TargetMessageBytes {
		return ErrTargetCredentials
	}
	seen := map[string]bool{}
	for _, ns := range r.Namespaces {
		if !slices.Contains([]string{"user", "pid", "net", "mnt", "ipc", "uts", "cgroup"}, ns.Kind) || seen[ns.Kind] ||
			!strings.HasPrefix(ns.ID, ns.Kind+":[") || !strings.HasSuffix(ns.ID, "]") {
			return ErrTargetCredentials
		}
		seen[ns.Kind] = true
	}
	if len(seen) == 0 {
		return ErrTargetCredentials
	}
	return nil
}

// RunTarget pins the running payload, including a deleted/cache/memfd payload.
// Only this executable FD crosses exec; scoped filesystem descriptors remain CLOEXEC.
// The trusted worker starts under the launching identity and drops all credentials
// before interpreting application options, opening target files, or running probes.
func RunTarget(ctx context.Context, request TargetRequest) (ExecResult, error) {
	if err := request.validate(); err != nil {
		return ExecResult{}, err
	}
	current, err := CurrentCredentials()
	if err != nil || current.IsValid() != nil || current.EUID != 0 {
		return ExecResult{}, ErrTargetCredentials
	}
	data, err := json.Marshal(request)
	if err != nil || len(data) > TargetMessageBytes {
		return ExecResult{}, ErrTargetCredentials
	}
	executable, err := os.Open("/proc/self/exe")
	if err != nil {
		return ExecResult{}, safeCommandError(err)
	}
	result, runErr := NewOSCommandRunner(TargetWorkerTimeout).run(ctx,
		CommandSpec{Path: "/proc/self/fd/3", Args: []string{TargetWorkerArgument}, Timeout: TargetWorkerTimeout},
		bytes.NewReader(data), []*os.File{executable})
	return result, errors.Join(runErr, executable.Close())
}

func decodeTarget(data []byte) (TargetRequest, error) {
	var request TargetRequest
	if len(data) > TargetMessageBytes {
		return request, ErrTargetCredentials
	}
	if err := json.Unmarshal(data, &request, json.RejectUnknownMembers(true)); err != nil {
		return request, ErrTargetCredentials
	}
	var presence struct {
		Target *struct {
			UID, GID    *uint32
			GroupsKnown *bool `json:"groups_known"`
		}
	}
	if err := json.Unmarshal(data, &presence, json.MatchCaseInsensitiveNames(true)); err != nil || presence.Target == nil ||
		presence.Target.UID == nil || presence.Target.GID == nil || presence.Target.GroupsKnown == nil {
		return request, ErrTargetCredentials
	}
	return request, request.validate()
}

// ReceiveTarget is only called from the private worker entrypoint, before app initialization.
// Global credential changes are restricted to this isolated process's bootstrap.
func ReceiveTarget(ctx context.Context) (TargetRequest, error) {
	return bootstrapTarget(ctx, os.Stdin, targetBootstrap{closeTargetExecutable, CurrentCredentials,
		syscall.Setgroups, syscall.Setresgid, syscall.Setresuid, verifyTarget})
}

type targetBootstrap struct {
	closeExecutable func() error
	credentials     func() (model.CurrentCredentials, error)
	setgroups       func([]int) error
	setresgid       func(int, int, int) error
	setresuid       func(int, int, int) error
	verify          func(context.Context, TargetRequest) error
}

func bootstrapTarget(ctx context.Context, input io.Reader, ops targetBootstrap) (TargetRequest, error) {
	var request TargetRequest
	if err := ops.closeExecutable(); err != nil {
		return request, err
	}
	data, err := io.ReadAll(io.LimitReader(input, TargetMessageBytes+1))
	if err != nil {
		return request, ErrTargetCredentials
	}
	request, err = decodeTarget(data)
	if err != nil {
		return request, err
	}
	current, err := ops.credentials()
	if err != nil || current.IsValid() != nil || current.EUID != 0 {
		return request, ErrTargetCredentials
	}
	groups := make([]int, len(request.Target.SupplementaryGroups))
	for i, gid := range request.Target.SupplementaryGroups {
		groups[i] = int(gid)
	}
	if err := ops.setgroups(groups); err != nil {
		return request, ErrTargetCredentials
	}
	gid, uid := int(request.Target.GID), int(request.Target.UID)
	if err := ops.setresgid(gid, gid, gid); err != nil {
		return request, ErrTargetCredentials
	}
	if err := ops.setresuid(uid, uid, uid); err != nil {
		return request, ErrTargetCredentials
	}
	return request, ops.verify(ctx, request)
}

func closeTargetExecutable() error {
	file := os.NewFile(targetExecutableFD, "target executable")
	if file == nil {
		return ErrTargetCredentials
	}
	info, err := file.Stat()
	self, selfErr := os.Stat("/proc/self/exe")
	closeErr := file.Close()
	if err != nil || selfErr != nil || closeErr != nil || !os.SameFile(info, self) {
		return ErrTargetCredentials
	}
	return nil
}

func verifyTarget(ctx context.Context, request TargetRequest) error {
	credentials, err := CurrentCredentials()
	actual := &model.UserIdentity{UID: credentials.EUID, GID: credentials.EGID,
		GroupsKnown: credentials.GroupsKnown, SupplementaryGroups: credentials.Groups}
	identity := model.IdentityContext{Target: &request.Target, Execution: actual}
	if err != nil || credentials.IsValid() != nil || identity.IsValid() != nil {
		return ErrTargetCredentials
	}
	files, err := NewScopedOSReader("/")
	if err != nil {
		return err
	}
	verifyErr := verifyTargetFiles(ctx, files, request)
	return errors.Join(verifyErr, files.Close())
}

func verifyTargetFiles(ctx context.Context, files ScopedView, request TargetRequest) error {
	for _, ns := range request.Namespaces {
		id, err := files.Readlink("proc/self/ns/" + ns.Kind)
		if err != nil || id != ns.ID {
			return ErrTargetCredentials
		}
	}
	data, err := NewHostProcfsReader(files).ReadSelf(ctx, "status")
	if err != nil {
		return ErrTargetCredentials
	}
	return verifyTargetStatus(data, request.Target)
}

func verifyTargetStatus(data []byte, target model.UserIdentity) error {
	seen := map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		key, value, _ := strings.Cut(line, ":")
		switch key {
		case "Uid", "Gid":
			fields := strings.Fields(value)
			const idFields = 4
			if seen[key] || len(fields) != idFields {
				return ErrTargetCredentials
			}
			want := target.UID
			if key == "Gid" {
				want = target.GID
			}
			for _, field := range fields {
				n, err := strconv.ParseUint(field, 10, 32)
				if err != nil || uint32(n) != want {
					return ErrTargetCredentials
				}
			}
			seen[key] = true
		case "CapInh", "CapPrm", "CapEff", "CapAmb":
			const hexBase, capBits = 16, 64
			n, err := strconv.ParseUint(strings.TrimSpace(value), hexBase, capBits)
			if seen[key] || err != nil || target.UID != 0 && n != 0 {
				return ErrTargetCredentials
			}
			seen[key] = true
		}
	}
	const requiredFields = 6
	if len(seen) != requiredFields {
		return ErrTargetCredentials
	}
	return nil
}
