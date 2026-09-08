package requirement_test

import (
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/requirement"
)

func TestRequirementState_ValidationAndString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		state   requirement.RequirementState
		wantStr string
		wantErr bool
	}{
		{
			name:    "valid satisfied",
			state:   requirement.RequirementSatisfied,
			wantStr: "SATISFIED",
			wantErr: false,
		},
		{
			name:    "valid unsatisfied",
			state:   requirement.RequirementUnsatisfied,
			wantStr: "UNSATISFIED",
			wantErr: false,
		},
		{
			name:    "valid indeterminate",
			state:   requirement.RequirementIndeterminate,
			wantStr: "INDETERMINATE",
			wantErr: false,
		},
		{
			name:    "invalid empty",
			state:   requirement.RequirementState(""),
			wantStr: "",
			wantErr: true,
		},
		{
			name:    "invalid lowercase",
			state:   requirement.RequirementState("satisfied"),
			wantStr: "satisfied",
			wantErr: true,
		},
		{
			name:    "invalid unknown string",
			state:   requirement.RequirementState("UNKNOWN"),
			wantStr: "UNKNOWN",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.state.IsValid()
			if (err != nil) != tt.wantErr {
				t.Fatalf("RequirementState.IsValid() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if got := tt.state.String(); got != tt.wantStr {
				t.Errorf("RequirementState.String() = %q, want %q", got, tt.wantStr)
			}
		})
	}
}

func TestLogic_And(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  requirement.RequirementState
		right requirement.RequirementState
		want  requirement.RequirementState
	}{
		// 1. SATISFIED AND SATISFIED = SATISFIED
		{
			name:  "SATISFIED and SATISFIED",
			left:  requirement.RequirementSatisfied,
			right: requirement.RequirementSatisfied,
			want:  requirement.RequirementSatisfied,
		},
		// 2. SATISFIED AND UNSATISFIED = UNSATISFIED
		{
			name:  "SATISFIED and UNSATISFIED",
			left:  requirement.RequirementSatisfied,
			right: requirement.RequirementUnsatisfied,
			want:  requirement.RequirementUnsatisfied,
		},
		// 3. SATISFIED AND INDETERMINATE = INDETERMINATE
		{
			name:  "SATISFIED and INDETERMINATE",
			left:  requirement.RequirementSatisfied,
			right: requirement.RequirementIndeterminate,
			want:  requirement.RequirementIndeterminate,
		},
		// 4. UNSATISFIED AND SATISFIED = UNSATISFIED
		{
			name:  "UNSATISFIED and SATISFIED",
			left:  requirement.RequirementUnsatisfied,
			right: requirement.RequirementSatisfied,
			want:  requirement.RequirementUnsatisfied,
		},
		// 5. UNSATISFIED AND UNSATISFIED = UNSATISFIED
		{
			name:  "UNSATISFIED and UNSATISFIED",
			left:  requirement.RequirementUnsatisfied,
			right: requirement.RequirementUnsatisfied,
			want:  requirement.RequirementUnsatisfied,
		},
		// 6. UNSATISFIED AND INDETERMINATE = UNSATISFIED
		{
			name:  "UNSATISFIED and INDETERMINATE",
			left:  requirement.RequirementUnsatisfied,
			right: requirement.RequirementIndeterminate,
			want:  requirement.RequirementUnsatisfied,
		},
		// 7. INDETERMINATE AND SATISFIED = INDETERMINATE
		{
			name:  "INDETERMINATE and SATISFIED",
			left:  requirement.RequirementIndeterminate,
			right: requirement.RequirementSatisfied,
			want:  requirement.RequirementIndeterminate,
		},
		// 8. INDETERMINATE AND UNSATISFIED = UNSATISFIED
		{
			name:  "INDETERMINATE and UNSATISFIED",
			left:  requirement.RequirementIndeterminate,
			right: requirement.RequirementUnsatisfied,
			want:  requirement.RequirementUnsatisfied,
		},
		// 9. INDETERMINATE AND INDETERMINATE = INDETERMINATE
		{
			name:  "INDETERMINATE and INDETERMINATE",
			left:  requirement.RequirementIndeterminate,
			right: requirement.RequirementIndeterminate,
			want:  requirement.RequirementIndeterminate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := requirement.And(tt.left, tt.right)
			if got != tt.want {
				t.Errorf("And(%s, %s) = %s, want %s", tt.left, tt.right, got, tt.want)
			}
		})
	}
}

func TestLogic_Or(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  requirement.RequirementState
		right requirement.RequirementState
		want  requirement.RequirementState
	}{
		// 1. SATISFIED OR SATISFIED = SATISFIED
		{
			name:  "SATISFIED or SATISFIED",
			left:  requirement.RequirementSatisfied,
			right: requirement.RequirementSatisfied,
			want:  requirement.RequirementSatisfied,
		},
		// 2. SATISFIED OR UNSATISFIED = SATISFIED
		{
			name:  "SATISFIED or UNSATISFIED",
			left:  requirement.RequirementSatisfied,
			right: requirement.RequirementUnsatisfied,
			want:  requirement.RequirementSatisfied,
		},
		// 3. SATISFIED OR INDETERMINATE = SATISFIED
		{
			name:  "SATISFIED or INDETERMINATE",
			left:  requirement.RequirementSatisfied,
			right: requirement.RequirementIndeterminate,
			want:  requirement.RequirementSatisfied,
		},
		// 4. UNSATISFIED OR SATISFIED = SATISFIED
		{
			name:  "UNSATISFIED or SATISFIED",
			left:  requirement.RequirementUnsatisfied,
			right: requirement.RequirementSatisfied,
			want:  requirement.RequirementSatisfied,
		},
		// 5. UNSATISFIED OR UNSATISFIED = UNSATISFIED
		{
			name:  "UNSATISFIED or UNSATISFIED",
			left:  requirement.RequirementUnsatisfied,
			right: requirement.RequirementUnsatisfied,
			want:  requirement.RequirementUnsatisfied,
		},
		// 6. UNSATISFIED OR INDETERMINATE = INDETERMINATE
		{
			name:  "UNSATISFIED or INDETERMINATE",
			left:  requirement.RequirementUnsatisfied,
			right: requirement.RequirementIndeterminate,
			want:  requirement.RequirementIndeterminate,
		},
		// 7. INDETERMINATE OR SATISFIED = SATISFIED
		{
			name:  "INDETERMINATE or SATISFIED",
			left:  requirement.RequirementIndeterminate,
			right: requirement.RequirementSatisfied,
			want:  requirement.RequirementSatisfied,
		},
		// 8. INDETERMINATE OR UNSATISFIED = INDETERMINATE
		{
			name:  "INDETERMINATE or UNSATISFIED",
			left:  requirement.RequirementIndeterminate,
			right: requirement.RequirementUnsatisfied,
			want:  requirement.RequirementIndeterminate,
		},
		// 9. INDETERMINATE OR INDETERMINATE = INDETERMINATE
		{
			name:  "INDETERMINATE or INDETERMINATE",
			left:  requirement.RequirementIndeterminate,
			right: requirement.RequirementIndeterminate,
			want:  requirement.RequirementIndeterminate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := requirement.Or(tt.left, tt.right)
			if got != tt.want {
				t.Errorf("Or(%s, %s) = %s, want %s", tt.left, tt.right, got, tt.want)
			}
		})
	}
}

func TestLogic_Not(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state requirement.RequirementState
		want  requirement.RequirementState
	}{
		{
			name:  "NOT SATISFIED = UNSATISFIED",
			state: requirement.RequirementSatisfied,
			want:  requirement.RequirementUnsatisfied,
		},
		{
			name:  "NOT UNSATISFIED = SATISFIED",
			state: requirement.RequirementUnsatisfied,
			want:  requirement.RequirementSatisfied,
		},
		{
			name:  "NOT INDETERMINATE = INDETERMINATE",
			state: requirement.RequirementIndeterminate,
			want:  requirement.RequirementIndeterminate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := requirement.Not(tt.state)
			if got != tt.want {
				t.Errorf("Not(%s) = %s, want %s", tt.state, got, tt.want)
			}
		})
	}
}

func TestLogic_AllAndAny(t *testing.T) {
	t.Parallel()

	t.Run("All empty is SATISFIED", func(t *testing.T) {
		t.Parallel()
		if got := requirement.All(); got != requirement.RequirementSatisfied {
			t.Errorf("All() = %s, want SATISFIED", got)
		}
	})

	t.Run("All with all SATISFIED is SATISFIED", func(t *testing.T) {
		t.Parallel()
		got := requirement.All(requirement.RequirementSatisfied, requirement.RequirementSatisfied)
		if got != requirement.RequirementSatisfied {
			t.Errorf("All(SATISFIED, SATISFIED) = %s, want SATISFIED", got)
		}
	})

	t.Run("All with one UNSATISFIED is UNSATISFIED", func(t *testing.T) {
		t.Parallel()
		got := requirement.All(requirement.RequirementSatisfied, requirement.RequirementUnsatisfied, requirement.RequirementIndeterminate)
		if got != requirement.RequirementUnsatisfied {
			t.Errorf("All(...) = %s, want UNSATISFIED", got)
		}
	})

	t.Run("All with SATISFIED and INDETERMINATE is INDETERMINATE", func(t *testing.T) {
		t.Parallel()
		got := requirement.All(requirement.RequirementSatisfied, requirement.RequirementIndeterminate)
		if got != requirement.RequirementIndeterminate {
			t.Errorf("All(...) = %s, want INDETERMINATE", got)
		}
	})

	t.Run("Any empty is UNSATISFIED", func(t *testing.T) {
		t.Parallel()
		if got := requirement.Any(); got != requirement.RequirementUnsatisfied {
			t.Errorf("Any() = %s, want UNSATISFIED", got)
		}
	})

	t.Run("Any with one SATISFIED is SATISFIED", func(t *testing.T) {
		t.Parallel()
		got := requirement.Any(requirement.RequirementUnsatisfied, requirement.RequirementSatisfied, requirement.RequirementIndeterminate)
		if got != requirement.RequirementSatisfied {
			t.Errorf("Any(...) = %s, want SATISFIED", got)
		}
	})

	t.Run("Any with only UNSATISFIED is UNSATISFIED", func(t *testing.T) {
		t.Parallel()
		got := requirement.Any(requirement.RequirementUnsatisfied, requirement.RequirementUnsatisfied)
		if got != requirement.RequirementUnsatisfied {
			t.Errorf("Any(...) = %s, want UNSATISFIED", got)
		}
	})

	t.Run("Any with UNSATISFIED and INDETERMINATE is INDETERMINATE", func(t *testing.T) {
		t.Parallel()
		got := requirement.Any(requirement.RequirementUnsatisfied, requirement.RequirementIndeterminate)
		if got != requirement.RequirementIndeterminate {
			t.Errorf("Any(...) = %s, want INDETERMINATE", got)
		}
	})
}
