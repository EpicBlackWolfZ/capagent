package requirement

// And evaluates the 3-valued logical conjunction of two requirement states.
//
// In accordance with capagent truth tables (architecture.md §3.1):
// - Short-circuits definite failure (UNSATISFIED).
// - Preserves INDETERMINATE when neither branch has definitely failed.
func And(a, b RequirementState) RequirementState {
	if a == RequirementUnsatisfied || b == RequirementUnsatisfied {
		return RequirementUnsatisfied
	}
	if a.IsValid() != nil || b.IsValid() != nil || a == RequirementIndeterminate || b == RequirementIndeterminate {
		return RequirementIndeterminate
	}
	return RequirementSatisfied
}

// Or evaluates the 3-valued logical disjunction of two requirement states.
//
// In accordance with capagent truth tables (architecture.md §3.2):
// - Short-circuits definite satisfaction (SATISFIED).
// - Preserves INDETERMINATE when alternative branches have failed or remain unknown.
// - Returns INDETERMINATE if an operand is invalid and not short-circuited by SATISFIED.
func Or(a, b RequirementState) RequirementState {
	if a == RequirementSatisfied || b == RequirementSatisfied {
		return RequirementSatisfied
	}
	if a.IsValid() != nil || b.IsValid() != nil || a == RequirementIndeterminate || b == RequirementIndeterminate {
		return RequirementIndeterminate
	}
	return RequirementUnsatisfied
}

// Not evaluates the 3-valued logical negation of a requirement predicate.
//
// In accordance with architecture.md §3.3:
// - Negation applies strictly to requirement evaluation predicates.
// - NOT(SATISFIED) = UNSATISFIED
// - NOT(UNSATISFIED) = SATISFIED
// - NOT(INDETERMINATE) = INDETERMINATE
func Not(s RequirementState) RequirementState {
	switch s {
	case RequirementSatisfied:
		return RequirementUnsatisfied
	case RequirementUnsatisfied:
		return RequirementSatisfied
	default:
		return RequirementIndeterminate
	}
}

// All evaluates the logical conjunction across a slice of requirement states.
//
// Strictly implemented as a left-associative fold over And. An empty slice evaluates
// to SATISFIED (the identity element of conjunction).
func All(states ...RequirementState) RequirementState {
	res := RequirementSatisfied
	for _, s := range states {
		res = And(res, s)
	}
	return res
}

// Any evaluates the logical disjunction across a slice of requirement states.
//
// Strictly implemented as a left-associative fold over Or. An empty slice evaluates
// to UNSATISFIED (the identity element of disjunction).
func Any(states ...RequirementState) RequirementState {
	res := RequirementUnsatisfied
	for _, s := range states {
		res = Or(res, s)
	}
	return res
}
