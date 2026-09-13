package contract_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/fixture"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

// Both paths use the same fixture files, command snapshot and real Podman
// parser. The platform's larger conformance matrix remains authoritative for
// descriptor, cancellation, symlink and fault behavior; this is its application
// integration check, not another filesystem implementation.
func TestFixtureOSMemoryIntegration(t *testing.T) {
	t.Parallel()
	for _, name := range []string{fixtureSupported, "unsupported", "misconfigured", "unavailable", testUnknown} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join(findRepoRoot(t), fixtureRelativeRoot, name, "fixture.json"))
			if err != nil {
				t.Fatal(err)
			}
			doc, err := fixture.Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			services, err := fixture.Open(doc)
			if err != nil {
				t.Fatal(err)
			}
			defer services.Close()
			root := t.TempDir()
			for _, file := range doc.Files {
				dest := filepath.Join(root, file.Path)
				switch file.Kind {
				case "directory":
					err = os.Mkdir(dest, os.FileMode(file.Mode))
				case "file":
					err = os.WriteFile(dest, []byte(file.Content), os.FileMode(file.Mode))
				default:
					t.Fatalf("unsupported OS integration entry: %s", file.Kind)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			reader, err := platform.NewScopedOSReader(root)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			osEnv := platform.NewEnvironment(nil, nil, nil, services.Environment.Runner()).WithFiles(reader).WithScope(doc.Scope())
			probe := podman.InfoProbe{Command: services.Command, Timestamp: doc.Timestamp, LegacyInfo: true}
			memoryObservation, memoryError := probe.Run(t.Context(), services.Environment)
			osObservation, osError := probe.Run(t.Context(), osEnv)
			if (memoryError == nil) != (osError == nil) || !reflect.DeepEqual(memoryObservation, osObservation) {
				t.Fatalf("fixture OS/memory mismatch: memory=%+v (%v), OS=%+v (%v)", memoryObservation, memoryError, osObservation, osError)
			}
		})
	}
}
