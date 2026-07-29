package clipresent

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestRelationOrderJSONDisclosesPhysicalSequenceAndForeignRisk(t *testing.T) {
	decision := presentRelationOrderDecision(
		t,
		target.TargetPi,
		hostrelation.RuntimePrecedence,
	)
	rows := relationOrderJSONActions([]reconcile.RelationOrderDecision{decision})
	if len(rows) != 1 {
		t.Fatalf("rows = %#v", rows)
	}
	row := rows[0]
	if row.ClassID != "extension:pi:project:packages" ||
		row.SequenceID != "pi:project:settings.packages" ||
		row.RuntimeMeaning != string(hostrelation.RuntimePrecedence) ||
		row.Kind != string(reconcile.OrderNormalize) ||
		len(row.DesiredMembers) != 2 ||
		len(row.ObservedMembers) != 2 ||
		row.ForeignRowCount != 1 ||
		len(row.Risks) != 2 ||
		row.Risks[0].Code != foreignPrecedenceChangeRisk ||
		!row.RequiresMutation ||
		row.BlocksOrdinaryApply {
		t.Fatalf("row = %#v", row)
	}
}

func TestPlanAndApplyJSONEnvelopesIncludeRelationOrderActions(t *testing.T) {
	decision := presentRelationOrderDecision(
		t,
		target.TargetPi,
		hostrelation.RuntimePrecedence,
	)
	result, err := reconcile.NewResult(reconcile.ResultInput{
		Context:        reconcile.ContextDryRun,
		RelationOrders: []reconcile.RelationOrderDecision{decision},
	})
	if err != nil {
		t.Fatal(err)
	}

	var planOutput bytes.Buffer
	if err := PrintPlanJSON(&planOutput, PlanJSONInput{
		Command:        "status",
		Mode:           "status",
		Reconciliation: result,
	}); err != nil {
		t.Fatal(err)
	}
	var planPayload planJSONOutput
	if err := json.Unmarshal(planOutput.Bytes(), &planPayload); err != nil {
		t.Fatal(err)
	}
	if planPayload.SchemaVersion != planJSONSchemaVersion ||
		len(planPayload.RelationOrders) != 1 {
		t.Fatalf("plan payload = %#v", planPayload)
	}

	var applyOutput bytes.Buffer
	if err := PrintApplyResultJSON(&applyOutput, ApplyResultJSONInput{
		Reconciliation: result,
	}); err != nil {
		t.Fatal(err)
	}
	var applyPayload applyResultJSONOutput
	if err := json.Unmarshal(applyOutput.Bytes(), &applyPayload); err != nil {
		t.Fatal(err)
	}
	if applyPayload.SchemaVersion != applyResultJSONSchemaVersion ||
		len(applyPayload.RelationOrders) != 1 {
		t.Fatalf("apply payload = %#v", applyPayload)
	}
}

func TestPrintRelationOrderActionsDistinguishesRuntimeAndConfigOrder(t *testing.T) {
	runtimeDecision := presentRelationOrderDecision(
		t,
		target.TargetPi,
		hostrelation.RuntimePrecedence,
	)
	configDecision := presentRelationOrderDecision(
		t,
		target.TargetOpenCode,
		hostrelation.ConfigOrderOnly,
	)
	var output bytes.Buffer
	PrintRelationOrderActionsWithOptions(
		&output,
		[]reconcile.RelationOrderDecision{runtimeDecision, configDecision},
		HumanOptions{},
	)
	text := output.String()
	for _, want := range []string{
		"runtime extension precedence",
		"extension config order",
		"includes 2 managed/foreign precedence changes",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output lacks %q:\n%s", want, text)
		}
	}
}

func TestPrintRelationOrderActionsDisclosesTypedObservationBlock(t *testing.T) {
	constraint := presentOrderConstraint(
		t,
		target.TargetPi,
		hostrelation.RuntimePrecedence,
	)
	sequenceID, err := hostrelation.NewPhysicalSequenceID("pi:project:settings.packages")
	if err != nil {
		t.Fatal(err)
	}
	decision, err := reconcile.NewBlockedRelationOrderDecision(
		reconcile.BlockedRelationOrderDecisionInput{
			Target:     target.TargetPi,
			Scope:      target.ScopeProject,
			Constraint: constraint,
			SequenceID: sequenceID,
			Reason:     reconcile.OrderReasonObservationUnavailable,
			Detail:     "settings file is malformed",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	PrintRelationOrderActionsWithOptions(
		&output,
		[]reconcile.RelationOrderDecision{decision},
		HumanOptions{},
	)
	if text := output.String(); !strings.Contains(text, "blocked runtime extension precedence") ||
		!strings.Contains(text, "settings file is malformed") {
		t.Fatalf("output = %q", text)
	}
}

func presentRelationOrderDecision(
	t testing.TB,
	selectedTarget target.Target,
	meaning hostrelation.RuntimeMeaning,
) reconcile.RelationOrderDecision {
	t.Helper()
	constraint := presentOrderConstraint(t, selectedTarget, meaning)
	members := constraint.Members()
	foreignIdentity, err := hostrelation.NewHostLoadIdentity("foreign")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := observerelation.NewObservedRelationRow(foreignIdentity)
	if err != nil {
		t.Fatal(err)
	}
	rows := []observerelation.ObservedRelationRow{
		presentOrderRow(t, members[1]),
		foreign,
		presentOrderRow(t, members[0]),
	}
	classPrefix := string(selectedTarget)
	sequenceSuffix := "settings.packages"
	if selectedTarget == target.TargetOpenCode {
		sequenceSuffix = "server.plugins"
	}
	sequenceID, err := hostrelation.NewPhysicalSequenceID(
		classPrefix + ":project:" + sequenceSuffix,
	)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := observerelation.NewSequenceAuthority(
		classPrefix + ":project:" + sequenceSuffix,
	)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := observerelation.NewSequenceRevision("sha256:present")
	if err != nil {
		t.Fatal(err)
	}
	sequence, err := observerelation.NewObservedRelationSequence(
		constraint.ClassID(),
		sequenceID,
		authority,
		revision,
		rows,
	)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := reconcile.NewRelationOrderDecision(
		reconcile.RelationOrderDecisionInput{
			Target:     selectedTarget,
			Scope:      target.ScopeProject,
			Constraint: constraint,
			Sequence:   sequence,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return decision
}

func presentOrderConstraint(
	t testing.TB,
	selectedTarget target.Target,
	meaning hostrelation.RuntimeMeaning,
) hostrelation.RelationOrderConstraint {
	t.Helper()
	classID, err := hostrelation.NewOrderClassID(
		"extension:" + string(selectedTarget) + ":project:packages",
	)
	if err != nil {
		t.Fatal(err)
	}
	members := []hostrelation.RelationOrderMember{
		presentOrderMember(t, "alpha", "alpha"),
		presentOrderMember(t, "beta", "beta"),
	}
	constraint, err := hostrelation.NewRelationOrderConstraint(
		classID,
		"test-order-v1",
		meaning,
		members,
	)
	if err != nil {
		t.Fatal(err)
	}
	return constraint
}

func presentOrderMember(
	t testing.TB,
	key string,
	loadIdentityValue string,
) hostrelation.RelationOrderMember {
	t.Helper()
	subject, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"test.extension",
		key,
	)
	if err != nil {
		t.Fatal(err)
	}
	loadIdentity, err := hostrelation.NewHostLoadIdentity(loadIdentityValue)
	if err != nil {
		t.Fatal(err)
	}
	member, err := hostrelation.NewRelationOrderMember(subject, loadIdentity)
	if err != nil {
		t.Fatal(err)
	}
	return member
}

func presentOrderRow(
	t testing.TB,
	member hostrelation.RelationOrderMember,
) observerelation.ObservedRelationRow {
	t.Helper()
	row, err := observerelation.NewCorrelatedObservedRelationRow(
		member.HostLoadIdentity(),
		member.Subject(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return row
}
