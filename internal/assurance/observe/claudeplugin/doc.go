// Package claudeplugin adapts passive Claude Code plugin inventory facts.
//
// The generic relation inventory and correlation model lives in observe/relation.
// This package preserves Claude-specific boundary facts such as host inventory
// scope, trust, activation, and prior delegate attempts. It does not collect
// inventory, invoke Claude Code, grant mutation authority, implement adoption,
// or treat those facts as convergence.
package claudeplugin
