package knowledge

import (
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

func TestContainersSourceProfile(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		version model.PodmanVersion
		id      string
	}{
		{model.PodmanVersion{Major: 4, Minor: 9, Patch: 3, Canonical: "4.9.3"}, Containers493},
		{model.PodmanVersion{Major: 5, Minor: 8, Patch: 4, Canonical: "5.8.4", Suffix: "vendor1"}, Containers584},
		{model.PodmanVersion{Major: 5, Minor: 8, Patch: 4, Canonical: "9.0.0"}, ContainersUnqualified},
		{model.PodmanVersion{Major: 5, Minor: 9, Patch: 0, Canonical: "5.9.0"}, ContainersUnqualified},
	} {
		t.Run(test.version.Canonical, func(t *testing.T) {
			t.Parallel()
			profile := ContainersSourceProfile(test.version)
			if profile != test.id {
				t.Fatalf("profile=%s want=%s", profile, test.id)
			}
		})
	}
}
