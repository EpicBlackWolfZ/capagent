package host

import (
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

// ResolveTarget uses only local passwd/group records. It does not emulate NSS or a PAM login.
// The primary group is included in the selected set, matching initgroups conventions.
func ResolveTarget(selector model.TargetSelector, passwd, groups []byte) (*model.UserIdentity, error) {
	if err := selector.IsValid(); err != nil {
		return nil, err
	}
	kind, value, _ := strings.Cut(selector.Value, ":")
	if kind == "" {
		return nil, errors.New("target resolution requires an explicit selector")
	}
	if err := validAccountInput(passwd); err != nil {
		return nil, err
	}
	if err := validAccountInput(groups); err != nil {
		return nil, err
	}
	seenUID, seenName := map[uint32]bool{}, map[string]bool{}
	var result *model.UserIdentity
	for line := range strings.SplitSeq(string(passwd), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.SplitN(line, ":", accountFields+1)
		if len(fields) != accountFields {
			return nil, errors.New("malformed local account file")
		}
		uid, uidErr := accountID(fields[accountUIDField])
		const gidField = 3
		gid, gidErr := accountID(fields[gidField])
		if uidErr != nil || gidErr != nil || !accountNamePattern.MatchString(fields[0]) {
			return nil, errors.New("invalid account record")
		}
		if seenUID[uid] || seenName[fields[0]] {
			return nil, errors.New("ambiguous local account identity")
		}
		seenUID[uid], seenName[fields[0]] = true, true
		matches := kind == "user" && fields[0] == value || kind == "uid" && strconv.FormatUint(uint64(uid), 10) == canonicalUID(value)
		if !matches {
			continue
		}
		result = &model.UserIdentity{UID: uid, GID: gid, Username: fields[0]}
		if strings.HasPrefix(fields[accountHomeField], "/") && validHostText(fields[accountHomeField]) {
			result.HomeDir = fields[accountHomeField]
		}
	}
	if result == nil {
		return nil, errors.New("target account unavailable")
	}
	selected, err := localGroups(result, groups)
	if err != nil {
		return nil, err
	}
	result.SupplementaryGroups, result.GroupsKnown = selected, true
	return result, result.IsValid()
}

func canonicalUID(value string) string {
	value = strings.TrimLeft(value, "0")
	if value == "" {
		return "0"
	}
	return value
}

func accountID(raw string) (uint32, error) {
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0, errors.New("invalid account ID")
		}
	}
	n, err := strconv.ParseUint(raw, 10, identityNumberBits)
	if err != nil || n == math.MaxUint32 {
		return 0, errors.New("invalid account ID")
	}
	return uint32(n), nil
}

func localGroups(user *model.UserIdentity, data []byte) ([]uint32, error) {
	seen := map[uint32]bool{user.GID: true}
	const groupFields, groupMembers = 4, 3
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.SplitN(line, ":", groupFields+1)
		if len(f) != groupFields {
			return nil, errors.New("malformed local group file")
		}
		gid, err := accountID(f[accountUIDField])
		if err != nil {
			return nil, err
		}
		if slices.Contains(strings.Split(f[groupMembers], ","), user.Username) {
			seen[gid] = true
		}
		if len(seen) > model.MaxIdentityGroups {
			return nil, errors.New("too many target groups")
		}
	}
	result := make([]uint32, 0, len(seen))
	for gid := range seen {
		result = append(result, gid)
	}
	slices.Sort(result)
	return result, nil
}

func validAccountInput(data []byte) error {
	const maxBytes, maxLine, maxRecords = 8 << 20, 64 << 10, 65536
	if len(data) > maxBytes {
		return errors.New("account input exceeds byte budget")
	}
	records := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		records++
		if len(line) > maxLine || records > maxRecords || strings.ContainsRune(line, 0) {
			return errors.New("account input exceeds record budget or contains NUL")
		}
	}
	return nil
}
