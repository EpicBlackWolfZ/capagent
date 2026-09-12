package config_test

import (
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/config"
)

func TestParseRequirement(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, input string
		valid       bool
	}{
		{"predicate", `{"capability":"runtime.podman.netavark"}`, true},
		{"empty all", `{"all":[]}`, true},
		{"any", `{"any":[{"not":{"capability":"context.user"}}]}`, true},
		{"null", `null`, false}, {"malformed", `{`, false}, {"unknown operator", `{"and":[]}`, false},
		{"multiple", `{"all":[],"any":[]}`, false}, {"duplicate", `{"all":[],"all":[]}`, false},
		{"trailing", `{"all":[]} {}`, false}, {"oversize", strings.Repeat(" ", config.MaxRequirementBytes+1), false},
		{"deep",
			strings.Repeat(`{"not":`, config.MaxRequirementDepth+1) + `{"all":[]}` + strings.Repeat("}", config.MaxRequirementDepth+1), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := config.ParseRequirement([]byte(tt.input))
			if (err == nil) != tt.valid {
				t.Fatalf("parse: %v", err)
			}
		})
	}
}
