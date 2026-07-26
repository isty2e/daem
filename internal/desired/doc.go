// Package desired owns the canonical authored desired environment aggregate.
//
// Family packages own family-local behavior. This root owns only manifest-wide
// collection invariants, normalized defaults and target context, immutable
// family collections, and cross-family declared references. It does not own
// declaration syntax, source I/O, host realization, current state,
// reconciliation, or effects.
package desired
