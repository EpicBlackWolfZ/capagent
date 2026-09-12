package platform

import (
	"context"
	"errors"
)

// ReadDocument reads one bounded document below an explicitly selected root.
// Root selection is caller authority; untrusted subpaths retain ScopedReader's
// kernel-backed containment. No reader survives this call.
func ReadDocument(ctx context.Context, root, subpath string, maxBytes int) ([]byte, error) {
	limits := DefaultReadLimits()
	limits.FileBytes = maxBytes
	reader, err := NewScopedOSReaderWithLimits(root, limits)
	if err != nil {
		return nil, err
	}
	data, readErr := reader.ReadFile(ctx, subpath)
	closeErr := reader.Close()
	return data, errors.Join(readErr, closeErr)
}
