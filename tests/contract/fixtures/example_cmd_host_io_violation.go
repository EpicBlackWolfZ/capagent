// example_cmd_host_io_violation.go deliberately uses a forbidden
// host primitive (os.ReadFile) inside the cmd/ tree. The CLI is
// NOT in the host-IO allowlist; the AST scanner MUST flag this
// file as a violation.
//
// This file is referenced by TestArchitecture_HostPrimitiveFixtures
// and is intentionally NOT exempt from the denylist.
package fixtures

import "os"

// readSecretInCLI is a deliberately bad helper that the AST
// denylist MUST detect as a violation in the cmd/ tree.
func readSecretInCLI() ([]byte, error) {
	//nolint:gosec // G304: this is a fixture demonstrating the rule.
	return os.ReadFile("/etc/passwd")
}