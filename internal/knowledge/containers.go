// Package knowledge contains narrowly scoped rules pinned to primary sources.
// A source-selection rule never proves runtime operation or substitutes for
// current target measurements.
package knowledge

import "github.com/EpicBlackWolfZ/capagent/internal/model"

const (
	Containers493         = "podman-4.9.3"
	Containers584         = "podman-5.8.4"
	ContainersUnqualified = "unqualified"
	Containers493Source   = "https://github.com/containers/podman/blob/v4.9.3/vendor/github.com/containers/common/pkg/config/new.go"
	Containers584Source   = "https://github.com/containers/podman/blob/v5.8.4/vendor/go.podman.io/common/pkg/config/new.go"
)

// ContainersSourceProfile intentionally has no version-range inference. The
// qualified numeric upstream versions retain vendor suffixes as metadata;
// distribution patches to these rules remain a documented qualification limit.
func ContainersSourceProfile(version model.PodmanVersion) string {
	const legacyMajor, legacyMinor, legacyPatch = 4, 9, 3
	const currentMajor, currentMinor, currentPatch = 5, 8, 4
	if version.Major == legacyMajor && version.Minor == legacyMinor && version.Patch == legacyPatch && version.Canonical == "4.9.3" {
		return Containers493
	}
	if version.Major == currentMajor && version.Minor == currentMinor && version.Patch == currentPatch && version.Canonical == "5.8.4" {
		return Containers584
	}
	return ContainersUnqualified
}
