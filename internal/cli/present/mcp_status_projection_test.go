package clipresent

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	mcpobserve "github.com/isty2e/daem/internal/assurance/observe/mcp"
	"github.com/isty2e/daem/internal/assurance/runtimeprobe"
	"github.com/isty2e/daem/internal/target"
)

func TestMCPStatusEvidencePreservesPublicGroupsRowsAndOrder(t *testing.T) {
	runtime, err := runtimeprobe.FoldFacts(nil)
	if err != nil {
		t.Fatalf("FoldFacts returned error: %v", err)
	}
	current := mcpobserve.AggregateProjectionObservation{
		Projection: mcpobserve.ProjectionObservation{State: mcpobserve.ProjectionProjected},
		Ownership:  mcpobserve.OwnershipObservation{State: mcpobserve.OwnershipUnknown},
		Shadowing: mcpobserve.ShadowObservation{
			State:  mcpobserve.ShadowCarrierCollision,
			Reason: mcpobserve.ReasonConfigShadowed,
		},
	}
	history := mcpobserve.LastDelegateAttemptObservation{
		State:  mcpobserve.DelegateAttemptNotObserved,
		Reason: mcpobserve.ReasonLastDelegateAttemptUnobserved,
	}

	got, err := mcpStatusEvidence(target.ScopeProject, current, history, runtime)
	if err != nil {
		t.Fatalf("mcpStatusEvidence returned error: %v", err)
	}
	if !slices.Equal(got.Projection, []MCPStatusDimension{
		{Dimension: "project_projection", State: string(mcpobserve.ProjectionProjected)},
	}) {
		t.Fatalf("projection dimensions = %#v", got.Projection)
	}
	if !slices.Equal(got.Host, []MCPStatusDimension{
		{Dimension: "same_scope_ownership", State: "unobserved"},
		{Dimension: "effective_shadowing", State: string(mcpobserve.ShadowCarrierCollision), Reason: string(mcpobserve.ReasonConfigShadowed)},
	}) {
		t.Fatalf("host dimensions = %#v", got.Host)
	}
	if !slices.Equal(got.Delegate, []MCPStatusDimension{
		{Dimension: "delegate_last_attempt", State: string(mcpobserve.DelegateAttemptNotObserved), Reason: string(mcpobserve.ReasonLastDelegateAttemptUnobserved)},
	}) {
		t.Fatalf("delegate dimensions = %#v", got.Delegate)
	}
	if !slices.Equal(got.Runtime, []MCPStatusDimension{
		{Dimension: "runtime_launcher", State: string(runtimeprobe.NotProbed), Reason: string(runtimeprobe.ReasonNotProbed)},
		{Dimension: "protocol_initialize", State: string(runtimeprobe.NotProbed), Reason: string(runtimeprobe.ReasonNotProbed)},
		{Dimension: "runtime_authentication", State: string(runtimeprobe.NotProbed), Reason: string(runtimeprobe.ReasonNotProbed)},
		{Dimension: "endpoint_health", State: string(runtimeprobe.NotProbed), Reason: string(runtimeprobe.ReasonNotProbed)},
		{Dimension: "tool_inventory", State: string(runtimeprobe.NotProbed), Reason: string(runtimeprobe.ReasonNotProbed)},
	}) {
		t.Fatalf("runtime dimensions = %#v", got.Runtime)
	}
	if !slices.Equal(got.Residue, []MCPStatusDimension{
		{Dimension: "adoption_orphan_residue", State: string(mcpobserve.ResidueNotApplicable)},
	}) {
		t.Fatalf("residue dimensions = %#v", got.Residue)
	}
	if len(got.Other) != 0 {
		t.Fatalf("other dimensions = %#v, want none", got.Other)
	}

	global, err := mcpStatusEvidence(target.ScopeGlobal, current, history, runtime)
	if err != nil {
		t.Fatalf("global mcpStatusEvidence returned error: %v", err)
	}
	if global.Projection[0].Dimension != "global_projection" {
		t.Fatalf("global projection dimension = %q", global.Projection[0].Dimension)
	}
	if _, err := mcpStatusEvidence(target.Scope("workspace"), current, history, runtime); err == nil {
		t.Fatal("mcpStatusEvidence accepted unsupported scope")
	}
}

func TestMCPStatusEvidenceDoesNotCarryRuntimeDetail(t *testing.T) {
	runtime, err := runtimeprobe.FoldFacts([]runtimeprobe.Fact{{
		Dimension:       runtimeprobe.DimensionLauncher,
		State:           runtimeprobe.ObservedFailed,
		Reason:          runtimeprobe.ReasonObservedFailed,
		Source:          runtimeprobe.SourceExplicit,
		Freshness:       runtimeprobe.FreshnessCurrent,
		SanitizedDetail: "runtime-detail-canary",
	}})
	if err != nil {
		t.Fatalf("FoldFacts returned error: %v", err)
	}
	status, err := mcpStatusEvidence(
		target.ScopeProject,
		mcpobserve.AggregateProjectionObservation{},
		mcpobserve.LastDelegateAttemptObservation{},
		runtime,
	)
	if err != nil {
		t.Fatalf("mcpStatusEvidence returned error: %v", err)
	}
	if strings.Contains(fmt.Sprintf("%#v", status), "runtime-detail-canary") {
		t.Fatalf("passive status leaked active runtime detail: %#v", status)
	}
}
