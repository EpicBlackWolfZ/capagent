// Package output provides deterministic JSON serialization and DTO projections for
// the Schema v1 container compatibility evaluation report format.
//
// In accordance with repository architecture invariants, Schema v1 represents an external,
// versioned, machine-readable wire contract and is strictly decoupled from the internal
// domain model (internal/model).
//
// # API Lifecycle
//
// The package supports both builder-style report assembly and direct domain projection:
//
//   - NewReport constructs an empty mutable builder/skeleton with initialized non-nil maps
//     and SchemaVersion set to CurrentSchemaVersion.
//   - NewReportFromModel projects internal domain types into a complete Report DTO.
//   - (*Report).Validate checks report-level invariants not guaranteed by Go's type system
//     (such as required collections, enum validity, and non-nil evidence slices).
//     Note that Validate is not a full JSON Schema validator; authoritative wire-contract
//     validation is performed by the JSON Schema suite.
//   - Marshal and MarshalCompact serialize a Report non-destructively. Marshal does not
//     automatically invoke Validate; callers assembling reports manually should invoke
//     Validate prior to serialization if contract enforcement is required.
//
// # Deterministic Serialization
//
// Using standard library encoding/json/v2 with json.Deterministic(true) and lexicographical
// evidence slice sorting, identical inputs produce canonical byte-identical output for a
// given build.
package output

