package platform

import (
	"context"
	"errors"
	"io/fs"
	"os/exec"
	"testing"
	"time"
)

func TestCommandCompletionClassification(t *testing.T) {
	t.Parallel()
	exited, exitErr := NewOSCommandRunner(time.Second).Run(t.Context(), CommandSpec{Path: "/bin/false"})
	if exitErr == nil {
		t.Fatal("missing real exit error")
	}
	for _, tc := range []struct {
		name     string
		result   ExecResult
		err      error
		complete bool
	}{
		{"zero exit", ExecResult{}, nil, true},
		{"nonzero replay", ExecResult{ExitCode: 1}, nil, true},
		{"real nonzero", exited, exitErr, true},
		{"mismatched status", ExecResult{}, exitErr, false},
		{"signalled replay", ExecResult{ExitCode: -1}, nil, false},
		{"invalid exit error", ExecResult{ExitCode: 1}, &exec.ExitError{}, false},
		{"cancelled", ExecResult{}, context.Canceled, false},
		{"timeout", ExecResult{TimedOut: true}, nil, false},
		{"stdout cap", ExecResult{StdoutTruncated: true}, nil, false},
		{"stderr cap", ExecResult{StderrTruncated: true}, nil, false},
		{"drain", ExecResult{}, exec.ErrWaitDelay, false},
		{"masked drain", ExecResult{ExitCode: 1, OutputIncomplete: true}, exitErr, false},
		{"transport", ExecResult{ExitCode: 1}, errors.New("transport error"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if CommandCompleted(tc.result, tc.err) != tc.complete {
				t.Fatal("wrong command completion")
			}
		})
	}
}

func TestCommandStartupClassification(t *testing.T) {
	t.Parallel()
	_, startup := NewOSCommandRunner(time.Second).Run(t.Context(), CommandSpec{Path: "/capagent-missing-command"})
	for _, tc := range []struct {
		name  string
		err   error
		start bool
	}{
		{"real startup", startup, true},
		{"wrapped startup", errors.Join(startup), true},
		{"working directory", &fs.PathError{Op: "chdir", Path: "PRIVATE", Err: fs.ErrPermission}, true},
		{"unclassified", errors.New("PRIVATE"), false},
		{"unrelated path", &fs.PathError{Op: "read", Path: "PRIVATE", Err: fs.ErrPermission}, false},
		{"drain", exec.ErrWaitDelay, false}, {"no error", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if CommandFailedToStart(tc.err) != tc.start {
				t.Fatal("wrong startup classification")
			}
		})
	}
}
