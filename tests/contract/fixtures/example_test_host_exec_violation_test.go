// example_test_host_exec_violation_test.go deliberately imports
// os/exec from a _test.go file. The scanner MUST flag os/exec
// imports in BOTH production and test files outside the platform
// layer.
//
// The fixture uses _ = exec.Command to ensure the import is real
// AST content for the scanner to find, and the file is named
// _test.go so it is classified as a test file by the scan loop.
//
//nolint:unused
package fixtures

import (
	"os/exec"
	"testing"
)

// _ ensures exec is referenced so the file is not degenerate.
var _ = exec.Command

func TestExampleTestHostExecViolation(t *testing.T) {
	t.Log("this file exists solely for the AST scanner")
}