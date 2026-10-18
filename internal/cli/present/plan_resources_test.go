package clipresent

import (
	"bytes"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/target"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func TestProjectionIdentityUsesMCPNamespaceBeforeEntityKeyGrammar(t *testing.T) {
	subject, err := topologymcp.ProjectionSubject(target.TargetClaudeCode, target.ScopeProject, "skill/tools")
	if err != nil {
		t.Fatal(err)
	}
	label, reference := projectionIdentityLabel(subject)
	if label != "resource" || reference != "mcp-server/skill/tools" {
		t.Fatalf("MCP family identity became %s=%q", label, reference)
	}
}

func TestResourcePlanRetainsRepeatedAndUnassociatedMCPObservations(t *testing.T) {
	plan := mcpProjectionPlan(t)
	decision, _ := plan.Decisions()[0].Aggregate()
	matched := MCPStatus{
		Subject: mcpSubjectFromID(decision.Subject()), Target: string(decision.Target()), Scope: string(decision.Scope()),
		Host: []MCPStatusDimension{{Dimension: "effective_shadowing", State: "unobserved", Reason: "EFFECTIVE_STATE_UNOBSERVED"}},
	}
	second := matched
	second.Host = nil
	second.Delegate = []MCPStatusDimension{{Dimension: "delegate_last_attempt", State: "not_observed"}}
	otherSubject, err := topologymcp.ProjectionSubject(target.TargetClaudeCode, target.ScopeGlobal, "other")
	if err != nil {
		t.Fatal(err)
	}
	other := MCPStatus{
		Subject: mcpSubjectFromID(otherSubject), Target: "claude-code", Scope: "global",
		Projection: []MCPStatusDimension{{Dimension: "global_projection", State: "missing"}},
	}

	var output bytes.Buffer
	PrintStatusPlanWithOptions(&output, HumanPlanInput{Reconciliation: plan, MCPStatuses: []MCPStatus{matched, second, other}}, HumanOptions{})
	text := output.String()
	for _, fact := range []string{
		`resource="mcp-server/context7" target=claude-code scope=project`,
		"effective_shadowing: unobserved reason=EFFECTIVE_STATE_UNOBSERVED",
		"delegate_last_attempt: not_observed",
		`subject="projection/claude-code.global.mcp-server/other" target=claude-code scope=global`,
		"global_projection: missing",
	} {
		if strings.Count(text, fact) != 1 {
			t.Fatalf("fact %q was lost or duplicated: %s", fact, text)
		}
	}
}

func TestResourcePlanOmitsOnlyRoutineNoOpConfiguration(t *testing.T) {
	fixture := newManagedPathPlanFixture(t, "current", "current", target.ScopeProject, []target.Target{target.TargetCodex}, "desired")
	plan := fixture.buildPlan(t, managedPathPlanInput{
		includeDesired: true, selectedTargets: []target.Target{target.TargetCodex},
		states:   []durable.ManagedPathState{fixture.state(t, []target.Target{target.TargetCodex}, "desired")},
		evidence: []observe.ManagedPathEvidence{fixture.evidence(t, true, "desired")},
	})
	var ordinary bytes.Buffer
	PrintStatusPlanWithOptions(&ordinary, HumanPlanInput{Reconciliation: plan}, HumanOptions{})
	if ordinary.String() != "status: 1 output entry (current=1 pending=0 blocked=0)\n" {
		t.Fatalf("routine no-op output = %q", ordinary.String())
	}

	var verbose bytes.Buffer
	PrintStatusPlanWithOptions(&verbose, HumanPlanInput{Reconciliation: plan}, HumanOptions{Verbose: true})
	for _, fact := range []string{`resource="skill/current" target=codex scope=project`, "output: noop", fixture.destination.String()} {
		if !strings.Contains(verbose.String(), fact) {
			t.Fatalf("verbose omitted no-op fact %q: %s", fact, verbose.String())
		}
	}
}
