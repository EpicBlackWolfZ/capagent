package model_test

import (
	json "encoding/json/v2"
	"reflect"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

const testRuntimePodman = "podman"

func TestEvaluationContext_ConstructionAndSeparation(t *testing.T) {
	t.Parallel()

	currentIdentity := model.UserIdentity{
		UID:      0,
		GID:      0,
		Username: "root",
		HomeDir:  "/root",
	}

	targetIdentity := model.UserIdentity{
		UID:      1000,
		GID:      1000,
		Username: "appuser",
		HomeDir:  "/home/appuser",
	}

	subUIDs := []model.SubIDRange{
		{Start: 100000, Length: 65536},
	}
	subGIDs := []model.SubIDRange{
		{Start: 100000, Length: 65536},
	}

	idCtx := model.IdentityContext{
		Current:        &currentIdentity,
		Target:         &targetIdentity,
		IsRootless:     boolPointer(true),
		SubUIDRanges:   subUIDs,
		SubGIDRanges:   subGIDs,
		XDGRuntimeDir:  "/run/user/1000",
		HasUserSystemd: boolPointer(true),
		InContainer:    boolPointer(false),
	}

	hostCtx := model.HostContext{
		OS:            "rhel",
		OSVersion:     "9.4",
		Kernel:        "5.14.0-427.el9.x86_64",
		Architecture:  "x86_64",
		CgroupVersion: "v2",
		SystemdActive: boolPointer(true),
	}

	runtimeCtx := model.RuntimeContext{
		ActiveRuntimes: []string{testRuntimePodman},
		DefaultRuntime: testRuntimePodman,
	}

	configCtx := model.ConfigContext{
		SearchPaths: []string{"/etc/containers", "/home/appuser/.config/containers"},
	}

	evalCtx := model.EvaluationContext{
		Host:          hostCtx,
		Identity:      idCtx,
		Runtime:       runtimeCtx,
		Configuration: configCtx,
	}

	// Verify identity separation
	if evalCtx.Identity.Current.UID != 0 || evalCtx.Identity.Current.Username != "root" {
		t.Errorf("expected Current identity UID=0, got %d", evalCtx.Identity.Current.UID)
	}
	if evalCtx.Identity.Target.UID != 1000 || evalCtx.Identity.Target.Username != "appuser" {
		t.Errorf("expected Target identity UID=1000, got %d", evalCtx.Identity.Target.UID)
	}
	if evalCtx.Identity.IsRootless == nil || !*evalCtx.Identity.IsRootless {
		t.Error("expected IsRootless to be true")
	}
	if len(evalCtx.Identity.SubUIDRanges) != 1 || evalCtx.Identity.SubUIDRanges[0].Start != 100000 {
		t.Errorf("unexpected SubUIDRanges: %+v", evalCtx.Identity.SubUIDRanges)
	}
	if evalCtx.Host.OS != "rhel" || evalCtx.Host.CgroupVersion != "v2" {
		t.Errorf("unexpected HostContext: %+v", evalCtx.Host)
	}
	if evalCtx.Runtime.DefaultRuntime != testRuntimePodman {
		t.Errorf("unexpected DefaultRuntime: %q", evalCtx.Runtime.DefaultRuntime)
	}
	if len(evalCtx.Configuration.SearchPaths) != 2 {
		t.Errorf("unexpected SearchPaths length: %d", len(evalCtx.Configuration.SearchPaths))
	}
}

func TestEvaluationContext_Serialization(t *testing.T) {
	t.Parallel()

	original := model.EvaluationContext{
		Host: model.HostContext{
			OS:            "rhel",
			OSVersion:     "9.4",
			Kernel:        "5.14.0-427.el9.x86_64",
			Architecture:  "x86_64",
			CgroupVersion: "v2",
			SystemdActive: boolPointer(true),
		},
		Identity: model.IdentityContext{
			Current: &model.UserIdentity{
				UID:      0,
				GID:      0,
				Username: "root",
				HomeDir:  "/root",
			},
			Target: &model.UserIdentity{
				UID:      1000,
				GID:      1000,
				Username: "appuser",
				HomeDir:  "/home/appuser",
			},
			IsRootless: boolPointer(true),
			SubUIDRanges: []model.SubIDRange{
				{Start: 100000, Length: 65536},
			},
			SubGIDRanges: []model.SubIDRange{
				{Start: 100000, Length: 65536},
			},
			XDGRuntimeDir:  "/run/user/1000",
			HasUserSystemd: boolPointer(true),
			InContainer:    boolPointer(false),
		},
		Runtime: model.RuntimeContext{
			ActiveRuntimes: []string{testRuntimePodman},
			DefaultRuntime: testRuntimePodman,
		},
		Configuration: model.ConfigContext{
			SearchPaths: []string{"/etc/containers", "/home/appuser/.config/containers"},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var decoded model.EvaluationContext
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if !reflect.DeepEqual(original, decoded) {
		t.Errorf("JSON round-trip mismatch:\ngot  %+v\nwant %+v", decoded, original)
	}

	// Verify empty context round-trip
	var empty model.EvaluationContext
	emptyData, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("empty json.Marshal failed: %v", err)
	}

	var emptyDecoded model.EvaluationContext
	if err := json.Unmarshal(emptyData, &emptyDecoded); err != nil {
		t.Fatalf("empty json.Unmarshal failed: %v", err)
	}

	if !reflect.DeepEqual(empty, emptyDecoded) {
		t.Errorf("empty JSON round-trip mismatch:\ngot  %+v\nwant %+v", emptyDecoded, empty)
	}
}

func boolPointer(value bool) *bool { return &value }
