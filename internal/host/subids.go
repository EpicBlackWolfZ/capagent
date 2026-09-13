package host

import (
	"cmp"
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

const subIDProviderUnknown = "unknown"

const maxSubIDRecords = 2048
const subIDFields = 3

// ParseSubIDs parses one independent pool. Source order and invalid range values survive;
// no allocation minimum or effective-provider claim is made here.
func ParseSubIDs(data []byte, target model.UserIdentity) (model.SubIDAllocation, error) {
	out := model.SubIDAllocation{Ranges: []model.SubIDRange{}, Records: []model.SubIDRecord{}}
	if target.IsValid() != nil || validAccountInput(data) != nil {
		return out, platform.ErrMalformed
	}
	type interval struct {
		start, end uint64
		record     int
	}
	var intervals []interval
	var problems error
	lineNumber := uint32(0)
	count := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		lineNumber++
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		count++
		if count > maxSubIDRecords {
			problems = errors.Join(problems, platform.ErrLimitExceeded)
			break
		}
		fields := strings.SplitN(line, ":", subIDFields+1)
		selected := fields[0] == target.Username && target.Username != ""
		if id, err := accountID(fields[0]); err == nil && id == target.UID {
			selected = true
		}
		record := model.SubIDRecord{Line: lineNumber}
		index := -1
		if selected {
			record.Owner = fields[0]
			index = len(out.Records)
		}
		var r model.SubIDRange
		var err error
		if len(fields) != subIDFields || !accountNamePattern.MatchString(fields[0]) {
			err = platform.ErrMalformed
		} else {
			r.Start, err = accountID(fields[1])
			if err == nil {
				r.Length, err = accountID(fields[2])
			}
			if err == nil {
				record.Range = &r
				if selected {
					out.Ranges = append(out.Ranges, r)
				}
				err = r.IsValid()
			}
		}
		if err != nil {
			problems = platform.ErrMalformed
			record.Problem = "invalid_record"
		} else {
			intervals = append(intervals, interval{uint64(r.Start), uint64(r.Start) + uint64(r.Length), index})
		}
		if selected {
			out.Records = append(out.Records, record)
		}
	}
	slices.SortFunc(intervals, func(a, b interval) int { return cmp.Compare(a.start, b.start) })
	var previousEnd uint64
	for i, row := range intervals {
		conflict := row.start < previousEnd || i+1 < len(intervals) && intervals[i+1].start < row.end
		if conflict && row.record >= 0 {
			out.Records[row.record].Problem = "allocation_overlap"
			problems = platform.ErrMalformed
		}
		previousEnd = max(previousEnd, row.end)
	}
	out.Valid = ptr(problems == nil)
	if problems == nil {
		var total uint64
		for _, r := range out.Ranges {
			total += uint64(r.Length)
		}
		out.Total = &total
	}
	return out, problems
}

type SubIDProbe struct {
	Target model.UserIdentity
	Now    func() time.Time
}

func (SubIDProbe) ID() string             { return "context.subids" }
func (SubIDProbe) Dependencies() []string { return nil }
func (p SubIDProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	obs := newHostObservation(p.ID(), env.Scope(), p.Now)
	obs.Host = nil
	obs.SubIDs = &model.SubIDObservation{Helpers: []model.MappingHelper{}}
	obs.SubIDs.UID = readSubIDs(ctx, env, &obs, "etc/subuid", p.Target)
	obs.SubIDs.GID = readSubIDs(ctx, env, &obs, "etc/subgid", p.Target)
	data, err := env.Files().ReadFile(ctx, "etc/nsswitch.conf")
	recordSource(&obs, "etc/nsswitch.conf", missingIsKnown(err))
	obs.SubIDs.Provider = subIDProvider(data, err)
	if obs.SubIDs.Provider != "files" {
		hostWarning(&obs, "subid_provider_unresolved")
		obs.Completeness = model.Partial
	}
	for _, name := range []string{"newuidmap", "newgidmap"} {
		obs.SubIDs.Helpers = append(obs.SubIDs.Helpers, observeMappingHelper(ctx, env, &obs, name))
	}
	return obs, ctx.Err()
}
func readSubIDs(ctx context.Context, env platform.Environment, obs *model.Observation, name string,
	target model.UserIdentity,
) model.SubIDAllocation {
	data, err := env.Files().ReadFile(ctx, name)
	recordSource(obs, name, missingIsKnown(err))
	if errors.Is(err, fs.ErrNotExist) {
		return model.SubIDAllocation{Present: ptr(false), Ranges: []model.SubIDRange{},
			Records: []model.SubIDRecord{}, Total: ptr(uint64(0)), Valid: ptr(true)}
	}
	out, parseErr := ParseSubIDs(data, target)
	if err == nil {
		out.Present = ptr(true)
	} else {
		out.Total = nil
		out.Valid = nil
	}
	finishSource(obs, parseErr)
	if target.Username == "" {
		out.Total = nil
		out.Valid = nil
		hostWarning(obs, "subid_account_unresolved")
		obs.Completeness = model.Partial
	}
	return out
}
func subIDProvider(data []byte, err error) string {
	if errors.Is(err, fs.ErrNotExist) {
		return "files"
	}
	if err != nil || validAccountInput(data) != nil {
		return subIDProviderUnknown
	}
	provider := "files"
	found := false
	for line := range strings.SplitSeq(string(data), "\n") {
		line, _, _ = strings.Cut(line, "#")
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) != "subid" {
			continue
		}
		if found {
			return subIDProviderUnknown
		}
		found = true
		fields := strings.Fields(value)
		if len(fields) != 1 {
			return subIDProviderUnknown
		}
		if fields[0] != "files" {
			provider = "external"
		}
	}
	return provider
}
