// Package relation models passive relation inventory and correlation facts.
//
// The package owns generic observation availability, freshness, row identity,
// same-subject/shadow/drift/ambiguity correlation, and non-authoritative
// watchpoints. It does not parse host config, collect inventory, plan actions,
// invoke routes, grant adoption authority, or prove convergence.
package relation
