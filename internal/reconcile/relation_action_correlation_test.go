package reconcile_test

import (
	"strings"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	reconcile "github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestResultAuthorizedExactCorrelationProducesNoOpWithoutHostInvocation(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	action := mustPlan(t, subject, correlationFor(t, subject, observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observerelation.Row{
			mustManagedRow(t, "context7", "managed/context7"),
		},
	}), blockedAdmission(t))

	assertAction(t, action, reconcile.ActionNoOp, reconcile.ExecutionNoMutation, reconcile.ReasonNone)
	assertEvidence(t, action, observerelation.InventorySupported, observerelation.EvidenceFresh)
	if action.InvokesHostRoute() {
		t.Fatalf("exact no-op must not invoke host route")
	}
}

func TestResultExactCorrelationWithoutManagementAuthorityBlocks(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	input := validInput(t, subject, correlationFor(t, subject, observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observerelation.Row{
			mustManagedRow(t, "context7", "managed/context7"),
		},
	}), hostDelegatedAdmission(t))
	input.ManagedClaimPresent = false

	action, err := reconcile.NewRelationAction(input)
	if err != nil {
		t.Fatal(err)
	}
	assertAction(
		t,
		action,
		reconcile.ActionBlock,
		reconcile.ExecutionBlocked,
		reconcile.ReasonPresentUnclaimed,
	)
	if !action.BlocksOrdinaryApply() || action.InvokesHostRoute() {
		t.Fatalf("unclaimed exact correlation must block without invoking a host route")
	}
}

func TestResultBoundsAntigravityUnkeyedEvidenceToSelectorSources(t *testing.T) {
	subjectID, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"antigravity-cli.plugin-carrier",
		"guidance",
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		source string
		want   reconcile.RelationActionKind
	}{
		{
			name:   "selector source with pending authority",
			source: "guidance@google",
			want:   reconcile.ActionNoOp,
		},
		{
			name:   "opaque source with pending authority",
			source: "guidance",
			want:   reconcile.ActionBlock,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			identity := mustCarrierIdentity(
				t,
				subjectID,
				target.TargetAntigravityCLI,
				target.ScopeGlobal,
				test.source,
				"guidance",
			)
			correlation := correlationFor(
				t,
				identity.ExpectedRelation(),
				observerelation.InventorySpec{
					Availability: observerelation.InventorySupported,
					Freshness:    observerelation.EvidenceFresh,
					Rows: []observerelation.Row{
						mustUnmanagedRow(t, "guidance"),
					},
				},
			)
			input := validInput(t, identity.ExpectedRelation(), correlation, hostDelegatedAdmission(t))
			input.CarrierIdentity = identity
			input.PendingInstallPresent = true
			input.ManagedClaimPresent = false

			action, err := reconcile.NewRelationAction(input)
			if err != nil {
				t.Fatal(err)
			}
			if action.Kind() != test.want {
				t.Fatalf("action kind = %q, want %q", action.Kind(), test.want)
			}
			if action.CorrelationState() != observerelation.StateUnkeyedSameSubject {
				t.Fatalf("correlation state = %q, want unkeyed evidence", action.CorrelationState())
			}
		})
	}
}

func TestResultRejectsCrossWiredAntigravityUnkeyedEvidence(t *testing.T) {
	subjectID, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"antigravity-cli.plugin-carrier",
		"guidance",
	)
	if err != nil {
		t.Fatal(err)
	}
	identity := mustCarrierIdentity(
		t,
		subjectID,
		target.TargetAntigravityCLI,
		target.ScopeGlobal,
		"guidance@google",
		"guidance",
	)
	other := mustSubject(t, "other", "managed/other")
	correlation := correlationFor(t, other, observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observerelation.Row{
			mustUnmanagedRow(t, "other"),
		},
	})
	input := validInput(t, identity.ExpectedRelation(), correlation, hostDelegatedAdmission(t))
	input.CarrierIdentity = identity
	input.PendingInstallPresent = true
	input.ManagedClaimPresent = false

	_, err = reconcile.NewRelationAction(input)
	if err == nil || !strings.Contains(err.Error(), "does not match locked relation subject key") {
		t.Fatalf("NewRelationAction error = %v, want cross-wired unkeyed evidence rejection", err)
	}
}

func TestResultBlocksStaleEvidenceBeforeExactLookingRows(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	action := mustPlan(t, subject, correlationFor(t, subject, observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceStale,
		Rows: []observerelation.Row{
			mustManagedRow(t, "context7", "managed/context7"),
		},
	}), ordinaryAdmission(t))

	assertAction(t, action, reconcile.ActionBlock, reconcile.ExecutionBlocked, reconcile.ReasonStaleEvidence)
	assertEvidence(t, action, observerelation.InventorySupported, observerelation.EvidenceStale)
	assertWatchpoints(t, action, []observerelation.Watchpoint{observerelation.WatchpointFreshInventoryRequired})
	if action.InvokesHostRoute() {
		t.Fatalf("stale evidence must not invoke host route")
	}
}

func TestResultBlocksUnmanagedShadowDriftAndAmbiguity(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	tests := []struct {
		name   string
		rows   []observerelation.Row
		reason reconcile.RelationReasonCode
	}{
		{
			name: "unmanaged same subject",
			rows: []observerelation.Row{
				mustUnmanagedRow(t, "context7"),
			},
			reason: reconcile.ReasonUnkeyedSameSubject,
		},
		{
			name: "same subject shadow",
			rows: []observerelation.Row{
				mustManagedRow(t, "context7", "managed/other"),
			},
			reason: reconcile.ReasonSameSubjectShadow,
		},
		{
			name: "managed key drift",
			rows: []observerelation.Row{
				mustManagedRow(
					t,
					"renamed-context7",
					string(subject.ManagedInstanceKey()),
				),
			},
			reason: reconcile.ReasonManagedKeyDrift,
		},
		{
			name: "ambiguous managed key",
			rows: []observerelation.Row{
				mustManagedRow(t, "context7", "managed/context7"),
				mustManagedRow(
					t,
					"context7-copy",
					string(subject.ManagedInstanceKey()),
				),
			},
			reason: reconcile.ReasonAmbiguousRelation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := mustPlan(t, subject, correlationFor(t, subject, observerelation.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceFresh,
				Rows:         tt.rows,
			}), ordinaryAdmission(t))
			assertAction(t, action, reconcile.ActionBlock, reconcile.ExecutionBlocked, tt.reason)
			if action.InvokesHostRoute() {
				t.Fatalf("blocked relation conflict must not invoke host route")
			}
		})
	}
}
