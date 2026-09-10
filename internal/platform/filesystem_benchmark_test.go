package platform_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

func BenchmarkFilesystemRead(b *testing.B) {
	const bytes = 64 * 1024
	root := b.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte(strings.Repeat("x", bytes)), 0o600); err != nil {
		b.Fatal(err)
	}
	r, err := platform.NewScopedOSReader(root)
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close()
	b.ReportAllocs()
	b.SetBytes(bytes)
	b.ResetTimer()
	for b.Loop() {
		if _, err := r.ReadFile(b.Context(), "file"); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkFilesystemParser(b *testing.B) {
	const records = 512
	mem := platform.NewMemPlatformReader()
	mem.AddDir("/proc", 0o755)
	mem.AddDir("/proc/self", 0o755)
	mem.AddFile("/proc/self/mountinfo", []byte(strings.Repeat("1 0 0:1 / / rw - proc proc rw\n", records)), 0o600)
	p := platform.NewProcfsReader(platform.NewScopedMemReader("/proc", mem))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := p.Mounts(b.Context()); err != nil {
			b.Fatal(err)
		}
	}
}
