package clipresent

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
)

func TestPrintPlanIncludesSharedConsumerTargets(t *testing.T) {
	fixture := newManagedPathPlanFixture(
		t,
		"oracle",
		"oracle",
		target.ScopeProject,
		[]target.Target{target.TargetAntigravityCLI, target.TargetCodex},
		"desired",
	)
	planResult := fixture.buildPlan(t, managedPathPlanInput{
		includeDesired: true,
		selectedTargets: []target.Target{
			target.TargetAntigravityCLI,
			target.TargetCodex,
		},
		evidence: []observe.ManagedPathEvidence{fixture.evidence(t, false, "")},
	})

	var stdout bytes.Buffer
	PrintActionPlanWithOptions(&stdout, "dry-run", planResult, HumanOptions{Verbose: true})
	if !strings.Contains(stdout.String(), "targets=antigravity-cli,codex") {
		t.Fatalf("stdout = %q, want shared targets", stdout.String())
	}
}

func TestPrintPlanIncludesOwnershipConflictProvenance(t *testing.T) {
	fixture := newManagedPathPlanFixture(
		t,
		"oracle",
		"oracle",
		target.ScopeGlobal,
		[]target.Target{target.TargetCodex},
		"desired",
	)
	requester, err := ownership.NewOwnerAuthority("/work/right/state.json", "/work/right/daem.toml")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := ownership.NewOwnerAuthority("/work/left/state.json", "/work/left/daem.toml")
	if err != nil {
		t.Fatal(err)
	}
	address, err := ownership.NewManagedAddress(filepath.Join(t.TempDir(), "oracle"), "")
	if err != nil {
		t.Fatal(err)
	}
	activeClaim, err := ownership.NewActiveClaim(address, foreign)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := ownership.PresentClaim(activeClaim)
	if err != nil {
		t.Fatal(err)
	}
	planResult := fixture.buildPlan(t, managedPathPlanInput{
		includeDesired:  true,
		selectedTargets: []target.Target{target.TargetCodex},
		evidence:        []observe.ManagedPathEvidence{fixture.evidence(t, true, "desired")},
		owner:           requester,
		ownership: []observe.OwnershipObservation{{
			Destination: fixture.destination,
			Address:     address,
			Claim:       claim,
		}},
	})

	var stdout bytes.Buffer
	PrintStatusPlanWithOptions(&stdout, planResult, HumanOptions{})
	if !strings.Contains(stdout.String(), `detail: managed address is claimed by manifest "/work/left/daem.toml"`) {
		t.Fatalf("stdout = %q, want ownership provenance", stdout.String())
	}
}

func TestPrintPlanResultIncludesCompleteCountsAndRows(t *testing.T) {
	managedPaths := make([]reconcile.ManagedPathDecision, 5)
	for index := range managedPaths {
		name := fmt.Sprintf("oracle-%d", index)
		fixture := newManagedPathPlanFixture(
			t,
			name,
			name,
			target.ScopeProject,
			[]target.Target{target.TargetCodex},
			"desired",
		)
		managedPaths[index] = fixture.buildPlan(t, managedPathPlanInput{
			includeDesired:  true,
			selectedTargets: []target.Target{target.TargetCodex},
			evidence:        []observe.ManagedPathEvidence{fixture.evidence(t, false, "")},
		}).ManagedPaths()[0]
	}
	planResult := mustReconciliationPlan(t, managedPaths, nil)

	var defaultOutput bytes.Buffer
	PrintPlanResultWithOptions(&defaultOutput, planResult, HumanOptions{})
	if got := strings.Count(defaultOutput.String(), "add managed output resource="); got != 5 {
		t.Fatalf("default rows = %d, want 5; output=%q", got, defaultOutput.String())
	}
	if !strings.Contains(defaultOutput.String(), "add managed output: 5") {
		t.Fatalf("default output = %q, want complete count", defaultOutput.String())
	}

	var verboseOutput bytes.Buffer
	PrintPlanResultWithOptions(&verboseOutput, planResult, HumanOptions{Verbose: true})
	if got := strings.Count(verboseOutput.String(), "create resource="); got != 5 {
		t.Fatalf("verbose rows = %d, want 5; output=%q", got, verboseOutput.String())
	}
}

func TestPrintPlanHumanOutputDistinguishesRecordReasons(t *testing.T) {
	adopted := newManagedPathPlanFixture(
		t,
		"adopted",
		"adopted",
		target.ScopeProject,
		[]target.Target{target.TargetCodex},
		"desired",
	)
	adoption := adopted.buildPlan(t, managedPathPlanInput{
		includeDesired:       true,
		selectedTargets:      []target.Target{target.TargetCodex},
		evidence:             []observe.ManagedPathEvidence{adopted.evidence(t, true, "desired")},
		manageUnmanagedMatch: true,
	}).ManagedPaths()[0]
	shared := newManagedPathPlanFixture(
		t,
		"shared",
		"shared",
		target.ScopeProject,
		[]target.Target{target.TargetAntigravityCLI, target.TargetCodex},
		"desired",
	)
	ownershipUpdate := shared.buildPlan(t, managedPathPlanInput{
		selectedTargets: []target.Target{target.TargetCodex},
		states: []durable.ManagedPathState{
			shared.state(t, []target.Target{target.TargetAntigravityCLI, target.TargetCodex}, "desired"),
		},
		evidence: []observe.ManagedPathEvidence{shared.evidence(t, true, "desired")},
	}).ManagedPaths()[0]
	sharedSecond := newManagedPathPlanFixture(
		t,
		"shared-second",
		"shared-second",
		target.ScopeProject,
		[]target.Target{target.TargetAntigravityCLI, target.TargetCodex},
		"desired",
	)
	ownershipUpdateSecond := sharedSecond.buildPlan(t, managedPathPlanInput{
		selectedTargets: []target.Target{target.TargetCodex},
		states: []durable.ManagedPathState{
			sharedSecond.state(t, []target.Target{target.TargetAntigravityCLI, target.TargetCodex}, "desired"),
		},
		evidence: []observe.ManagedPathEvidence{sharedSecond.evidence(t, true, "desired")},
	}).ManagedPaths()[0]
	planResult := mustReconciliationPlan(
		t,
		[]reconcile.ManagedPathDecision{adoption, ownershipUpdate, ownershipUpdateSecond},
		nil,
	)

	var planOutput bytes.Buffer
	PrintActionPlanWithOptions(&planOutput, "status", planResult, HumanOptions{})
	for _, want := range []string{
		`manage existing matching output resource="skill/adopted"`,
		`update managed ownership resource="skill/shared"`,
	} {
		if !strings.Contains(planOutput.String(), want) {
			t.Fatalf("plan output = %q, want %q", planOutput.String(), want)
		}
	}

	var resultOutput bytes.Buffer
	PrintPlanResultWithOptions(&resultOutput, planResult, HumanOptions{})
	for _, want := range []string{
		"manage existing matching output: 1",
		"update managed ownership: 2",
	} {
		if !strings.Contains(resultOutput.String(), want) {
			t.Fatalf("result output = %q, want %q", resultOutput.String(), want)
		}
	}
	if got := strings.Count(resultOutput.String(), ` resource="skill/`); got != 3 {
		t.Fatalf("result rows = %d, want 3; output=%q", got, resultOutput.String())
	}
}

func TestPrintPlanUsesSubjectForAggregateProjectionDecisions(t *testing.T) {
	planResult := mcpProjectionPlan(t)

	var stdout bytes.Buffer
	PrintActionPlanWithOptions(&stdout, "dry-run", planResult, HumanOptions{Verbose: true})

	for _, want := range []string{
		`create subject="projection/claude-code.project.mcp-server/context7" target=claude-code scope=project destination=".mcp.json" content_path="/mcpServers/context7" reason=missing_output`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	if strings.Contains(stdout.String(), `resource="/"`) || strings.Contains(stdout.String(), `resource="/context7"`) {
		t.Fatalf("stdout leaked fake resource identity: %q", stdout.String())
	}
}

func TestPrintPlansReportSafetyStates(t *testing.T) {
	removed := newManagedPathPlanFixture(
		t, "removed", "removed", target.ScopeProject, []target.Target{target.TargetCodex}, "state",
	)
	drifted := newManagedPathPlanFixture(
		t, "drifted", "drifted", target.ScopeProject, []target.Target{target.TargetCodex}, "state",
	)
	missingEvidence := newManagedPathPlanFixture(
		t, "missing-evidence", "missing-evidence", target.ScopeProject, []target.Target{target.TargetCodex}, "state",
	)
	removeDecision := removed.buildPlan(t, managedPathPlanInput{
		selectedTargets: []target.Target{target.TargetCodex},
		states: []durable.ManagedPathState{
			removed.state(t, []target.Target{target.TargetCodex}, "state"),
		},
		evidence: []observe.ManagedPathEvidence{removed.evidence(t, true, "state")},
	}).ManagedPaths()[0]
	driftDecision := drifted.buildPlan(t, managedPathPlanInput{
		selectedTargets: []target.Target{target.TargetCodex},
		states: []durable.ManagedPathState{
			drifted.state(t, []target.Target{target.TargetCodex}, "state"),
		},
		evidence: []observe.ManagedPathEvidence{drifted.evidence(t, true, "external")},
	}).ManagedPaths()[0]
	missingDecision := missingEvidence.buildPlan(t, managedPathPlanInput{
		selectedTargets: []target.Target{target.TargetCodex},
		states: []durable.ManagedPathState{
			missingEvidence.state(t, []target.Target{target.TargetCodex}, "state"),
		},
	}).ManagedPaths()[0]

	var stdout bytes.Buffer
	PrintDryRunPlanWithOptions(
		&stdout,
		mustReconciliationPlan(
			t,
			[]reconcile.ManagedPathDecision{removeDecision, driftDecision, missingDecision},
			nil,
		),
		HumanOptions{Verbose: true},
	)

	for _, want := range []string{
		`delete resource="skill/removed" target=codex scope=project`,
		`reason=removed_from_manifest`,
		`safety=deletable`,
		`error resource="skill/drifted" target=codex scope=project`,
		`reason=drifted_output`,
		`safety=drift_blocked`,
		`error resource="skill/missing-evidence" target=codex scope=project`,
		`reason=missing_live_observation`,
		`safety=missing_evidence`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestPrintRelationActionsDoesNotUseSuccessWording(t *testing.T) {
	action := claudePluginCarrierAction(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})

	var stdout bytes.Buffer
	PrintRelationActionsWithOptions(&stdout, []reconcile.RelationAction{action}, HumanOptions{Verbose: true})
	if !strings.Contains(stdout.String(), "relation actions: 1 subjects") {
		t.Fatalf("stdout = %q, want generic relation actions header", stdout.String())
	}
	for _, forbidden := range []string{"installed", "enabled", "ready", "converged", "applied", "success"} {
		if strings.Contains(stdout.String(), forbidden) {
			t.Fatalf("stdout = %q, want no %q wording", stdout.String(), forbidden)
		}
	}
	if strings.Contains(stdout.String(), "claude plugin carrier actions") {
		t.Fatalf("stdout = %q, want no legacy Claude plugin carrier header", stdout.String())
	}
	if !strings.Contains(stdout.String(), "invokes_host_route=false") ||
		!strings.Contains(stdout.String(), `route_admission_row="RA-01"`) ||
		!strings.Contains(stdout.String(), "requested_outcome=ordinary-mutation") ||
		!strings.Contains(stdout.String(), "selected_outcome=blocked") ||
		!strings.Contains(stdout.String(), "evidence_source=passive_relation_inventory") ||
		!strings.Contains(stdout.String(), "evidence_availability=supported") ||
		!strings.Contains(stdout.String(), "evidence_freshness=fresh") ||
		!strings.Contains(stdout.String(), "replay_boundary=locked_route_request_identity_only") ||
		!strings.Contains(stdout.String(), "retained_effects=") ||
		!strings.Contains(stdout.String(), "non_claims=") ||
		!strings.Contains(stdout.String(), "allows_host_route_invocation=false") ||
		!strings.Contains(stdout.String(), "blocks_ordinary_apply=true") {
		t.Fatalf("stdout = %q, want non-executing blocked disclosure", stdout.String())
	}
}

func TestPrintRelationActionsDisclosesHostDelegatedInvocationWithoutSuccessWording(t *testing.T) {
	action := claudePluginCarrierActionWithSelectedOutcome(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	}, reconcile.AdmissionOutcomeHostDelegated)

	var stdout bytes.Buffer
	PrintRelationActionsWithOptions(&stdout, []reconcile.RelationAction{action}, HumanOptions{Verbose: true})
	output := stdout.String()
	for _, required := range []string{
		"kind=create",
		"execution=host_route",
		"selected_outcome=host-delegated",
		"invokes_host_route=true",
		"allows_host_route_invocation=true",
		"blocks_ordinary_apply=false",
		"replay_boundary=locked_route_request_identity_only",
		"exact_artifact_replay",
		"future_skip_authority",
	} {
		if !strings.Contains(output, required) {
			t.Fatalf("stdout = %q, want %q", output, required)
		}
	}
	for _, forbidden := range []string{
		"installed",
		"enabled",
		"ready",
		"converged",
		"success",
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("stdout = %q, want no %q wording", output, forbidden)
		}
	}
	if strings.Contains(output, `non_claims=["host_route_invocation"`) ||
		strings.Contains(output, ` "host_route_invocation"`) {
		t.Fatalf("stdout = %q, want no host_route_invocation non-claim", output)
	}
}
