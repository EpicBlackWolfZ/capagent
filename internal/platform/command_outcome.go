package platform

import (
	"errors"
	"io/fs"
	"os/exec"
)

// CommandCompleted identifies a fully collected successful or ordinary nonzero
// exit. Signal termination, startup, cancellation, truncation and drain failures
// are incomplete. A nil error with a nonzero status is supported for replay.
// Inspecting typed errors here keeps command implementation details in platform.
func CommandCompleted(result ExecResult, err error) bool {
	if result.TimedOut || result.StdoutTruncated || result.StderrTruncated || result.OutputIncomplete || result.ExitCode < 0 {
		return false
	}
	if err == nil {
		return true
	}
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ProcessState != nil && exitErr.Exited() &&
		result.ExitCode > 0 && result.ExitCode == exitErr.ExitCode()
}

// CommandFailedToStart requires typed startup evidence; an unclassified error
// with the default zero exit code cannot establish failure to start.
func CommandFailedToStart(err error) bool {
	var pathErr *fs.PathError
	return errors.As(err, &pathErr) && (pathErr.Op == "fork/exec" || pathErr.Op == "chdir")
}
