package reconcile

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestRelationOrderDecisionClassifiesExactNormalizeAndCarrierConditions(t *testing.T) {
	alpha := orderTestMember(t, "alpha", "pkg:alpha")
	beta := orderTestMember(t, "beta", "pkg:beta")
	retiring := orderTestSubject(t, "retiring")
	constraint := orderTestConstraint(t, "extension:pi:project:packages", alpha, beta)

	tests := []struct {
		name             string
		rows             []observerelation.ObservedRelationRow
		installs         []topology.SubjectID
		removals         []topology.SubjectID
		wantKind         RelationOrderDecisionKind
		wantReason       RelationOrderReason
		wantForeign      int
		wantMissing      int
		wantPrecedence   int
		requiresMutation bool
	}{
		{
			name:        "exact with foreign row",
			rows:        []observerelation.ObservedRelationRow{orderTestRow(t, alpha), orderTestForeignRow(t, "foreign"), orderTestRow(t, beta)},
			wantKind:    OrderExact,
			wantForeign: 1,
		},
		{
			name:             "normalize with foreign precedence change",
			rows:             []observerelation.ObservedRelationRow{orderTestRow(t, beta), orderTestForeignRow(t, "foreign"), orderTestRow(t, alpha)},
			wantKind:         OrderNormalize,
			wantForeign:      1,
			wantPrecedence:   2,
			requiresMutation: true,
		},
		{
			name:        "conditional pending install",
			rows:        []observerelation.ObservedRelationRow{orderTestRow(t, alpha)},
			installs:    []topology.SubjectID{beta.Subject()},
			wantKind:    OrderConditionalAfterCarrierChange,
			wantReason:  OrderReasonPendingCarrierInstall,
			wantMissing: 1,
		},
		{
			name:        "conditional pending removal",
			rows:        []observerelation.ObservedRelationRow{orderTestRow(t, alpha), orderTestRow(t, beta), orderTestForeignRow(t, "retiring-load")},
			removals:    []topology.SubjectID{retiring},
			wantKind:    OrderConditionalAfterCarrierChange,
			wantReason:  OrderReasonPendingCarrierRemoval,
			wantForeign: 1,
		},
		{
			name:       "conditional correlated pending removal",
			rows:       []observerelation.ObservedRelationRow{orderTestRow(t, alpha), orderTestCorrelatedRow(t, retiring, "retiring-load"), orderTestRow(t, beta)},
			removals:   []topology.SubjectID{retiring},
			wantKind:   OrderConditionalAfterCarrierChange,
			wantReason: OrderReasonPendingCarrierRemoval,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			decision := orderTestDecision(t, RelationOrderDecisionInput{
				Target:          target.TargetPi,
				Scope:           target.ScopeProject,
				Constraint:      constraint,
				Sequence:        orderTestSequence(t, constraint, "pi:project:settings.packages", testCase.rows),
				PendingInstalls: testCase.installs,
				PendingRemovals: testCase.removals,
			})
			if decision.Kind() != testCase.wantKind || decision.Reason() != testCase.wantReason {
				t.Fatalf(
					"kind=%q reason=%q, want %q/%q",
					decision.Kind(),
					decision.Reason(),
					testCase.wantKind,
					testCase.wantReason,
				)
			}
			if decision.ForeignRowCount() != testCase.wantForeign ||
				len(decision.MissingMembers()) != testCase.wantMissing ||
				len(decision.PrecedenceChanges()) != testCase.wantPrecedence {
				t.Fatalf(
					"foreign=%d missing=%d precedence=%d",
					decision.ForeignRowCount(),
					len(decision.MissingMembers()),
					len(decision.PrecedenceChanges()),
				)
			}
			if decision.RequiresMutation() != testCase.requiresMutation {
				t.Fatalf("RequiresMutation=%t, want %t", decision.RequiresMutation(), testCase.requiresMutation)
			}
			if decision.BlocksOrdinaryApply() {
				t.Fatal("nominal or conditional decision blocked ordinary apply")
			}
			if err := decision.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
}

func TestResultReportsEveryNonExactRelationOrder(t *testing.T) {
	alpha := orderTestMember(t, "alpha", "pkg:alpha")
	beta := orderTestMember(t, "beta", "pkg:beta")
	constraint := orderTestConstraint(t, "extension:pi:project:packages", alpha, beta)
	tests := []struct {
		name     string
		rows     []observerelation.ObservedRelationRow
		installs []topology.SubjectID
	}{
		{
			name: "exact",
			rows: []observerelation.ObservedRelationRow{
				orderTestRow(t, alpha),
				orderTestRow(t, beta),
			},
		},
		{
			name: "normalize",
			rows: []observerelation.ObservedRelationRow{
				orderTestRow(t, beta),
				orderTestRow(t, alpha),
			},
		},
		{
			name:     "conditional",
			rows:     []observerelation.ObservedRelationRow{orderTestRow(t, alpha)},
			installs: []topology.SubjectID{beta.Subject()},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := orderTestDecision(t, RelationOrderDecisionInput{
				Target:     target.TargetPi,
				Scope:      target.ScopeProject,
				Constraint: constraint,
				Sequence: orderTestSequence(
					t,
					constraint,
					"pi:project:settings.packages",
					test.rows,
				),
				PendingInstalls: test.installs,
			})
			result, err := NewResult(ResultInput{
				Context:        ContextInspect,
				RelationOrders: []RelationOrderDecision{decision},
			})
			if err != nil {
				t.Fatal(err)
			}
			wantNonExact := test.name != "exact"
			if result.HasNonExactRelationOrders() != wantNonExact {
				t.Fatalf(
					"HasNonExactRelationOrders = %t, want %t",
					result.HasNonExactRelationOrders(),
					wantNonExact,
				)
			}
		})
	}
}

func TestRelationOrderDecisionBlocksUnexplainedMembershipAndIdentityDrift(t *testing.T) {
	alpha := orderTestMember(t, "alpha", "pkg:alpha")
	beta := orderTestMember(t, "beta", "pkg:beta")
	constraint := orderTestConstraint(t, "extension:pi:project:packages", alpha, beta)
	unexpected := orderTestSubject(t, "unexpected")

	tests := []struct {
		name       string
		rows       []observerelation.ObservedRelationRow
		installs   []topology.SubjectID
		removals   []topology.SubjectID
		wantReason RelationOrderReason
	}{
		{
			name:       "missing without install",
			rows:       []observerelation.ObservedRelationRow{orderTestRow(t, alpha)},
			wantReason: OrderReasonMembershipMismatch,
		},
		{
			name:       "extra without removal",
			rows:       []observerelation.ObservedRelationRow{orderTestRow(t, alpha), orderTestCorrelatedRow(t, unexpected, "unexpected"), orderTestRow(t, beta)},
			wantReason: OrderReasonMembershipMismatch,
		},
		{
			name:       "managed load identity drift",
			rows:       []observerelation.ObservedRelationRow{orderTestCorrelatedRow(t, alpha.Subject(), "wrong"), orderTestRow(t, beta)},
			wantReason: OrderReasonLoadIdentityMismatch,
		},
		{
			name:       "install outside desired membership",
			rows:       []observerelation.ObservedRelationRow{orderTestRow(t, alpha), orderTestRow(t, beta)},
			installs:   []topology.SubjectID{unexpected},
			wantReason: OrderReasonConflictingCarrierPlan,
		},
		{
			name:       "remove desired member",
			rows:       []observerelation.ObservedRelationRow{orderTestRow(t, alpha), orderTestRow(t, beta)},
			removals:   []topology.SubjectID{beta.Subject()},
			wantReason: OrderReasonConflictingCarrierPlan,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			decision := orderTestDecision(t, RelationOrderDecisionInput{
				Target:          target.TargetPi,
				Scope:           target.ScopeProject,
				Constraint:      constraint,
				Sequence:        orderTestSequence(t, constraint, "pi:project:settings.packages", testCase.rows),
				PendingInstalls: testCase.installs,
				PendingRemovals: testCase.removals,
			})
			if decision.Kind() != OrderBlocked || decision.Reason() != testCase.wantReason {
				t.Fatalf("kind=%q reason=%q, want blocked/%q", decision.Kind(), decision.Reason(), testCase.wantReason)
			}
			if !decision.BlocksOrdinaryApply() || decision.RequiresMutation() {
				t.Fatalf(
					"BlocksOrdinaryApply=%t RequiresMutation=%t",
					decision.BlocksOrdinaryApply(),
					decision.RequiresMutation(),
				)
			}
		})
	}
}

func TestRelationOrderDecisionRejectsConflictingOrDuplicateCarrierFacts(t *testing.T) {
	alpha := orderTestMember(t, "alpha", "pkg:alpha")
	beta := orderTestMember(t, "beta", "pkg:beta")
	constraint := orderTestConstraint(t, "extension:pi:project:packages", alpha, beta)
	sequence := orderTestSequence(
		t,
		constraint,
		"pi:project:settings.packages",
		[]observerelation.ObservedRelationRow{orderTestRow(t, alpha), orderTestRow(t, beta)},
	)

	tests := []struct {
		name     string
		installs []topology.SubjectID
		removals []topology.SubjectID
		want     string
	}{
		{
			name:     "duplicate install",
			installs: []topology.SubjectID{alpha.Subject(), alpha.Subject()},
			want:     "appears more than once",
		},
		{
			name:     "same subject install and removal",
			installs: []topology.SubjectID{alpha.Subject()},
			removals: []topology.SubjectID{alpha.Subject()},
			want:     "both install and removal",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := NewRelationOrderDecision(RelationOrderDecisionInput{
				Target:          target.TargetPi,
				Scope:           target.ScopeProject,
				Constraint:      constraint,
				Sequence:        sequence,
				PendingInstalls: testCase.installs,
				PendingRemovals: testCase.removals,
			})
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error=%v, want %q", err, testCase.want)
			}
		})
	}
}

func TestBlockedRelationOrderDecisionCarriesNoFabricatedRevision(t *testing.T) {
	alpha := orderTestMember(t, "alpha", "pkg:alpha")
	beta := orderTestMember(t, "beta", "pkg:beta")
	constraint := orderTestConstraint(t, "extension:pi:project:packages", alpha, beta)
	sequenceID, err := hostrelation.NewPhysicalSequenceID("pi:project:settings.packages")
	if err != nil {
		t.Fatalf("NewPhysicalSequenceID: %v", err)
	}
	decision, err := NewBlockedRelationOrderDecision(BlockedRelationOrderDecisionInput{
		Target:     target.TargetPi,
		Scope:      target.ScopeProject,
		Constraint: constraint,
		SequenceID: sequenceID,
		Reason:     OrderReasonObservationUnavailable,
		Detail:     "settings file is malformed",
	})
	if err != nil {
		t.Fatalf("NewBlockedRelationOrderDecision: %v", err)
	}
	if !decision.BlocksOrdinaryApply() ||
		decision.HasCurrentSequence() ||
		decision.Authority() != "" ||
		decision.Revision() != "" ||
		decision.Detail() != "settings file is malformed" {
		t.Fatalf("blocked decision=%#v", decision)
	}
	if err := decision.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestRelationOrderObservationFailureReasonPreservesResourceLimits(t *testing.T) {
	limitErr := fmt.Errorf(
		"observe Pi order: %w",
		observerelation.ErrOrderLimitExceeded,
	)
	if got := RelationOrderObservationFailureReason(limitErr); got != OrderReasonResourceLimitExceeded {
		t.Fatalf("resource limit reason = %q", got)
	}
	if got := RelationOrderObservationFailureReason(errors.New("settings malformed")); got != OrderReasonObservationUnavailable {
		t.Fatalf("generic observation reason = %q", got)
	}
}

func TestBlockedRelationOrderDecisionAcceptsResourceLimitReason(t *testing.T) {
	alpha := orderTestMember(t, "alpha", "pkg:alpha")
	beta := orderTestMember(t, "beta", "pkg:beta")
	constraint := orderTestConstraint(t, "extension:pi:project:packages", alpha, beta)
	sequenceID, err := hostrelation.NewPhysicalSequenceID("pi:project:settings.packages")
	if err != nil {
		t.Fatal(err)
	}
	decision, err := NewBlockedRelationOrderDecision(BlockedRelationOrderDecisionInput{
		Target:     target.TargetPi,
		Scope:      target.ScopeProject,
		Constraint: constraint,
		SequenceID: sequenceID,
		Reason:     OrderReasonResourceLimitExceeded,
		Detail:     "extension order resource limit exceeded: observed_rows observed=4097 limit=4096",
	})
	if err != nil {
		t.Fatalf("NewBlockedRelationOrderDecision: %v", err)
	}
	if decision.Reason() != OrderReasonResourceLimitExceeded ||
		!decision.BlocksOrdinaryApply() ||
		decision.HasCurrentSequence() {
		t.Fatalf("resource-limit decision = %#v", decision)
	}
	if err := decision.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestResultOrdersRelationOrderDecisionsByClassAndSequence(t *testing.T) {
	alpha := orderTestMember(t, "alpha", "pkg:alpha")
	beta := orderTestMember(t, "beta", "pkg:beta")
	constraint := orderTestConstraint(t, "extension:opencode:project:plugins", alpha, beta)
	tui := orderTestDecision(t, RelationOrderDecisionInput{
		Target:     target.TargetOpenCode,
		Scope:      target.ScopeProject,
		Constraint: constraint,
		Sequence: orderTestSequence(
			t,
			constraint,
			"opencode:project:tui.plugins",
			[]observerelation.ObservedRelationRow{orderTestRow(t, alpha), orderTestRow(t, beta)},
		),
	})
	server := orderTestDecision(t, RelationOrderDecisionInput{
		Target:     target.TargetOpenCode,
		Scope:      target.ScopeProject,
		Constraint: constraint,
		Sequence: orderTestSequence(
			t,
			constraint,
			"opencode:project:server.plugins",
			[]observerelation.ObservedRelationRow{orderTestRow(t, alpha), orderTestRow(t, beta)},
		),
	})

	result, err := NewResult(ResultInput{
		Context:        ContextInspect,
		RelationOrders: []RelationOrderDecision{tui, server},
	})
	if err != nil {
		t.Fatalf("NewResult: %v", err)
	}
	got := result.RelationOrders()
	if len(got) != 2 ||
		got[0].SequenceID() != "opencode:project:server.plugins" ||
		got[1].SequenceID() != "opencode:project:tui.plugins" {
		t.Fatalf("relation orders=%#v, want server then TUI", got)
	}
	cloned := result.Clone().RelationOrders()
	cloned[0] = RelationOrderDecision{}
	if result.RelationOrders()[0].SequenceID() == "" {
		t.Fatal("caller mutation changed result relation orders")
	}
	if result.DecisionCount() != 2 {
		t.Fatalf("DecisionCount=%d, want 2", result.DecisionCount())
	}
	if result.HasBlockedRelationOrders() || result.HasErrors() {
		t.Fatal("exact relation order decisions were classified as errors")
	}
	if _, err := NewResult(ResultInput{
		Context:        ContextInspect,
		RelationOrders: []RelationOrderDecision{server, server},
	}); err == nil || !strings.Contains(err.Error(), "duplicate relation order decision") {
		t.Fatalf("duplicate relation order error=%v", err)
	}
}

func TestResultRejectsRelationOrderSequenceClaimedByDifferentClasses(t *testing.T) {
	alpha := orderTestMember(t, "alpha", "pkg:alpha")
	beta := orderTestMember(t, "beta", "pkg:beta")
	firstConstraint := orderTestConstraint(t, "extension:opencode:project:plugins", alpha, beta)
	secondConstraint := orderTestConstraint(t, "extension:pi:project:packages", alpha, beta)
	sequenceID := "shared:project:extensions"
	first := orderTestDecision(t, RelationOrderDecisionInput{
		Target:     target.TargetOpenCode,
		Scope:      target.ScopeProject,
		Constraint: firstConstraint,
		Sequence: orderTestSequence(
			t,
			firstConstraint,
			sequenceID,
			[]observerelation.ObservedRelationRow{orderTestRow(t, alpha), orderTestRow(t, beta)},
		),
	})
	second := orderTestDecision(t, RelationOrderDecisionInput{
		Target:     target.TargetPi,
		Scope:      target.ScopeProject,
		Constraint: secondConstraint,
		Sequence: orderTestSequence(
			t,
			secondConstraint,
			sequenceID,
			[]observerelation.ObservedRelationRow{orderTestRow(t, alpha), orderTestRow(t, beta)},
		),
	})

	_, err := NewResult(ResultInput{
		Context:        ContextInspect,
		RelationOrders: []RelationOrderDecision{first, second},
	})
	if err == nil || !strings.Contains(err.Error(), "is shared by classes") {
		t.Fatalf("shared physical sequence error=%v", err)
	}
}

func TestResultRejectsInconsistentSequenceDecisionsForOneClass(t *testing.T) {
	alpha := orderTestMember(t, "alpha", "pkg:alpha")
	beta := orderTestMember(t, "beta", "pkg:beta")
	constraint := orderTestConstraint(t, "extension:opencode:project:plugins", alpha, beta)
	first := orderTestDecision(t, RelationOrderDecisionInput{
		Target:     target.TargetOpenCode,
		Scope:      target.ScopeProject,
		Constraint: constraint,
		Sequence: orderTestSequence(
			t,
			constraint,
			"opencode:project:server.plugins",
			[]observerelation.ObservedRelationRow{orderTestRow(t, alpha), orderTestRow(t, beta)},
		),
	})
	second := orderTestDecision(t, RelationOrderDecisionInput{
		Target:     target.TargetOpenCode,
		Scope:      target.ScopeGlobal,
		Constraint: constraint,
		Sequence: orderTestSequence(
			t,
			constraint,
			"opencode:global:tui.plugins",
			[]observerelation.ObservedRelationRow{orderTestRow(t, alpha), orderTestRow(t, beta)},
		),
	})

	_, err := NewResult(ResultInput{
		Context:        ContextInspect,
		RelationOrders: []RelationOrderDecision{first, second},
	})
	if err == nil || !strings.Contains(err.Error(), "has inconsistent sequence decisions") {
		t.Fatalf("inconsistent class decision error=%v", err)
	}
}

func TestResultSurfacesFirstBlockedRelationOrder(t *testing.T) {
	alpha := orderTestMember(t, "alpha", "pkg:alpha")
	beta := orderTestMember(t, "beta", "pkg:beta")
	constraint := orderTestConstraint(t, "extension:pi:project:packages", alpha, beta)
	sequenceID, err := hostrelation.NewPhysicalSequenceID("pi:project:settings.packages")
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := NewBlockedRelationOrderDecision(BlockedRelationOrderDecisionInput{
		Target:     target.TargetPi,
		Scope:      target.ScopeProject,
		Constraint: constraint,
		SequenceID: sequenceID,
		Reason:     OrderReasonObservationUnavailable,
		Detail:     "malformed settings",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewResult(ResultInput{
		Context:        ContextInspect,
		RelationOrders: []RelationOrderDecision{blocked},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, found := result.FirstBlockedRelationOrder()
	if !found ||
		first.SequenceID() != sequenceID ||
		!result.HasBlockedRelationOrders() ||
		!result.HasErrors() {
		t.Fatalf("blocked result = %#v found=%t", result, found)
	}
}

func orderTestDecision(t testing.TB, input RelationOrderDecisionInput) RelationOrderDecision {
	t.Helper()
	decision, err := NewRelationOrderDecision(input)
	if err != nil {
		t.Fatalf("NewRelationOrderDecision: %v", err)
	}
	return decision
}

func orderTestConstraint(
	t testing.TB,
	classValue string,
	members ...hostrelation.RelationOrderMember,
) hostrelation.RelationOrderConstraint {
	t.Helper()
	classID, err := hostrelation.NewOrderClassID(classValue)
	if err != nil {
		t.Fatalf("NewOrderClassID: %v", err)
	}
	constraint, err := hostrelation.NewRelationOrderConstraint(
		classID,
		"test-load-identity:v1",
		hostrelation.RuntimePrecedence,
		members,
	)
	if err != nil {
		t.Fatalf("NewRelationOrderConstraint: %v", err)
	}
	return constraint
}

func orderTestMember(
	t testing.TB,
	key string,
	loadIdentity string,
) hostrelation.RelationOrderMember {
	t.Helper()
	subject := orderTestSubject(t, key)
	identity, err := hostrelation.NewHostLoadIdentity(loadIdentity)
	if err != nil {
		t.Fatalf("NewHostLoadIdentity: %v", err)
	}
	member, err := hostrelation.NewRelationOrderMember(subject, identity)
	if err != nil {
		t.Fatalf("NewRelationOrderMember: %v", err)
	}
	return member
}

func orderTestSubject(t testing.TB, key string) topology.SubjectID {
	t.Helper()
	subject, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"test.extension-relation",
		key,
	)
	if err != nil {
		t.Fatalf("NewSubjectID: %v", err)
	}
	return subject
}

func orderTestRow(
	t testing.TB,
	member hostrelation.RelationOrderMember,
) observerelation.ObservedRelationRow {
	t.Helper()
	return orderTestCorrelatedRow(t, member.Subject(), string(member.HostLoadIdentity()))
}

func orderTestCorrelatedRow(
	t testing.TB,
	subject topology.SubjectID,
	loadIdentity string,
) observerelation.ObservedRelationRow {
	t.Helper()
	identity, err := hostrelation.NewHostLoadIdentity(loadIdentity)
	if err != nil {
		t.Fatalf("NewHostLoadIdentity: %v", err)
	}
	row, err := observerelation.NewCorrelatedObservedRelationRow(identity, subject)
	if err != nil {
		t.Fatalf("NewCorrelatedObservedRelationRow: %v", err)
	}
	return row
}

func orderTestForeignRow(
	t testing.TB,
	loadIdentity string,
) observerelation.ObservedRelationRow {
	t.Helper()
	identity, err := hostrelation.NewHostLoadIdentity(loadIdentity)
	if err != nil {
		t.Fatalf("NewHostLoadIdentity: %v", err)
	}
	row, err := observerelation.NewObservedRelationRow(identity)
	if err != nil {
		t.Fatalf("NewObservedRelationRow: %v", err)
	}
	return row
}

func orderTestSequence(
	t testing.TB,
	constraint hostrelation.RelationOrderConstraint,
	sequenceValue string,
	rows []observerelation.ObservedRelationRow,
) observerelation.ObservedRelationSequence {
	t.Helper()
	sequenceID, err := hostrelation.NewPhysicalSequenceID(sequenceValue)
	if err != nil {
		t.Fatalf("NewPhysicalSequenceID: %v", err)
	}
	authority, err := observerelation.NewSequenceAuthority("test-authority:" + sequenceValue)
	if err != nil {
		t.Fatalf("NewSequenceAuthority: %v", err)
	}
	revision, err := observerelation.NewSequenceRevision("sha256:test-revision")
	if err != nil {
		t.Fatalf("NewSequenceRevision: %v", err)
	}
	sequence, err := observerelation.NewObservedRelationSequence(
		constraint.ClassID(),
		sequenceID,
		authority,
		revision,
		rows,
	)
	if err != nil {
		t.Fatalf("NewObservedRelationSequence: %v", err)
	}
	return sequence
}

func TestRelationOrderDecisionAccessorsDefensivelyCopy(t *testing.T) {
	alpha := orderTestMember(t, "alpha", "pkg:alpha")
	beta := orderTestMember(t, "beta", "pkg:beta")
	constraint := orderTestConstraint(t, "extension:pi:project:packages", alpha, beta)
	decision := orderTestDecision(t, RelationOrderDecisionInput{
		Target:     target.TargetPi,
		Scope:      target.ScopeProject,
		Constraint: constraint,
		Sequence: orderTestSequence(
			t,
			constraint,
			"pi:project:settings.packages",
			[]observerelation.ObservedRelationRow{orderTestRow(t, beta), orderTestForeignRow(t, "foreign"), orderTestRow(t, alpha)},
		),
	})

	desired := decision.DesiredMembers()
	observed := decision.ObservedMembers()
	risks := decision.PrecedenceChanges()
	desired[0] = hostrelation.RelationOrderMember{}
	observed[0] = hostrelation.RelationOrderMember{}
	risks[0] = observerelation.PrecedenceChange{}
	if slices.Contains(decision.DesiredMembers(), hostrelation.RelationOrderMember{}) ||
		slices.Contains(decision.ObservedMembers(), hostrelation.RelationOrderMember{}) ||
		slices.Contains(decision.PrecedenceChanges(), observerelation.PrecedenceChange{}) {
		t.Fatal("caller mutation changed relation order decision")
	}
}
