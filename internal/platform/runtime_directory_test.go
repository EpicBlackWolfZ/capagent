package platform_test

import (
	"context"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"io/fs"
	"testing"
)

func TestPrivateRuntimeDirectoryPolicy(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"private", "public", "owner", "ownership unavailable", "missing", "permission",
		"regular node", "nil files", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			mem.AddDir("/runtime", 0o700)
			mem.SetOwnership("/runtime", platform.FileOwnership{UID: 1000})
			switch name {
			case "public":
				mem.AddDir("/runtime", 0o755)
				mem.SetOwnership("/runtime", platform.FileOwnership{UID: 1000})
			case "owner":
				mem.SetOwnership("/runtime", platform.FileOwnership{})
			case "ownership unavailable":
				mem.AddDir("/runtime", 0o700)
			case "missing":
				mem.AddError("/runtime", fs.ErrNotExist)
			case "permission":
				mem.AddError("/runtime", fs.ErrPermission)
			case "regular node":
				mem.AddFile("/runtime", nil, 0o700)
				mem.SetOwnership("/runtime", platform.FileOwnership{UID: 1000})
			}
			files := platform.NewScopedMemReader("/", mem)
			defer files.Close()
			var view platform.ScopedView = files
			if name == "nil files" {
				view = nil
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if name == "cancelled" {
				cancel()
			}
			err := platform.ValidateRuntimeDirectory(ctx, view, "/runtime", 1000)
			if (err == nil) != (name == "private") {
				t.Fatal(name, err)
			}
		})
	}
	for _, path := range []string{"", "/", "relative", "/a/../b", "/a\x00b", "/a\nb", "/a//b", "/bad\xff"} {
		if platform.ValidRuntimePath(path) {
			t.Fatal(path)
		}
		if _, err := platform.InspectRuntimeDirectory(t.Context(), nil, path, 0); err == nil {
			t.Fatal(path)
		}
	}
}
