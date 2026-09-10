package platform

import (
	"errors"
	"fmt"
	"os"
)

var (
	ErrIncomplete    = errors.New("platform: incomplete measurement")
	ErrLimitExceeded = errors.New("platform: resource limit exceeded")
	ErrFileType      = errors.New("platform: file type not readable")
	ErrFileChanged   = errors.New("platform: file changed during open")
	ErrMalformed     = errors.New("platform: malformed input")
)

// LimitError reports an observed overflow, not an inferred total source size.
type LimitError struct {
	Resource string
	Limit    int
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("platform: %s limit %d exceeded", e.Resource, e.Limit)
}
func (e *LimitError) Is(target error) bool {
	return target == ErrLimitExceeded || target == ErrIncomplete
}

const (
	defaultFileBytes        = 8 * 1024 * 1024
	defaultDirectoryEntries = 8192
	defaultNameBytes        = 1024 * 1024
	capabilityBytes         = 64 * 1024
)

// ReadLimits bounds retained data and encountered directory names per operation.
// It is copied at construction. Values must be positive; no unlimited setting exists.
type ReadLimits struct{ FileBytes, DirectoryEntries, DirectoryNameBytes int }

func DefaultReadLimits() ReadLimits {
	return ReadLimits{FileBytes: defaultFileBytes, DirectoryEntries: defaultDirectoryEntries, DirectoryNameBytes: defaultNameBytes}
}
func (l ReadLimits) validate() error {
	// Leave space for sentinel arithmetic and allocation growth on every architecture.
	const maxLimit = int(^uint(0)>>1) / 2
	if l.FileBytes <= 0 || l.DirectoryEntries <= 0 || l.DirectoryNameBytes <= 0 ||
		l.FileBytes > maxLimit || l.DirectoryEntries > maxLimit || l.DirectoryNameBytes > maxLimit {
		return errors.New("platform: limits must be positive and below half MaxInt")
	}
	return nil
}
func incomplete(err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(ErrIncomplete, err)
}

func pathError(op, path string, err error) error {
	if err == nil {
		return nil
	}
	return &os.PathError{Op: op, Path: path, Err: err}
}
