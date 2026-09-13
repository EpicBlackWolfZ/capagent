//go:build linux

package platform

import (
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// Test setup creates only a temporary FIFO. LookPath measures selection and does
// not execute or open the FIFO for reading/writing. Suitable-program metadata is
// deliberately a different question from Go's non-directory lookup contract.
func TestGoLookPathCanSelectAnExecutableFIFO(t *testing.T) {
	t.Parallel()
	name := filepath.Join(t.TempDir(), "helper")
	if err := unix.Mkfifo(name, 0o700); err != nil {
		t.Fatal(err)
	}
	selected, err := exec.LookPath(name)
	if err != nil || selected != name {
		t.Fatalf("lookup=%q error=%v", selected, err)
	}
}
