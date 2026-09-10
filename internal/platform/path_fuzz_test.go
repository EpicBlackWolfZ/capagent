package platform

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/tests/fuzzutil"
)

const fuzzFileName = "file"
const fuzzSymlinkNodes = 12
const fuzzFileContent = "inside-fuzz-root"
const fuzzOutsideContent = "outside-fuzz-root"

func FuzzSubpath(f *testing.F) {
	for _, seed := range []string{"", ".", "../x", "/abs", "a/../b", "a/../../b", "a\x00", "file/", "link/../file"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		fuzzutil.Default(fuzzutil.PathInput).Run(t, input, func() {
			path := string(input)
			a, b := ValidateSubpath(path), ValidateSubpath(path)
			if fmt.Sprint(a) != fmt.Sprint(b) {
				t.Fatal("path validation changed")
			}
			if a == nil {
				cleaned := filepath.Clean(path)
				if path == "" || strings.ContainsRune(path, 0) || filepath.IsAbs(path) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
					t.Fatal("unsafe lexical path accepted")
				}
			}
		})
	})
}

// Fixture creation uses fixed relative names. Mutated bytes only influence
// symlink contents and observed paths, never an unscoped write destination.
func FuzzScopedSymlinkGraph(f *testing.F) {
	for _, seed := range []string{"", "../outside", "/outside", "n0/../file", "./file", "file/", "n0", "../../file"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		fuzzutil.Default(fuzzutil.PathInput).Run(t, input, func() {
			parent := t.TempDir()
			root := filepath.Join(parent, "root")
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(parent, "outside"), []byte(fuzzOutsideContent), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, fuzzFileName), []byte(fuzzFileContent), 0o600); err != nil {
				t.Fatal(err)
			}
			mem := NewMemPlatformReader()
			mem.AddDir("/", 0o755)
			mem.AddDir("/root", 0o755)
			mem.AddFile("/outside", []byte(fuzzOutsideContent), 0o600)
			mem.AddFile("/root/file", []byte(fuzzFileContent), 0o600)
			for i := range fuzzSymlinkNodes {
				name := fmt.Sprintf("n%d", i)
				const targetBytes = 256
				target := string(input[:min(len(input), targetBytes)])
				if i > 0 {
					target = fmt.Sprintf("n%d", i-1)
				}
				// Empty/NUL symlink contents are invalid at creation; the original
				// input is still tested as an observed subpath below.
				if target == "" || strings.ContainsRune(target, 0) {
					target = "missing"
				}
				if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
					t.Fatal(err)
				}
				mem.AddSymlink("/root/"+name, target)
			}
			reader, err := NewScopedOSReader(root)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			memory := NewScopedMemReader("/root", mem)
			for _, scoped := range []ScopedReader{reader, memory} {
				for _, path := range []string{string(input), "n0", "n11", "n0/../file", fuzzFileName, "file/"} {
					a, ae := scoped.ReadFile(t.Context(), path)
					b, be := scoped.ReadFile(t.Context(), path)
					if !bytes.Equal(a, b) || fmt.Sprint(ae) != fmt.Sprint(be) {
						t.Fatal("immutable graph observation changed")
					}
					if bytes.Contains(a, []byte(fuzzOutsideContent)) {
						t.Fatal("root escape")
					}
					if ae == nil && string(a) != fuzzFileContent {
						t.Fatal("unexpected successful file observation")
					}
				}
			}
			// Absolute target semantics intentionally differ: the OS re-roots them,
			// while memory targets name the backing tree. Do not assert equivalence.
		})
	})
}
