// Package requirement implements the 3-valued Boolean logic evaluation engine
// for capagent workload requirements.
//
// In accordance with capagent architecture, requirement states (SATISFIED, UNSATISFIED,
// INDETERMINATE) and their truth tables are strictly isolated from operational capability
// states (supported, unsupported, misconfigured, unavailable, unknown).
package requirement
