package clipresent

import (
	"bytes"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/reconcile/carrieradoption"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestExtensionSectionsFoldOnlyUnchangedRows(t *testing.T) {
	record, relation := claudePluginCarrierFixture(t)
	expected := relation.ExpectedRelation()
	noOp := claudePluginCarrierActionForSubject(t, record, relation, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			claudePluginManagedRow(t, string(expected.SubjectKey()), string(expected.ManagedInstanceKey())),
		},
	})
	adoption := newPresentCarrierAdoptionFixture(t)
	claim := adoption.claim(t, adoption.owner, durablecarrier.ClaimProvenanceInstalledObserved)
	claimed := adoption.action(t, false, adoption.lifecycle, []durablecarrier.ManagedCarrierClaim{claim})
	candidateRecord, candidateRelation := hostSourceCarrierFixture(t, "candidate", desiredextension.CarrierPiPackage, target.TargetPi, target.ScopeProject, "npm:@acme/candidate")
	candidate := newPresentCarrierAdoptionFixtureFromContract(t, candidateRecord, candidateRelation)
	wouldRecord := candidate.action(t, true, candidate.lifecycle, nil)
	retained := retainedCarrierSummaryAction(t)
	pending := pendingCarrierAbsenceAction(t)
	exact := summaryOrderDecision(t, false)
	normalize := presentRelationOrderDecision(t, target.TargetOpenCode, hostrelation.ConfigOrderOnly)

	tests := []struct {
		name    string
		heading string
		quiet   string
		active  string
		print   func(*bytes.Buffer, bool, HumanOptions)
	}{
		{
			name: "relations", heading: "relation actions", quiet: "no change planned", active: "install extension through host",
			print: func(out *bytes.Buffer, mixed bool, options HumanOptions) {
				actions := []reconcile.RelationAction{noOp}
				if mixed {
					actions = append(actions, codexNoObserverAttemptAction(t))
				}
				PrintRelationActionsWithOptions(out, actions, options)
			},
		},
		{
			name: "adoption", heading: "carrier adoption actions", quiet: "external carrier already claimed", active: "would record external carrier claim",
			print: func(out *bytes.Buffer, mixed bool, options HumanOptions) {
				actions := []carrieradoption.Action{claimed}
				if mixed {
					actions = append(actions, wouldRecord)
				}
				PrintCarrierAdoptionActionsWithOptions(out, actions, options)
			},
		},
		{
			name: "absence", heading: "carrier absence actions", quiet: "retain managed carrier relation", active: "verify pending carrier removal",
			print: func(out *bytes.Buffer, mixed bool, options HumanOptions) {
				actions := []carrierabsence.Action{retained}
				if mixed {
					actions = append(actions, pending)
				}
				PrintCarrierAbsenceActionsWithOptions(out, actions, options)
			},
		},
		{
			name: "order", heading: "extension order", quiet: "runtime extension precedence is exact", active: "normalize extension config order",
			print: func(out *bytes.Buffer, mixed bool, options HumanOptions) {
				actions := []reconcile.RelationOrderDecision{exact}
				if mixed {
					actions = append(actions, normalize)
				}
				PrintRelationOrderActionsWithOptions(out, actions, options)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var ordinary bytes.Buffer
			test.print(&ordinary, false, HumanOptions{})
			unit := "entry"
			if test.name == "order" {
				unit = "sequence"
			}
			want := test.heading + ": 1 " + unit + " (1 unchanged)\n"
			if ordinary.String() != want {
				t.Fatalf("unchanged section = %q, want %q", ordinary.String(), want)
			}

			ordinary.Reset()
			test.print(&ordinary, true, HumanOptions{})
			if !strings.Contains(ordinary.String(), "(1 unchanged)") ||
				strings.Contains(ordinary.String(), test.quiet) || !strings.Contains(ordinary.String(), test.active) {
				t.Fatalf("mixed section lost action or retained passive row: %s", ordinary.String())
			}

			var verbose bytes.Buffer
			test.print(&verbose, true, HumanOptions{Verbose: true})
			if strings.Count(verbose.String(), "  - ") < 2 || strings.Contains(verbose.String(), "unchanged)") {
				t.Fatalf("verbose lost input rows: %s", verbose.String())
			}
		})
	}
}

func retainedCarrierSummaryAction(t *testing.T) carrierabsence.Action {
	t.Helper()
	record, relation := hostSourceCarrierFixture(t, "retained", desiredextension.CarrierPiPackage, target.TargetPi, target.ScopeProject, "npm:@acme/retained")
	fixture := presentCarrierAbsenceActionFromContract(t, record, relation)
	action, err := carrierabsence.NewAction(carrierabsence.ActionInput{
		Claim: fixture.Claim(), Desired: carrierabsence.DesiredRetained, Occupancy: fixture.Occupancy(),
		Route: carrierabsence.UnavailableRoute(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Decision() != carrierabsence.DecisionRetain {
		t.Fatalf("fixture is not retained: %s", action.Decision())
	}
	return action
}

func summaryOrderDecision(t *testing.T, pendingInstall bool) reconcile.RelationOrderDecision {
	t.Helper()
	constraint := presentOrderConstraint(t, target.TargetPi, hostrelation.RuntimePrecedence)
	sequenceID, err := hostrelation.NewPhysicalSequenceID("pi:project:settings.packages")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := observerelation.NewSequenceAuthority("pi:project:settings.packages")
	if err != nil {
		t.Fatal(err)
	}
	revision, err := observerelation.NewSequenceRevision("sha256:summary")
	if err != nil {
		t.Fatal(err)
	}
	var rows []observerelation.ObservedRelationRow
	for _, member := range constraint.Members() {
		rows = append(rows, presentOrderRow(t, member))
	}
	sequence, err := observerelation.NewObservedRelationSequence(constraint.ClassID(), sequenceID, authority, revision, rows)
	if err != nil {
		t.Fatal(err)
	}
	input := reconcile.RelationOrderDecisionInput{
		Target: target.TargetPi, Scope: target.ScopeProject, Constraint: constraint, Sequence: sequence,
	}
	wantKind := reconcile.OrderExact
	if pendingInstall {
		input.PendingInstalls = []topology.SubjectID{constraint.Members()[0].Subject()}
		wantKind = reconcile.OrderConditionalAfterCarrierChange
	}
	decision, err := reconcile.NewRelationOrderDecision(input)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind() != wantKind {
		t.Fatalf("order fixture kind=%s, want %s", decision.Kind(), wantKind)
	}
	return decision
}

func TestOutputResultsHeadingUsesProjectionEntries(t *testing.T) {
	plan := mcpProjectionPlan(t)
	empty, err := reconcile.NewResult(reconcile.ResultInput{Context: reconcile.ContextApply})
	if err != nil {
		t.Fatal(err)
	}
	for _, options := range []HumanOptions{{}, {Verbose: true}} {
		var output bytes.Buffer
		PrintPlanResultWithOptions(&output, plan, options)
		if !strings.HasPrefix(output.String(), "output results: 1 output entry\n") {
			t.Fatalf("result lacks its counted sibling heading: %s", output.String())
		}
		output.Reset()
		PrintPlanResultWithOptions(&output, empty, options)
		if output.Len() != 0 {
			t.Fatalf("empty output results became noisy: %s", output.String())
		}
	}
}
