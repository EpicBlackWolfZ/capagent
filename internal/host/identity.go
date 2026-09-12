// Package host constructs host observations through platform services.
package host

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

const identityProbeID = "host.identity"
const accountFields = 7
const accountUIDField, accountHomeField = 2, 5
const identityNumberBits = 32

var namespacePattern = regexp.MustCompile(`^(user|mnt|net):\[[0-9]+\]$`)
var accountNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,256}$`)

// ObserveCurrent bootstraps one immutable current=target context before runtime
// scheduling. Account and namespace lookup failures do not erase known IDs.
func ObserveCurrent(ctx context.Context, scope model.EvaluationScope, files platform.ScopedView,
	credentials model.CurrentCredentials, groupErr error, at time.Time,
) (model.EvaluationContext, model.Observation) {
	user := model.UserIdentity{UID: credentials.EUID, GID: credentials.EGID,
		SupplementaryGroups: slices.Clone(credentials.Groups), GroupsKnown: credentials.GroupsKnown}
	obs := model.Observation{ID: identityProbeID, ProbeID: identityProbeID, Scope: scope, Timestamp: at,
		Completeness: model.Complete, Summary: "Current execution identity and namespace snapshot"}
	addIdentityFact(&obs, "credentials", model.Complete)
	obs.Facts[0].Text = fmt.Sprintf("uid=%d euid=%d gid=%d egid=%d groups=%v groups_known=%t",
		credentials.UID, credentials.EUID, credentials.GID, credentials.EGID, credentials.Groups, credentials.GroupsKnown)
	if credentials.IsValid() != nil {
		identityDiagnostic(&obs, "credential_mismatch")
	}
	if groupErr != nil || !credentials.GroupsKnown {
		identityDiagnostic(&obs, "groups_unavailable")
	}
	if data, err := files.ReadFile(ctx, "etc/passwd"); err == nil {
		user.Username, user.HomeDir = accountMetadata(data, user.UID)
	}
	if user.Username == "" {
		identityDiagnostic(&obs, "account_metadata_unavailable")
	}
	addIdentityFact(&obs, "account", obs.Completeness)
	obs.Facts[len(obs.Facts)-1].Text = user.Username + ":" + user.HomeDir
	c := model.EvaluationContext{ID: scope.ContextID}
	for _, kind := range []string{"user", "mnt", "net"} {
		link, err := files.Readlink("proc/self/ns/" + kind)
		if err != nil || !namespacePattern.MatchString(link) || !strings.HasPrefix(link, kind+":") {
			identityDiagnostic(&obs, "namespace_unavailable")
			addIdentityFact(&obs, kind, model.Partial)
			continue
		}
		c.Namespaces = append(c.Namespaces, model.Namespace{Kind: kind, ID: link})
		addIdentityFact(&obs, kind, model.Complete)
		obs.Facts[len(obs.Facts)-1].Text = link
	}
	target := user
	target.SupplementaryGroups = slices.Clone(user.SupplementaryGroups)
	c.Identity = model.IdentityContext{Current: &user, Target: &target}
	return c, obs
}

func accountMetadata(data []byte, uid uint32) (string, string) {
	var username, home string
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) != accountFields {
			continue
		}
		id, err := strconv.ParseUint(fields[accountUIDField], 10, identityNumberBits)
		if err != nil || uint32(id) != uid {
			continue
		}
		if username != "" || !accountNamePattern.MatchString(fields[0]) {
			return "", ""
		}
		username = fields[0]
		if strings.HasPrefix(fields[accountHomeField], "/") {
			home = fields[accountHomeField]
		}
	}
	return username, home
}

func identityDiagnostic(obs *model.Observation, code string) {
	obs.Completeness = model.Partial
	obs.Diagnostics = append(obs.Diagnostics, model.Diagnostic{Code: code, Message: "current identity metadata is incomplete"})
}

func addIdentityFact(obs *model.Observation, name string, completeness model.Completeness) {
	obs.Facts = append(obs.Facts, model.Fact{ID: obs.ID + "." + name, Source: "current-process." + name,
		Scope: obs.Scope, Timestamp: obs.Timestamp, Completeness: completeness})
}
