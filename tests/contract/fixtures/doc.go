// Package fixtures contains AST fixtures used by the host-IO denylist
// test suite. Each Go file here is deliberately NOT part of the
// production build; the denylist test scans this directory and asserts
// that:
//
//   - example_host_io_violation.go: uses os.ReadFile directly,
//     which the denylist MUST flag.
//   - example_legitimate.go: uses only the platform package, which
//     the allowlist MUST exempt.
//
// The fixtures exist solely to give the AST scanner a target it can
// reliably classify. They are excluded from the production binary
// because the fixtures/ directory is the AST test corpus; no
// production code imports this package.
package fixtures