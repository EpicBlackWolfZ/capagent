package config

import (
	"os"
	"path"
	"strconv"
	"strings"
)

// ExpandStoragePath matches storage's literal $UID replacement followed by Go
// variable expansion. os.Expand is pure here: the callback consults only the
// supplied immutable environment, never ambient process state. Scoped filesystem
// operations resolve symlinks later; this function makes no canonical-inode claim.
func ExpandStoragePath(value string, environment map[string]string, uid uint32) (string, error) {
	value = strings.ReplaceAll(value, "$UID", strconv.FormatUint(uint64(uid), 10))
	expanded := os.Expand(value, func(key string) string { return environment[key] })
	// Cleaning .. before following symlinks can select a different inode from
	// upstream EvalSymlinks. The confined reader rejects parent traversal, so
	// preserve uncertainty for this unsupported spelling instead.
	for _, segment := range strings.Split(expanded, "/") {
		if segment == ".." {
			return "", ErrConfigFieldUnsupported
		}
	}
	if !configPath(expanded) {
		return "", ErrConfigFieldUnsupported
	}
	return path.Clean(expanded), nil
}
