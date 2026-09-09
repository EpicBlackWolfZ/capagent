package platform

import (
	"errors"
	"path/filepath"
	"strings"
)

// Sentinel errors returned by ValidateSubpath.
//
// Each sentinel is returned in exactly one rejection case. The error messages
// are stable contracts: callers may use them in log lines or human-readable
// diagnostics.
var (
	// ErrEmptySubpath is returned when subpath is the empty string.
	ErrEmptySubpath = errors.New("platform: empty subpath")

	// ErrPathContainsNUL is returned when subpath contains a NUL byte.
	// Linux syscalls treat NUL as a string terminator, so a path containing
	// NUL would silently truncate to whatever bytes precede it.
	ErrPathContainsNUL = errors.New("platform: path contains NUL byte")

	// ErrAbsoluteSubpath is returned when subpath begins with a path
	// separator. Absolute subpaths are rejected at the lexical layer so
	// the kernel never re-interprets them relative to the scoped root.
	ErrAbsoluteSubpath = errors.New("platform: absolute subpath not permitted")

	// ErrSubpathEscape is returned when filepath.Clean(subpath) equals
	// ".." or begins with "../". Such paths would traverse above the
	// scoped root once joined; the kernel's RESOLVE_IN_ROOT blocks the
	// traversal but the lexical check rejects the path eagerly so the
	// intent is unambiguous.
	ErrSubpathEscape = errors.New("platform: subpath would escape root")
)

// ValidateSubpath returns nil iff subpath is valid for use as the relative
// path argument of any ScopedReader method.
//
// On success, subpath is suitable for verbatim use as the path argument to
// openat2(rootfd, subpath, ..., RESOLVE_IN_ROOT). The function is a pure
// predicate; it does not transform or normalize the caller's string.
//
// Rejection rules (any one returns the corresponding sentinel):
//
//	subpath == ""                             -> ErrEmptySubpath
//	strings.IndexByte(subpath, 0) >= 0        -> ErrPathContainsNUL
//	filepath.IsAbs(subpath)                  -> ErrAbsoluteSubpath
//	filepath.Clean(subpath) == ".." ||
//	  strings.HasPrefix(filepath.Clean(subpath), "../") -> ErrSubpathEscape
//
// The function does not require knowledge of the scoped root; the rules
// above are sufficient to keep the joined path inside any non-empty root.
// The kernel's RESOLVE_IN_ROOT is a second layer of containment; the lexical
// check exists to make call-site intent unambiguous (the kernel never sees
// absolute-looking subpaths) and to fail fast on malformed input.
//
// The check order is stable: empty is checked first, then NUL, then absolute,
// then escape. Tests and the documentation table mirror this order.
func ValidateSubpath(subpath string) error {
	if subpath == "" {
		return ErrEmptySubpath
	}
	if strings.IndexByte(subpath, 0) >= 0 {
		return ErrPathContainsNUL
	}
	if filepath.IsAbs(subpath) {
		return ErrAbsoluteSubpath
	}
	cleaned := filepath.Clean(subpath)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return ErrSubpathEscape
	}
	return nil
}