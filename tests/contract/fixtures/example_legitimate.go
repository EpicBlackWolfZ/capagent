// example_legitimate.go uses only the platform package abstractions
// to read the same kind of file. The AST denylist MUST classify this
// code as compliant (no host-IO selector outside the platform layer
// is invoked directly).
package fixtures

import (
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

// readSecretLegitimately routes through the platform abstraction so
// no host-IO primitive appears in the AST. The denylist permits this.
func readSecretLegitimately(env platform.Environment) ([]byte, error) {
	return env.Reader().ReadFile("/etc/passwd")
}
