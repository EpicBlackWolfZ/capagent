package model_test

import (
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

func TestCurrentCredentialsValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value model.CurrentCredentials
		valid bool
	}{
		{"root is known", model.CurrentCredentials{GroupsKnown: true}, true},
		{"user", model.CurrentCredentials{UID: 1000, EUID: 1000, GID: 100, EGID: 100}, true},
		{"unknown groups", model.CurrentCredentials{}, true},
		{"mismatched uid", model.CurrentCredentials{UID: 1000}, false},
		{"mismatched gid", model.CurrentCredentials{GID: 1000}, false},
		{"groups without observation", model.CurrentCredentials{Groups: []uint32{1}}, false},
		{"duplicate groups", model.CurrentCredentials{GroupsKnown: true, Groups: []uint32{1, 1}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.value.IsValid(); (err == nil) != tt.valid {
				t.Fatalf("valid=%v error=%v", tt.valid, err)
			}
		})
	}
}
