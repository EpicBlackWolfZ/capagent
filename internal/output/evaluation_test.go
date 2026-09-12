package output_test

import (
	"bytes"
	json "encoding/json/v2"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
)

func TestProjectionExplicitRootAndUnknown(t *testing.T) {
	t.Parallel()
	ctx := model.EvaluationContext{Identity: model.IdentityContext{Current: &model.UserIdentity{UID: 1000}, Target: &model.UserIdentity{}}}
	r := output.NewReportFromModel(ctx, nil, nil)
	if r.Context.UID == nil || *r.Context.UID != 0 {
		t.Fatal("explicit root target replaced by current user")
	}
	empty := output.NewReportFromModel(model.EvaluationContext{}, nil, nil)
	data, err := output.MarshalCompact(empty)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range [][]byte{[]byte(`"uid":null`), []byte(`"is_rootless":null`), []byte(`"systemd":null`)} {
		if !bytes.Contains(data, want) {
			t.Fatalf("missing %s: %s", want, data)
		}
	}
}

func TestReportCanonicalKeys(t *testing.T) {
	t.Parallel()
	r := output.NewReport()
	r.Host.OS = "linux"
	r.Host.CgroupVersion = "unknown"
	r.Capabilities["typo.feature"] = output.CapabilityReport{State: "supported", Confidence: "derived", Evidence: []string{}}
	if r.Validate() == nil {
		t.Fatal("report accepted noncanonical key")
	}
}

func TestReportNumericBounds(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"-1", "4294967296", "1.5"} {
		if _, err := output.Unmarshal([]byte(`{"context":{"uid":` + value + `}}`)); err == nil {
			t.Fatal("invalid UID accepted", value)
		}
	}
	var r output.Report
	if err := json.Unmarshal([]byte(`{"context":{"uid":4294967295,"gid":0}}`), &r); err != nil {
		t.Fatal(err)
	}
	if r.Context.UID == nil || *r.Context.UID != ^uint32(0) || r.Context.GID == nil || *r.Context.GID != 0 {
		t.Fatal("lost numeric boundary")
	}
}

func testPointer[T any](value T) *T { return &value }
