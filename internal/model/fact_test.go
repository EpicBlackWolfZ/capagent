package model_test

import (
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

func TestFact_Creation(t *testing.T) {
	t.Parallel()

	now := time.Now()
	fact := model.Fact{
		Source:    "/proc/sys/kernel/osrelease",
		RawData:   []byte("6.1.0-28-amd64\n"),
		Text:      "6.1.0-28-amd64",
		Timestamp: now,
	}

	if fact.Source != "/proc/sys/kernel/osrelease" {
		t.Errorf("Fact.Source = %q, want %q", fact.Source, "/proc/sys/kernel/osrelease")
	}
	if string(fact.RawData) != "6.1.0-28-amd64\n" {
		t.Errorf("Fact.RawData = %q, want %q", string(fact.RawData), "6.1.0-28-amd64\n")
	}
	if fact.Text != "6.1.0-28-amd64" {
		t.Errorf("Fact.Text = %q, want %q", fact.Text, "6.1.0-28-amd64")
	}
	if !fact.Timestamp.Equal(now) {
		t.Errorf("Fact.Timestamp = %v, want %v", fact.Timestamp, now)
	}
}

func TestObservation_Creation(t *testing.T) {
	t.Parallel()

	now := time.Now()
	obs := model.Observation{
		ID:        "obs-cgroup-v2",
		ProbeID:   "host.cgroup",
		Timestamp: now,
		Summary:   "unified cgroup v2 hierarchy detected at /sys/fs/cgroup",
	}

	if obs.ID != "obs-cgroup-v2" {
		t.Errorf("Observation.ID = %q, want %q", obs.ID, "obs-cgroup-v2")
	}
	if obs.ProbeID != "host.cgroup" {
		t.Errorf("Observation.ProbeID = %q, want %q", obs.ProbeID, "host.cgroup")
	}
	if obs.Summary != "unified cgroup v2 hierarchy detected at /sys/fs/cgroup" {
		t.Errorf("Observation.Summary = %q, want %q", obs.Summary, "unified cgroup v2 hierarchy detected at /sys/fs/cgroup")
	}

	ref := model.ObservationRef{
		ID:      obs.ID,
		ProbeID: obs.ProbeID,
	}
	if ref.ID != obs.ID || ref.ProbeID != obs.ProbeID {
		t.Errorf("ObservationRef mismatch: got %+v, want ID=%q ProbeID=%q", ref, obs.ID, obs.ProbeID)
	}
}
