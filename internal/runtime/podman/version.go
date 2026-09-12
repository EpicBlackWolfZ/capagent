package podman

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

const maxVersionBytes = 4096
const versionNumberBits = 32

// Anchoring the entire line prevents extracting a plausible version from an
// error or mixed output. Only a narrow printable grammar is public-safe.
const versionArchitecture = `(?:linux/)?(?:amd64|arm64|x86_64|aarch64|arm|386|ppc64le|s390x|riscv64)`
const versionHash = `(?:\(git [0-9a-fA-F]{6,40}\)|[0-9a-fA-F]{7,40})`

var versionPattern = regexp.MustCompile(`^podman version (0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)` +
	`(?:-([a-zA-Z0-9][a-zA-Z0-9._-]{0,127}))?(?:\+([a-zA-Z0-9][a-zA-Z0-9._-]{0,127}))?` +
	`(?: (` + versionHash + `(?: ` + versionArchitecture + `)?|` + versionArchitecture + `))?$`)

func ParseVersion(data []byte) (model.PodmanVersion, error) {
	if len(data) > maxVersionBytes {
		return model.PodmanVersion{}, errors.New("podman version exceeds size limit")
	}
	line := strings.TrimSpace(string(data))
	match := versionPattern.FindStringSubmatch(line)
	if match == nil {
		return model.PodmanVersion{}, errors.New("invalid Podman version line")
	}
	var components [3]uint32
	for i := range components {
		n, err := strconv.ParseUint(match[i+1], 10, versionNumberBits)
		if err != nil {
			return model.PodmanVersion{}, errors.New("invalid Podman version component")
		}
		components[i] = uint32(n)
	}
	const suffixIndex, buildIndex, trailingIndex = 4, 5, 6
	return model.PodmanVersion{Major: components[0], Minor: components[1], Patch: components[2],
		Canonical: strings.Join(match[1:suffixIndex], "."), Suffix: match[suffixIndex], Build: match[buildIndex],
		Trailing: match[trailingIndex], Raw: line}, nil
}
