package host_test

import (
	"github.com/EpicBlackWolfZ/capagent/internal/host"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"strings"
	"testing"
)

const rootSelector = "uid:0"

func TestResolveTarget(t *testing.T) {
	t.Parallel()
	const aliceSelector = "user:alice"
	const passwd = "root:x:0:0:root:/root:/bin/sh\nalice:x:1000:100:Alice:/home/alice:/bin/sh\n"
	const groups = "users:x:100:\nextra:x:123:alice\n"
	for _, test := range []struct {
		name, selector, passwd, groups string
		uid                            uint32
		valid                          bool
	}{
		{"root", rootSelector, passwd, groups, 0, true},
		{"name", aliceSelector, passwd, groups, 1000, true},
		{"numeric", "uid:1000", passwd, groups, 1000, true},
		{"missing", "user:absent", passwd, groups, 0, false},
		{"duplicate", aliceSelector, passwd + passwd, groups, 0, false},
		{"bad gid", aliceSelector, "alice:x:1000:no::/home/alice:/bin/sh", groups, 0, false},
		{"bad groups", aliceSelector, passwd, "extra:x:bad:alice", 0, false},
		{"overflow", aliceSelector, passwd, "extra:x:4294967295:alice", 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			user, err := host.ResolveTarget(model.TargetSelector{Value: test.selector}, []byte(test.passwd), []byte(test.groups))
			if (err == nil) != test.valid {
				t.Fatal(err)
			}
			if test.valid && (user.UID != test.uid || !user.GroupsKnown) {
				t.Fatalf("%+v", user)
			}
			if test.valid && user.UID == 1000 && (len(user.SupplementaryGroups) != 2 || user.GID != 100) {
				t.Fatalf("%+v", user)
			}
		})
	}
}

func TestAccountInputBudgets(t *testing.T) {
	t.Parallel()
	for _, data := range []string{
		strings.Repeat("a", (8<<20)+1), strings.Repeat("a", (64<<10)+1),
		strings.Repeat("\n", 65537), "root:x:0:0::/root:\x00",
	} {
		if _, err := host.ResolveTarget(model.TargetSelector{Value: rootSelector}, []byte(data), nil); err == nil {
			t.Fatal("bad input accepted")
		}
	}
	for _, test := range []struct{ selector, passwd, groups string }{
		{"bad", "", ""}, {"current", "", ""}, {rootSelector, "root", ""},
		{rootSelector, "root:x:0:0::/root:/bin/sh\n", "bad"},
		{rootSelector, "root:x:0:0::/root:/bin/sh\n", "\x00"},
	} {
		if _, err := host.ResolveTarget(model.TargetSelector{Value: test.selector}, []byte(test.passwd), []byte(test.groups)); err == nil {
			t.Fatal(test.selector)
		}
	}
}
