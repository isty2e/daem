// Package archguard enforces repository architecture and documentation guardrails.
//
// It analyzes the current import graph, forbidden package shapes, and stable
// documentation reference rules. Documentation analysis is intentionally a
// bounded repository check rather than a general Markdown parser.
//
// Run the enforced baseline with:
//
//	go test -run 'Test(Topology|Documentation)GuardBaseline' -count=1 -v ./internal/archguard
//
// Production packages must not import archguard.
package archguard
