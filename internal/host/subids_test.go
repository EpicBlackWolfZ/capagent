package host_test

import (
	"github.com/EpicBlackWolfZ/capagent/internal/host"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"reflect"
	"strings"
	"testing"
)

func TestSubIDParsing(t *testing.T) {
	t.Parallel()
	target := model.UserIdentity{UID: 1000, Username: "alice"}
	for _, tc := range []struct {
		name, data string
		total      uint64
		valid      bool
		ranges     int
	}{
		{"empty", "", 0, true, 0},
		{"multiple small", "alice:100000:12\n1000:200000:20\n", 32, true, 2},
		{"adjacent", "alice:10:2\nalice:12:3", 5, true, 2},
		{"overlap", "alice:10:3\n1000:12:3", 0, false, 2},
		{"other owner overlap", "bob:10:100\nalice:12:3", 0, false, 1},
		{"duplicate", "alice:10:3\nalice:10:3", 0, false, 2},
		{"overflow", "alice:4294967294:2", 0, false, 1},
		{"zero", "alice:10:0", 0, false, 1},
		{"malformed", "alice:xx:2", 0, false, 0},
		{"other malformed", "bad", 0, false, 0},
		{"numeric leading zeros", "01000:20:5", 5, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			before := []byte(tc.data)
			got, err := host.ParseSubIDs(before, target)
			if (err == nil) != tc.valid || len(got.Ranges) != tc.ranges {
				t.Fatalf("%+v %v", got, err)
			}
			if tc.valid && (got.Total == nil || *got.Total != tc.total) {
				t.Fatalf("bad total %+v", got)
			}
			if !tc.valid && got.Total != nil {
				t.Fatal("invalid totals accepted")
			}
			if !reflect.DeepEqual(before, []byte(tc.data)) {
				t.Fatal("input mutated")
			}
		})
	}
}

func TestSubIDParserBudgetsAndUnresolvedIDs(t *testing.T) {
	t.Parallel()
	for _, data := range []string{strings.Repeat("bob:10:1\n", 2049), strings.Repeat("a", (8<<20)+1), "alice:\x00:3"} {
		if out, err := host.ParseSubIDs([]byte(data), model.UserIdentity{UID: 1000, Username: "alice"}); err == nil || out.Total != nil {
			t.Fatal("invalid input accepted")
		}
	}
	if _, err := host.ParseSubIDs(nil, model.UserIdentity{UID: ^uint32(0)}); err == nil {
		t.Fatal("unresolved ID accepted")
	}
}
