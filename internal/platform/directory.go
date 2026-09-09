package platform

import (
	"errors"
	"io/fs"
	"os"
	"sort"
	"syscall"
)

// captureDirectory captures metadata while lookup still owns its directory
// authority. Only an entry disappearing after enumeration may be skipped.
// Other failures discard the result; successful results are sorted snapshots.
func captureDirectory(names []string, lookup func(string) (os.FileInfo, error)) ([]os.DirEntry, error) {
	out := make([]os.DirEntry, 0, len(names))
	for _, name := range names {
		if name == "." || name == ".." {
			continue
		}
		info, err := lookup(name)
		if errors.Is(err, syscall.ENOENT) {
			continue
		}
		if err != nil {
			return nil, &os.PathError{Op: "readdir metadata", Path: name, Err: err}
		}
		out = append(out, fs.FileInfoToDirEntry(info))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}
