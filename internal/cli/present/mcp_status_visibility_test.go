package clipresent

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	mcpobserve "github.com/isty2e/daem/internal/assurance/observe/mcp"
	"github.com/isty2e/daem/internal/contractversion"
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
		{name: "no catalogued effective observer", dimension: MCPStatusDimension{Dimension: "effective_shadowing", State: "unobserved", Reason: "EFFECTIVE_OBSERVER_UNSUPPORTED"}, quiet: true},
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

func TestMCPObservationCauseSurvivesPlanAndApplyJSON(t *testing.T) {
	for _, observation := range []mcpobserve.ShadowObservation{
		{State: mcpobserve.ShadowObserverUnsupported, Reason: mcpobserve.ReasonEffectiveObserverUnsupported},
		{State: mcpobserve.ShadowUnknown, Reason: mcpobserve.ReasonEffectiveStateUnobserved},
	} {
		status := MCPStatus{Host: []MCPStatusDimension{shadowStatusDimension(observation)}}
		var plan, applied bytes.Buffer
		if err := PrintPlanJSON(&plan, PlanJSONInput{Reconciliation: mcpProjectionPlan(t), MCPStatuses: []MCPStatus{status}}); err != nil {
			t.Fatal(err)
		}
		if err := PrintApplyResultJSON(&applied, ApplyResultJSONInput{MCPStatuses: []MCPStatus{status}}); err != nil {
			t.Fatal(err)
		}
		for _, envelope := range []struct {
			data    []byte
			version int
		}{
			{plan.Bytes(), contractversion.ReconciliationPlanJSON},
			{applied.Bytes(), contractversion.ApplyResultJSON},
		} {
			var payload struct {
				Version  int         `json:"schema_version"`
				Statuses []MCPStatus `json:"mcp_statuses"`
			}
			if err := json.Unmarshal(envelope.data, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Version != envelope.version || len(payload.Statuses) != 1 || len(payload.Statuses[0].Host) != 1 {
				t.Fatalf("missing versioned status evidence: %s", envelope.data)
			}
			dimension := payload.Statuses[0].Host[0]
			if dimension.Dimension != "effective_shadowing" || dimension.State != "unobserved" || dimension.Reason != string(observation.Reason) {
				t.Fatalf("wire projection conflates observation causes: %#v", dimension)
			}
		}
	}
}
