package output

import (
	"errors"
	"path"
	"regexp"
	"strings"
	"unicode"
)

const maxRuntimePath = 4096

var runtimeName = regexp.MustCompile(`^[a-zA-Z0-9_.+-]{0,128}$`)

func (r RuntimeInfo) Validate() error {
	for _, value := range []*string{r.GraphRoot, r.RunRoot} {
		if value == nil || *value == "" {
			continue
		}
		if len(*value) > maxRuntimePath || !path.IsAbs(*value) || path.Clean(*value) != *value ||
			strings.IndexFunc(*value, unicode.IsControl) >= 0 {
			return errors.New("invalid runtime storage path")
		}
	}
	for _, value := range []*string{r.CgroupVersion, r.CgroupManager} {
		if value != nil && !runtimeName.MatchString(*value) {
			return errors.New("invalid runtime cgroup metadata")
		}
	}
	return nil
}
