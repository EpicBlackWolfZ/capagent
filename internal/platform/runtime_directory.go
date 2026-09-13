package platform

import (
	"context"
	"errors"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"io/fs"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

const privateRuntimeMode = 0o700
const maxRuntimePath = 4096

var ErrRuntimeDirectory = errors.New("target runtime directory must be owned by target UID with mode 0700")

func ValidRuntimePath(name string) bool {
	return utf8.ValidString(name) && len(name) > 1 && len(name) <= maxRuntimePath && path.IsAbs(name) && path.Clean(name) == name &&
		!strings.ContainsFunc(name, unicode.IsControl) && ValidateSubpath(name[1:]) == nil
}

// InspectRuntimeDirectory is the shared metadata policy for user services and runtime commands.
func InspectRuntimeDirectory(ctx context.Context, files ScopedView, name string, uid uint32) (model.RuntimeDirectoryObservation, error) {
	out := model.RuntimeDirectoryObservation{Path: name}
	if !ValidRuntimePath(name) {
		out.Path = ""
		out.Valid = directoryValue(false)
		return out, ErrRuntimeDirectory
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if files == nil {
		return out, ErrIncomplete
	}
	info, err := files.Stat(name[1:])
	if errors.Is(err, fs.ErrNotExist) {
		out.Present = directoryValue(false)
		out.Valid = directoryValue(false)
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.Present = directoryValue(true)
	out.Directory = directoryValue(info.IsDir())
	mode := uint32(info.Mode().Perm())
	out.Mode = &mode
	private := info.Mode()&(fs.ModePerm|fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) == privateRuntimeMode
	out.Private = &private
	owner, known := OwnershipOf(info)
	if !known {
		return out, ErrIncomplete
	}
	out.UID = &owner.UID
	out.Valid = directoryValue(info.IsDir() && private && owner.UID == uid)
	return out, nil
}
func ValidateRuntimeDirectory(ctx context.Context, files ScopedView, name string, uid uint32) error {
	metadata, err := InspectRuntimeDirectory(ctx, files, name, uid)
	if err != nil {
		return err
	}
	if metadata.Valid == nil || !*metadata.Valid {
		return ErrRuntimeDirectory
	}
	return nil
}
func directoryValue(v bool) *bool { return &v }
