package clipresent

import (
	"bytes"
	"strings"
	"testing"
)

func TestMCPDefaultVisibilityKeepsUnknownAndFailedFacts(t *testing.T) {
	for _, test := range []struct {
		name      string
		dimension MCPStatusDimension
		quiet     bool
	}{
		{name: "adopted ownership", dimension: MCPStatusDimension{Dimension: "same_scope_ownership", State: "adopted"}, quiet: true},
		{name: "unshadowed", dimension: MCPStatusDimension{Dimension: "effective_shadowing", State: "unshadowed"}, quiet: true},
		{name: "matching success", dimension: MCPStatusDimension{Dimension: "delegate_last_attempt", State: "succeeded"}, quiet: true},
		{name: "no matching history", dimension: MCPStatusDimension{Dimension: "delegate_last_attempt", State: "not_observed", Reason: "LAST_DELEGATE_ATTEMPT_UNOBSERVED"}, quiet: true},
		{name: "actual effective uncertainty", dimension: MCPStatusDimension{Dimension: "effective_shadowing", State: "unobserved", Reason: "EFFECTIVE_STATE_UNOBSERVED"}},
		{name: "ownership uncertainty", dimension: MCPStatusDimension{Dimension: "same_scope_ownership", State: "unobserved", Reason: "OWNERSHIP_STATE_UNOBSERVED"}},
		{name: "failed attempt", dimension: MCPStatusDimension{Dimension: "delegate_last_attempt", State: "failed", Reason: "DELEGATE_TIMEOUT"}},
		{name: "stale attempt", dimension: MCPStatusDimension{Dimension: "delegate_last_attempt", State: "stale", Reason: "LAST_DELEGATE_ATTEMPT_STALE"}},
		{name: "blocked attempt", dimension: MCPStatusDimension{Dimension: "delegate_last_attempt", State: "blocked", Reason: "DELEGATE_POLICY_BLOCKED"}},
		{name: "unrecognized success axis", dimension: MCPStatusDimension{Dimension: "future_dimension", State: "succeeded"}},
		{name: "success with unknown reason", dimension: MCPStatusDimension{Dimension: "delegate_last_attempt", State: "succeeded", Reason: "FUTURE_REASON"}},
		{name: "absence with unknown reason", dimension: MCPStatusDimension{Dimension: "delegate_last_attempt", State: "not_observed", Reason: "FUTURE_REASON"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			status := MCPStatus{
				Subject: MCPSubject{Kind: "projection", Namespace: "claude-code.project.mcp-server", Name: "context7"},
				Target:  "claude-code", Scope: "project", Other: []MCPStatusDimension{test.dimension},
			}
			var basic, verbose bytes.Buffer
			PrintMCPStatusesWithOptions(&basic, []MCPStatus{status}, HumanOptions{})
			PrintMCPStatusesWithOptions(&verbose, []MCPStatus{status}, HumanOptions{Verbose: true})
			fact := test.dimension.Dimension + ": " + test.dimension.State
			if (basic.Len() == 0) != test.quiet || (!test.quiet && !strings.Contains(basic.String(), fact)) {
				t.Fatalf("basic output quiet=%t: %q", test.quiet, basic.String())
			}
			if !strings.Contains(verbose.String(), fact) || (test.dimension.Reason != "" && !strings.Contains(verbose.String(), "reason="+test.dimension.Reason)) {
				t.Fatalf("verbose lost evidence: %s", verbose.String())
			}
		})
	}
}
