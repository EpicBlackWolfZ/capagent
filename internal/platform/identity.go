package platform

import (
	"crypto/rand"
	"os"
	"slices"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

func NewRunID() string { return rand.Text() }

// CurrentCredentials reads the executing process, never passwd-derived group
// membership or another user's credentials. Its caller owns the snapshot.
func CurrentCredentials() (model.CurrentCredentials, error) {
	// Linux credential syscalls return uint32 IDs; supported targets use 64-bit int.
	c := model.CurrentCredentials{UID: uint32(os.Getuid()), EUID: uint32(os.Geteuid()), //nolint:gosec // Kernel ID widths are fixed.
		GID: uint32(os.Getgid()), EGID: uint32(os.Getegid())} //nolint:gosec // Kernel ID widths are fixed.
	groups, err := os.Getgroups()
	if err != nil {
		return c, err
	}
	c.GroupsKnown = true
	for _, group := range groups {
		c.Groups = append(c.Groups, uint32(group)) //nolint:gosec // Linux getgroups returns uint32 group IDs.
	}
	slices.Sort(c.Groups)
	c.Groups = slices.Compact(c.Groups)
	return c, nil
}
