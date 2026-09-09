// example_host_io_violation.go deliberately uses a forbidden host
// primitive (os.ReadFile) so that the AST-based denylist can flag it
// in a known-good fixture. It is NOT compiled into any production
// binary.
//
// This file is referenced by TestArchitecture_HostPrimitiveFixtures
// and is intentionally NOT exempt from the denylist (the fixtures/
// directory is excluded from the AST walk, so this file is read by
// the fixture-specific test only).
package fixtures

import "os"

// readSecret is a deliberately bad helper that the AST denylist
// MUST detect as a violation.
func readSecret() ([]byte, error) {
	//nolint:gosec // G304: this is a fixture demonstrating the rule.
	return os.ReadFile("/etc/passwd")
}