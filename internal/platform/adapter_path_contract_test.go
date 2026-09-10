package platform_test

import (
	"context"
	"errors"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

type adapterPathRecorder struct {
	platform.ScopedReader
	paths []string
}

func (r *adapterPathRecorder) ReadFile(ctx context.Context, path string) ([]byte, error) {
	r.paths = append(r.paths, path)
	return nil, nil
}

func TestAdapterPathContract(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want error
	}{
		{"", platform.ErrEmptySubpath}, {"/", platform.ErrAbsoluteSubpath},
		{"/value", platform.ErrAbsoluteSubpath}, {"a\x00b", platform.ErrPathContainsNUL},
		{"../value", platform.ErrSubpathEscape}, {".", nil},
		{"link/../value", nil}, {"dir//value/", nil}, {`a\b`, nil},
	}
	for _, adapter := range []string{"proc", "sys", "self"} {
		t.Run(adapter, func(t *testing.T) {
			t.Parallel()
			for _, tt := range tests {
				t.Run(tt.path, func(t *testing.T) {
					t.Parallel()
					r := &adapterPathRecorder{ScopedReader: platform.NewScopedMemReader("/fixture", nil)}
					p := platform.NewProcfsReader(r)
					s := platform.NewSysfsReader(r)
					read := p.ReadProcFile
					prefix := ""
					if adapter == "sys" {
						read = s.ReadSysFile
					}
					if adapter == "self" {
						read = p.ReadSelf
						prefix = "self/"
					}
					_, err := read(t.Context(), tt.path)
					if !errors.Is(err, tt.want) {
						t.Fatalf("error = %v, want %v", err, tt.want)
					}
					if tt.want != nil {
						if len(r.paths) != 0 {
							t.Fatalf("invalid input forwarded: %v", r.paths)
						}
					} else if len(r.paths) != 1 || r.paths[0] != prefix+tt.path {
						t.Fatalf("forwarded %v, want %q", r.paths, prefix+tt.path)
					}
				})
			}
		})
	}
}
