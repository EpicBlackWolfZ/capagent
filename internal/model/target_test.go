package model_test

import (
	"math"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

func TestTargetValidation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		selector string
		valid    bool
	}{
		{"", true}, {"current", true}, {"user:root", true}, {"uid:0", true}, {"uid:1000", true},
		{"uid:4294967295", false}, {"uid:-1", false}, {"uid:+1", false}, {"uid:", false},
		{"user:", false}, {"user:../root", false}, {"user:a:b", false}, {"unprefixed", false},
		{"user:..", false}, {"user:" + strings.Repeat("a", 257), false}, {"uid:999999999999999999999999999", false},
	} {
		t.Run(test.selector, func(t *testing.T) {
			t.Parallel()
			if err := (model.TargetSelector{Value: test.selector}).IsValid(); (err == nil) != test.valid {
				t.Fatal(err)
			}
		})
	}
	for _, test := range []struct {
		ranges model.SubIDRanges
		valid  bool
	}{
		{nil, true}, {model.SubIDRanges{{Start: 0, Length: 1}}, true},
		{model.SubIDRanges{{Start: math.MaxUint32 - 1, Length: 1}}, true},
		{model.SubIDRanges{{Start: math.MaxUint32, Length: 1}}, false},
		{model.SubIDRanges{{Start: 1, Length: 0}}, false},
		{model.SubIDRanges{{Start: 10, Length: 10}, {Start: 0, Length: 10}}, true},
		{model.SubIDRanges{{Start: 10, Length: 10}, {Start: 19, Length: 10}}, false},
	} {
		if err := test.ranges.IsValid(); (err == nil) != test.valid {
			t.Fatalf("%+v: %v", test, err)
		}
	}
}

func TestIdentityAgreement(t *testing.T) {
	t.Parallel()
	root := &model.UserIdentity{GroupsKnown: true}
	user := &model.UserIdentity{UID: 1000, GID: 1000, GroupsKnown: true}
	for _, test := range []struct {
		value model.IdentityContext
		valid bool
	}{
		{model.IdentityContext{}, true},
		{model.IdentityContext{Current: root, Target: user, Execution: user}, true},
		{model.IdentityContext{Current: root, Target: user, Execution: root}, false},
		{model.IdentityContext{Current: root, Execution: root}, false},
		{model.IdentityContext{Target: &model.UserIdentity{UID: math.MaxUint32}}, false},
		{model.IdentityContext{Target: &model.UserIdentity{SupplementaryGroups: []uint32{1}}}, false},
		{model.IdentityContext{Target: &model.UserIdentity{GroupsKnown: true, SupplementaryGroups: []uint32{1, 1}}}, false},
		{model.IdentityContext{SubGIDRanges: []model.SubIDRange{{}}}, false},
		{model.IdentityContext{Selection: "bad"}, false},
		{model.IdentityContext{Execution: &model.UserIdentity{GroupsKnown: true, SupplementaryGroups: []uint32{1}},
			Target: &model.UserIdentity{GroupsKnown: true, SupplementaryGroups: []uint32{2}}}, false},
	} {
		if err := test.value.IsValid(); (err == nil) != test.valid {
			t.Fatalf("%+v: %v", test, err)
		}
	}
}
