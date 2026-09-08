package requirement

import (
	"fmt"
)

// RequirementState represents the 3-valued logic evaluation result of a requirement.
type RequirementState string

const (
	// RequirementSatisfied indicates all required conditions are affirmatively supported.
	RequirementSatisfied RequirementState = "SATISFIED"
	// RequirementUnsatisfied indicates at least one required condition is unsupported or misconfigured.
	RequirementUnsatisfied RequirementState = "UNSATISFIED"
	// RequirementIndeterminate indicates determination was prevented due to unknown or unavailable evidence.
	RequirementIndeterminate RequirementState = "INDETERMINATE"
)

// String returns the string representation of the RequirementState.
func (s RequirementState) String() string {
	return string(s)
}

// IsValid validates whether the state is one of the three canonical requirement states.
func (s RequirementState) IsValid() error {
	switch s {
	case RequirementSatisfied, RequirementUnsatisfied, RequirementIndeterminate:
		return nil
	default:
		return fmt.Errorf("invalid requirement state: %q", s)
	}
}
