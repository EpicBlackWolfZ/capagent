package model

import (
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"
)

const MaxIdentityGroups = 65536

// IdentityObservation describes resolution provenance, independently of credential authority.
type IdentityObservation struct {
	Selection     string `json:"selection"`
	AccountSource string `json:"account_source"`
	GroupsSource  string `json:"groups_source"`
	Resolved      bool   `json:"resolved"`
}

// TargetSelector preserves an explicit numeric request even when its account is unavailable.
type TargetSelector struct {
	Value string `json:"value"`
}

func (s TargetSelector) IsValid() error {
	if s.Value == "" || s.Value == "current" {
		return nil
	}
	kind, value, ok := strings.Cut(s.Value, ":")
	if !ok || value == "" {
		return errors.New("invalid target selector")
	}
	switch kind {
	case "uid":
		for _, c := range value {
			if c < '0' || c > '9' {
				return errors.New("invalid numeric target")
			}
		}
		id, err := strconv.ParseUint(value, 10, 32)
		if err != nil || id == math.MaxUint32 {
			return errors.New("invalid numeric target")
		}
	case "user":
		const maxName = 256
		if len(value) > maxName || value == "." || value == ".." {
			return errors.New("target name too long")
		}
		for _, c := range value {
			valid := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("_.-", c)
			if !valid {
				return errors.New("invalid target name")
			}
		}
	default:
		return errors.New("invalid target selector")
	}
	return nil
}

func (u UserIdentity) IsValid() error {
	if u.UID == math.MaxUint32 || u.GID == math.MaxUint32 || len(u.SupplementaryGroups) > MaxIdentityGroups {
		return errors.New("invalid identity IDs or group count")
	}
	if !u.GroupsKnown && len(u.SupplementaryGroups) != 0 {
		return errors.New("unobserved groups have values")
	}
	seen := make(map[uint32]bool, len(u.SupplementaryGroups))
	for _, gid := range u.SupplementaryGroups {
		if gid == math.MaxUint32 || seen[gid] {
			return errors.New("invalid or duplicate supplementary group")
		}
		seen[gid] = true
	}
	return nil
}

func (r SubIDRange) IsValid() error {
	if r.Length == 0 || uint64(r.Start)+uint64(r.Length) > math.MaxUint32 {
		return errors.New("invalid subordinate range")
	}
	return nil
}

// SubIDRanges validates a single allocation pool without sorting or modifying the caller's records.
type SubIDRanges []SubIDRange

func (ranges SubIDRanges) IsValid() error {
	ordered := slices.Clone(ranges)
	slices.SortFunc(ordered, func(a, b SubIDRange) int {
		if a.Start < b.Start {
			return -1
		}
		if a.Start > b.Start {
			return 1
		}
		return 0
	})
	var end uint64
	for _, r := range ordered {
		if err := r.IsValid(); err != nil {
			return err
		}
		if uint64(r.Start) < end {
			return errors.New("overlapping subordinate ranges")
		}
		end = uint64(r.Start) + uint64(r.Length)
	}
	return nil
}

func (c IdentityContext) IsValid() error {
	if err := (TargetSelector{Value: c.Selection}).IsValid(); err != nil {
		return err
	}
	for _, u := range []*UserIdentity{c.Current, c.Target, c.Execution} {
		if u != nil {
			if err := u.IsValid(); err != nil {
				return err
			}
		}
	}
	if e := c.Execution; e != nil {
		t := c.Target
		if t == nil || !e.GroupsKnown || !t.GroupsKnown || e.UID != t.UID || e.GID != t.GID ||
			len(e.SupplementaryGroups) != len(t.SupplementaryGroups) {
			return errors.New("execution does not match target")
		}
		for _, gid := range e.SupplementaryGroups {
			if !slices.Contains(t.SupplementaryGroups, gid) {
				return errors.New("execution groups differ from target")
			}
		}
	}
	return errors.Join(SubIDRanges(c.SubUIDRanges).IsValid(), SubIDRanges(c.SubGIDRanges).IsValid())
}
