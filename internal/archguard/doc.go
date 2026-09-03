// Package archguard enforces repository architecture guardrails.
//
// It analyzes the current import graph and forbidden package shapes. Blocking
// findings feed TestTopologyGuardBaseline. Compiler and State Barrier
// prefix-role checks are report-only shadow findings and never fail that
// baseline.
//
// Run the enforced baseline with:
//
//	tools/test-go.sh -run TestTopologyGuardBaseline -count=1 -v ./internal/archguard
//
// Production packages must not import archguard.
package archguard
