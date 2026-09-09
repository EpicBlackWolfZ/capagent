package platform_test

import (
	"errors"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

// Reused literal names hoisted to constants for the goconst linter.
const (
	scopedReaderEtcPasswd = "/etc/passwd"
)

// TestValidateSubpath verifies the lexical rejection/acceptance rules
// documented in docs/security/m1.1-filesystem-hardening-plan.md §4.3.
func TestValidateSubpath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		subpath string
		wantErr error // nil if expected to be accepted
	}{
		// Empty string.
		{
			name:    "empty string rejected",
			subpath: "",
			wantErr: platform.ErrEmptySubpath,
		},

		// Relative reference forms that resolve to "self" or "below self".
		{name: "dot accepted", subpath: ".", wantErr: nil},
		{name: "dot-slash accepted", subpath: "./", wantErr: nil},
		{name: "relative file path accepted", subpath: "foo/bar", wantErr: nil},
		{name: "collapsed separators accepted", subpath: "foo//bar", wantErr: nil},
		{name: "trailing slash trimmed in clean", subpath: "foo/", wantErr: nil},
		{name: "explicit dot prefix accepted", subpath: "./foo", wantErr: nil},
		{name: "traversal that returns to root accepted", subpath: "foo/..", wantErr: nil},
		{name: "traversal that returns to non-root accepted", subpath: "foo/../bar", wantErr: nil},
		{name: "backslash is a regular character on linux", subpath: "foo\\bar", wantErr: nil},

		// Absolute paths (any form starting with '/').
		{name: "absolute root rejected", subpath: "/", wantErr: platform.ErrAbsoluteSubpath},
		{name: "absolute double-slash rejected", subpath: "//", wantErr: platform.ErrAbsoluteSubpath},
		{name: "absolute /etc/passwd rejected", subpath: scopedReaderEtcPasswd, wantErr: platform.ErrAbsoluteSubpath},

		// Escape attempts.
		{name: "parent reference rejected", subpath: "..", wantErr: platform.ErrSubpathEscape},
		{name: "relative escape rejected", subpath: "../etc", wantErr: platform.ErrSubpathEscape},
		{name: "deep relative escape rejected", subpath: "../../etc/passwd", wantErr: platform.ErrSubpathEscape},
		{name: "traversal reaching parent rejected", subpath: "foo/../..", wantErr: platform.ErrSubpathEscape},

		// Embedded NUL.
		{name: "embedded NUL rejected", subpath: "foo\x00bar", wantErr: platform.ErrPathContainsNUL},
		{name: "leading NUL rejected", subpath: "\x00foo", wantErr: platform.ErrPathContainsNUL},
		{name: "trailing NUL rejected", subpath: "foo\x00", wantErr: platform.ErrPathContainsNUL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := platform.ValidateSubpath(tt.subpath)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateSubpath(%q) error = %v, want nil", tt.subpath, err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateSubpath(%q) error = %v, want %v", tt.subpath, err, tt.wantErr)
			}
		})
	}
}

// TestValidateSubpath_SentinelsAreDistinct guards against accidental
// sentinel collisions. Each rejection branch must return exactly its own
// sentinel; otherwise error.Is dispatch breaks at higher layers.
func TestValidateSubpath_SentinelsAreDistinct(t *testing.T) {
	t.Parallel()

	sentinels := []error{
		platform.ErrEmptySubpath,
		platform.ErrPathContainsNUL,
		platform.ErrAbsoluteSubpath,
		platform.ErrSubpathEscape,
	}
	for i, a := range sentinels {
		for j, b := range sentinels {
			if i == j {
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("sentinel %v is errors.Is(%d, %d) of %v; sentinels must be distinct", a, i, j, b)
			}
		}
	}
}