package podman

import (
	"path"
	"regexp"
	"strings"
	"unicode"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

const maxInfoPath = 4096

var infoToken = regexp.MustCompile(`^[a-zA-Z0-9_.+-]{1,128}$`)

// normalizeInfo removes unsafe typed values; the original bounded command fact
// remains internal. Unrecognized but well-formed names remain available as data.
func normalizeInfo(p *model.PodmanInfo) bool {
	valid := true
	for _, field := range []**string{&p.NetworkBackend, &p.StorageDriver, &p.CgroupVersion, &p.CgroupManager} {
		if *field != nil && **field != "" && !infoToken.MatchString(**field) {
			*field, valid = nil, false
		}
	}
	for _, field := range []**string{&p.GraphRoot, &p.RunRoot} {
		if *field != nil && **field != "" && !safeInfoPath(**field) {
			*field, valid = nil, false
		}
	}
	if p.Version != "" {
		version, err := ParseVersion([]byte("podman version " + p.Version))
		if err != nil {
			p.Version, valid = "", false
		} else {
			p.Version = strings.TrimPrefix(version.Raw, "podman version ")
			p.VersionParts = &version
		}
	}
	return valid
}

func safeInfoPath(value string) bool {
	return len(value) <= maxInfoPath && path.IsAbs(value) && path.Clean(value) == value &&
		strings.IndexFunc(value, unicode.IsControl) < 0 && platform.ValidateSubpath(path.Clean(strings.TrimPrefix(value, "/"))) == nil
}
