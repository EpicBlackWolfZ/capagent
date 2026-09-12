package model

import "errors"

// CurrentCredentials is a kernel credential snapshot. UID zero is an observed
// root identity; GroupsKnown distinguishes failed lookup from an empty set.
type CurrentCredentials struct {
	UID, EUID, GID, EGID uint32
	Groups               []uint32
	GroupsKnown          bool
}

// IsValid checks the supported current-process execution contract. Privilege
// transitions require a separate execution policy and are not supported yet.
func (c CurrentCredentials) IsValid() error {
	if c.UID != c.EUID || c.GID != c.EGID {
		return errors.New("real and effective credentials differ")
	}
	if !c.GroupsKnown && len(c.Groups) != 0 {
		return errors.New("unobserved supplementary groups have values")
	}
	seen := make(map[uint32]bool)
	for _, group := range c.Groups {
		if seen[group] {
			return errors.New("duplicate supplementary group")
		}
		seen[group] = true
	}
	return nil
}
