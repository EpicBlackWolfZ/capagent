package platform_test

import (
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"path/filepath"
)

// fixtureParents materializes parent directories omitted by old flat-map
// fixtures. Missing-parent conformance cases deliberately do not use it.
func fixtureParents(mem *platform.MemPlatformReader, path string) {
	snapshot := mem.Snapshot()
	for dir := filepath.Dir(path); dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		if _, exists := snapshot[dir]; !exists {
			mem.AddDir(dir, 0o755)
		}
	}
}
