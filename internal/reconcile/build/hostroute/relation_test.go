package hostroute

import (
	"path/filepath"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired/extension"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/realization/profile"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestBuildCarrierRelationActionsClassifiesPassiveCorrelationMatrix(t *testing.T) {
	locked, subject := statusClaudePluginExtensionLockfile(t, "context7", "context7@market")
	_, other := statusClaudePluginExtensionLockfile(t, "other", "other@market")
	tests := []struct {
		name          string
		inventory     observeclaudeplugin.Inventory
		wantKind      reconciliation.RelationActionKind
		wantExecution reconciliation.RelationExecutionClass
		wantReason    reconciliation.RelationReasonCode
		wantState     observerelation.CorrelationState
		wantBlocks    bool
		wantInvokes   bool
	}{
		{
			name:          "exact external relation blocks without management authority",
			inventory:     mustStatusClaudePluginInventory(t, observeclaudeplugin.InventorySpec{Availability: observerelation.InventorySupported, Freshness: observerelation.EvidenceFresh, Rows: []observeclaudeplugin.Row{mustStatusManagedRow(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey()))}}),
			wantKind:      reconciliation.ActionBlock,
			wantExecution: reconciliation.ExecutionBlocked,
			wantReason:    reconciliation.ReasonPresentUnclaimed,
			wantState:     observerelation.StateExactCorrelation,
			wantBlocks:    true,
		},
		{
			name:          "missing relation is host-delegated create",
			inventory:     mustStatusClaudePluginInventory(t, observeclaudeplugin.InventorySpec{Availability: observerelation.InventorySupported, Freshness: observerelation.EvidenceFresh}),
			wantKind:      reconciliation.ActionCreate,
			wantExecution: reconciliation.ExecutionHostRoute,
			wantReason:    reconciliation.ReasonNone,
			wantState:     observerelation.StateMissing,
			wantInvokes:   true,
		},
		{
			name:          "unkeyed same subject blocks adoption by default",
			inventory:     mustStatusClaudePluginInventory(t, observeclaudeplugin.InventorySpec{Availability: observerelation.InventorySupported, Freshness: observerelation.EvidenceFresh, Rows: []observeclaudeplugin.Row{mustStatusUnmanagedRow(t, "context7@market")}}),
			wantKind:      reconciliation.ActionBlock,
			wantExecution: reconciliation.ExecutionBlocked,
			wantReason:    reconciliation.ReasonUnkeyedSameSubject,
			wantState:     observerelation.StateUnkeyedSameSubject,
			wantBlocks:    true,
		},
		{
			name:          "same name shadow blocks replacement",
			inventory:     mustStatusClaudePluginInventory(t, observeclaudeplugin.InventorySpec{Availability: observerelation.InventorySupported, Freshness: observerelation.EvidenceFresh, Rows: []observeclaudeplugin.Row{mustStatusManagedRow(t, "context7@market", string(other.ExpectedRelation().ManagedInstanceKey()))}}),
			wantKind:      reconciliation.ActionBlock,
			wantExecution: reconciliation.ExecutionBlocked,
			wantReason:    reconciliation.ReasonSameSubjectShadow,
			wantState:     observerelation.StateSameSubjectShadow,
			wantBlocks:    true,
		},
		{
			name:          "managed key drift blocks rename",
			inventory:     mustStatusClaudePluginInventory(t, observeclaudeplugin.InventorySpec{Availability: observerelation.InventorySupported, Freshness: observerelation.EvidenceFresh, Rows: []observeclaudeplugin.Row{mustStatusManagedRow(t, "renamed-context7", string(subject.ExpectedRelation().ManagedInstanceKey()))}}),
			wantKind:      reconciliation.ActionBlock,
			wantExecution: reconciliation.ExecutionBlocked,
			wantReason:    reconciliation.ReasonManagedKeyDrift,
			wantState:     observerelation.StateManagedKeyDrift,
			wantBlocks:    true,
		},
		{
			name:          "ambiguous managed key blocks",
			inventory:     mustStatusClaudePluginInventory(t, observeclaudeplugin.InventorySpec{Availability: observerelation.InventorySupported, Freshness: observerelation.EvidenceFresh, Rows: []observeclaudeplugin.Row{mustStatusManagedRow(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey())), mustStatusManagedRow(t, "context7-copy", string(subject.ExpectedRelation().ManagedInstanceKey()))}}),
			wantKind:      reconciliation.ActionBlock,
			wantExecution: reconciliation.ExecutionBlocked,
			wantReason:    reconciliation.ReasonAmbiguousRelation,
			wantState:     observerelation.StateAmbiguous,
			wantBlocks:    true,
		},
		{
			name:          "stale evidence blocks before exact-looking rows",
			inventory:     mustStatusClaudePluginInventory(t, observeclaudeplugin.InventorySpec{Availability: observerelation.InventorySupported, Freshness: observerelation.EvidenceStale, Rows: []observeclaudeplugin.Row{mustStatusManagedRow(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey()))}}),
			wantKind:      reconciliation.ActionBlock,
			wantExecution: reconciliation.ExecutionBlocked,
			wantReason:    reconciliation.ReasonStaleEvidence,
			wantState:     observerelation.StateStaleEvidence,
			wantBlocks:    true,
		},
		{
			name: "unsupported passive inventory is observe-only",
			inventory: mustStatusClaudePluginInventory(t, observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventoryUnsupported,
				Freshness:    observerelation.EvidenceFresh,
			}),
			wantKind:      reconciliation.ActionObserveOnly,
			wantExecution: reconciliation.ExecutionObserveOnly,
			wantReason:    reconciliation.ReasonUnsupportedPassiveInventory,
			wantState:     observerelation.StateUnsupported,
		},
		{
			name: "unavailable relation evidence is observe-only",
			inventory: mustStatusClaudePluginInventory(t, observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventoryUnavailable,
				Freshness:    observerelation.EvidenceFresh,
			}),
			wantKind:      reconciliation.ActionObserveOnly,
			wantExecution: reconciliation.ExecutionObserveOnly,
			wantReason:    reconciliation.ReasonRelationEvidenceUnavailable,
			wantState:     observerelation.StateUnavailableEvidence,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actions, err := buildRelationActions(
				t,
				locked,
				statusClaudeSelection(t, "claude-code"),
				mustStatusClaudeObservationBatch(t, locked.Locked.Subjects()[0].SubjectID(), subject, tt.inventory),
			)
			if err != nil {
				t.Fatalf("BuildRelationActions returned error: %v", err)
			}
			if len(actions) != 1 {
				t.Fatalf("actions = %#v, want one", actions)
			}
			action := actions[0]
			if action.Kind() != tt.wantKind ||
				action.Execution() != tt.wantExecution ||
				action.Reason() != tt.wantReason ||
				action.CorrelationState() != tt.wantState ||
				action.BlocksOrdinaryApply() != tt.wantBlocks ||
				action.InvokesHostRoute() != tt.wantInvokes {
				t.Fatalf(
					"action = kind %q execution %q reason %q state %q blocks %t invokes %t",
					action.Kind(),
					action.Execution(),
					action.Reason(),
					action.CorrelationState(),
					action.BlocksOrdinaryApply(),
					action.InvokesHostRoute(),
				)
			}
		})
	}
}

func TestBuildCarrierRelationActionsKeepsManagementFactsSeparateFromExactEvidence(t *testing.T) {
	locked, subject := statusClaudePluginExtensionLockfile(t, "context7", "context7@market")
	observation := mustStatusClaudeObservationBatch(
		t,
		locked.Locked.Subjects()[0].SubjectID(),
		subject,
		mustStatusClaudePluginInventory(t, observeclaudeplugin.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
			Rows: []observeclaudeplugin.Row{
				mustStatusManagedRow(
					t,
					"context7@market",
					string(subject.ExpectedRelation().ManagedInstanceKey()),
				),
			},
		}),
	)
	pending, claim, owner := relationManagementFacts(t, locked)

	tests := []struct {
		name    string
		pending []durablecarrier.PendingCarrierInstall
		claims  []durablecarrier.ManagedCarrierClaim
	}{
		{name: "matching pending install", pending: []durablecarrier.PendingCarrierInstall{pending}},
		{name: "matching durable claim", claims: []durablecarrier.ManagedCarrierClaim{claim}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actions, err := BuildRelationActions(RelationInput{
				Locked:          locked,
				SelectedTargets: statusClaudeSelection(t, "claude-code"),
				Observations:    observation,
				CurrentOwner:    owner,
				PendingInstalls: test.pending,
				ManagedClaims:   test.claims,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(actions) != 1 ||
				actions[0].Kind() != reconciliation.ActionNoOp ||
				actions[0].Reason() != reconciliation.ReasonNone {
				t.Fatalf("actions = %#v, want one authorized exact no-op", actions)
			}
		})
	}
}

func TestBuildCarrierRelationActionsRejectsForeignOwnerClaimForExactRelation(t *testing.T) {
	locked, subject := statusClaudePluginExtensionLockfile(t, "context7", "context7@market")
	observation := mustStatusClaudeObservationBatch(
		t,
		locked.Locked.Subjects()[0].SubjectID(),
		subject,
		mustStatusClaudePluginInventory(t, observeclaudeplugin.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
			Rows: []observeclaudeplugin.Row{
				mustStatusManagedRow(
					t,
					"context7@market",
					string(subject.ExpectedRelation().ManagedInstanceKey()),
				),
			},
		}),
	)
	_, foreignClaim, _ := relationManagementFacts(t, locked)
	currentOwner := relationTestOwner(t)

	actions, err := BuildRelationActions(RelationInput{
		Locked:          locked,
		SelectedTargets: statusClaudeSelection(t, "claude-code"),
		Observations:    observation,
		CurrentOwner:    currentOwner,
		ManagedClaims:   []durablecarrier.ManagedCarrierClaim{foreignClaim},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 ||
		actions[0].Kind() != reconciliation.ActionBlock ||
		actions[0].Reason() != reconciliation.ReasonPresentUnclaimed {
		t.Fatalf("actions = %#v, want foreign claim to leave selected relation unclaimed", actions)
	}
}

func TestBuildCarrierRelationActionsRejectsNonmatchingManagementAuthority(t *testing.T) {
	locked, subject := statusClaudePluginExtensionLockfile(t, "context7", "context7@market")
	otherLocked, _ := statusClaudePluginExtensionLockfile(t, "other", "other@market")
	otherPending, otherClaim, owner := relationManagementFacts(t, otherLocked)
	observation := mustStatusClaudeObservationBatch(
		t,
		locked.Locked.Subjects()[0].SubjectID(),
		subject,
		mustStatusClaudePluginInventory(t, observeclaudeplugin.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
			Rows: []observeclaudeplugin.Row{
				mustStatusManagedRow(
					t,
					"context7@market",
					string(subject.ExpectedRelation().ManagedInstanceKey()),
				),
			},
		}),
	)

	actions, err := BuildRelationActions(RelationInput{
		Locked:          locked,
		SelectedTargets: statusClaudeSelection(t, "claude-code"),
		Observations:    observation,
		CurrentOwner:    owner,
		PendingInstalls: []durablecarrier.PendingCarrierInstall{otherPending},
		ManagedClaims:   []durablecarrier.ManagedCarrierClaim{otherClaim},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 ||
		actions[0].Kind() != reconciliation.ActionBlock ||
		actions[0].Reason() != reconciliation.ReasonPresentUnclaimed {
		t.Fatalf("actions = %#v, want exact observation blocked by nonmatching authority", actions)
	}
}

func TestBuildCarrierRelationActionsValidatesEveryManagementFact(t *testing.T) {
	locked, _ := statusClaudePluginExtensionLockfile(t, "context7", "context7@market")
	owner := relationTestOwner(t)
	for _, test := range []struct {
		name  string
		input RelationInput
		want  string
	}{
		{
			name: "zero pending install",
			input: RelationInput{
				CurrentOwner:    owner,
				PendingInstalls: []durablecarrier.PendingCarrierInstall{{}},
			},
			want: "relation pending install[0]",
		},
		{
			name: "zero managed claim",
			input: RelationInput{
				CurrentOwner:  owner,
				ManagedClaims: []durablecarrier.ManagedCarrierClaim{{}},
			},
			want: "relation managed claim[0]",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.input.Locked = locked
			test.input.SelectedTargets = statusClaudeSelection(t, "claude-code")
			_, err := BuildRelationActions(test.input)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("BuildRelationActions error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestBuildCarrierRelationActionsBoundsAntigravityEvidenceWithSeparateAuthority(t *testing.T) {
	locked, subject := statusAntigravityPluginExtensionLockfile(
		t,
		"guidance",
		"guidance@google",
	)
	row, err := observerelation.NewRow(observerelation.RowSpec{
		SubjectKey: string(subject.ExpectedRelation().SubjectKey()),
	})
	if err != nil {
		t.Fatal(err)
	}
	observation := mustStatusObservationBatch(
		t,
		locked.Locked.Subjects()[0].SubjectID(),
		subject,
		observerelation.Correlate(
			subject.ExpectedRelation(),
			mustStatusGenericRelationInventory(t, observerelation.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceFresh,
				Rows:         []observerelation.Row{row},
			}),
		),
	)
	pending, claim, owner := relationManagementFacts(t, locked)

	for _, test := range []struct {
		name    string
		pending []durablecarrier.PendingCarrierInstall
		claims  []durablecarrier.ManagedCarrierClaim
		want    reconciliation.RelationActionKind
	}{
		{name: "no authority", want: reconciliation.ActionBlock},
		{
			name:    "matching pending install",
			pending: []durablecarrier.PendingCarrierInstall{pending},
			want:    reconciliation.ActionNoOp,
		},
		{
			name:   "matching durable claim",
			claims: []durablecarrier.ManagedCarrierClaim{claim},
			want:   reconciliation.ActionNoOp,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			actions, err := BuildRelationActions(RelationInput{
				Locked:          locked,
				SelectedTargets: statusClaudeSelection(t, "antigravity-cli"),
				Observations:    observation,
				CurrentOwner:    owner,
				PendingInstalls: test.pending,
				ManagedClaims:   test.claims,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(actions) != 1 || actions[0].Kind() != test.want {
				t.Fatalf("actions = %#v, want one %q action", actions, test.want)
			}
			if actions[0].CorrelationState() != observerelation.StateUnkeyedSameSubject {
				t.Fatalf(
					"correlation state = %q, want source-inexact evidence retained",
					actions[0].CorrelationState(),
				)
			}
		})
	}
}

func TestBuildCarrierRelationActionsAcceptsEmptySelectedTargets(t *testing.T) {
	locked, _ := statusClaudePluginExtensionLockfile(t, "context7", "context7@market")
	actions, err := buildRelationActions(
		t,
		locked,
		reconciliation.SelectedTargets{},
		observerelation.Batch{},
	)
	if err != nil {
		t.Fatalf("BuildRelationActions empty selected targets returned error: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("BuildRelationActions empty selected targets actions = %#v, want none", actions)
	}
}

func TestBuildCarrierRelationActionsTreatsAbsentSubjectObservationAsUnsupported(t *testing.T) {
	locked, _ := statusClaudePluginExtensionLockfile(t, "context7", "context7@market")
	actions, err := buildRelationActions(
		t,
		locked,
		statusClaudeSelection(t, "claude-code"),
		observerelation.Batch{},
	)
	if err != nil {
		t.Fatalf("BuildRelationActions returned error: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("actions = %#v, want one", actions)
	}
	action := actions[0]
	if action.Kind() != reconciliation.ActionObserveOnly ||
		action.CorrelationState() != observerelation.StateUnsupported ||
		action.InvokesHostRoute() ||
		action.BlocksOrdinaryApply() {
		t.Fatalf(
			"action = kind %q state %q invokes %t blocks %t, want unsupported observe-only without subject observation",
			action.Kind(),
			action.CorrelationState(),
			action.InvokesHostRoute(),
			action.BlocksOrdinaryApply(),
		)
	}
}

func TestBuildCarrierRelationActionsUsesClaudeHostUserInventoryForGlobalDesiredScope(t *testing.T) {
	locked, subject := statusClaudePluginExtensionLockfileWithScope(t, "context7-global", "context7@market", target.ScopeGlobal)
	tests := []struct {
		name      string
		inventory observeclaudeplugin.Inventory
		wantKind  reconciliation.RelationActionKind
		wantState observerelation.CorrelationState
	}{
		{
			name: "host user row satisfies daem global",
			inventory: mustStatusClaudePluginInventory(t, observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceFresh,
				Rows: []observeclaudeplugin.Row{
					mustStatusManagedRowWithHostScope(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeUser),
				},
			}),
			wantKind:  reconciliation.ActionBlock,
			wantState: observerelation.StateExactCorrelation,
		},
		{
			name: "host project row with same managed key does not satisfy daem global",
			inventory: mustStatusClaudePluginInventory(t, observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceFresh,
				Rows: []observeclaudeplugin.Row{
					mustStatusManagedRowWithHostScope(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeProject),
				},
			}),
			wantKind:  reconciliation.ActionCreate,
			wantState: observerelation.StateMissing,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actions, err := buildRelationActions(
				t,
				locked,
				statusClaudeSelection(t, "claude-code"),
				mustStatusClaudeObservationBatch(t, locked.Locked.Subjects()[0].SubjectID(), subject, tt.inventory),
			)
			if err != nil {
				t.Fatalf("BuildRelationActions returned error: %v", err)
			}
			if len(actions) != 1 {
				t.Fatalf("actions = %#v, want one", actions)
			}
			action := actions[0]
			if action.Kind() != tt.wantKind || action.CorrelationState() != tt.wantState {
				t.Fatalf(
					"action = kind %q state %q, want kind %q state %q",
					action.Kind(),
					action.CorrelationState(),
					tt.wantKind,
					tt.wantState,
				)
			}
		})
	}
}

func TestBuildCarrierRelationActionsUsesFreshCodexMissingObservation(t *testing.T) {
	locked, subject := statusCodexPluginExtensionLockfile(t, "documents-managed", "documents@openai-primary-runtime")
	actions, err := buildRelationActions(
		t,
		locked,
		statusClaudeSelection(t, "codex"),
		mustStatusObservationBatch(
			t,
			locked.Locked.Subjects()[0].SubjectID(),
			subject,
			observerelation.Correlate(
				subject.ExpectedRelation(),
				mustStatusGenericRelationInventory(t, observerelation.InventorySpec{
					Availability: observerelation.InventorySupported,
					Freshness:    observerelation.EvidenceFresh,
				}),
			),
		),
	)
	if err != nil {
		t.Fatalf("BuildRelationActions returned error: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("actions = %#v, want one", actions)
	}
	action := actions[0]
	if action.Subject() != locked.Locked.Subjects()[0].SubjectID() ||
		action.Target() != target.TargetCodex ||
		action.Scope() != target.ScopeGlobal ||
		action.RelationSubjectKey() != string(subject.ExpectedRelation().SubjectKey()) ||
		action.Kind() != reconciliation.ActionCreate ||
		action.Execution() != reconciliation.ExecutionHostRoute ||
		action.CorrelationState() != observerelation.StateMissing ||
		!action.InvokesHostRoute() ||
		action.BlocksOrdinaryApply() {
		t.Fatalf("action = %#v, want Codex global missing-relation host-route create", action)
	}
}

func TestBuildCarrierRelationActionsBlocksCodexWithoutCurrentObservation(t *testing.T) {
	locked, subject := statusCodexPluginExtensionLockfile(t, "documents-managed", "documents@openai-primary-runtime")
	actions, err := buildRelationActions(
		t,
		locked,
		statusClaudeSelection(t, "codex"),
		observerelation.Batch{},
	)
	if err != nil {
		t.Fatalf("BuildRelationActions returned error: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("actions = %#v, want one", actions)
	}
	action := actions[0]
	if action.Subject() != locked.Locked.Subjects()[0].SubjectID() ||
		action.RelationSubjectKey() != string(subject.ExpectedRelation().SubjectKey()) ||
		action.Kind() != reconciliation.ActionObserveOnly ||
		action.Execution() != reconciliation.ExecutionObserveOnly ||
		action.CorrelationState() != observerelation.StateUnsupported ||
		action.Reason() != reconciliation.ReasonUnsupportedPassiveInventory ||
		action.RouteAdmission().ObservationPolicy() != reconciliation.ObservationRequireCurrent ||
		action.InvokesHostRoute() ||
		action.BlocksOrdinaryApply() {
		t.Fatalf("action = %#v, want non-executing Codex observation-only action", action)
	}
}

func TestCarrierRelationObservationPolicyRejectsMixedLockShapes(t *testing.T) {
	claudeLocked, _ := statusClaudePluginExtensionLockfile(t, "claude", "plugin@market")
	codexLocked, _ := statusCodexPluginExtensionLockfile(t, "codex", "plugin@market")
	claudeInstall, _ := claudeLocked.Locked.Subjects()[0].OperationContract(lock.OperationInstall)
	claudeObserve, _ := claudeLocked.Locked.Subjects()[0].OperationContract(lock.OperationObserve)
	codexInstall, _ := codexLocked.Locked.Subjects()[0].OperationContract(lock.OperationInstall)
	insufficientCodexInstall, err := lock.NewOperationContract(lock.OperationContractInput{
		Operation:            codexInstall.Operation(),
		Actuation:            codexInstall.Actuation(),
		Authority:            codexInstall.Authority(),
		Route:                codexInstall.Route(),
		HostCompatibility:    codexInstall.HostCompatibility(),
		Preconditions:        codexInstall.Preconditions(),
		EffectEnvelope:       codexInstall.EffectEnvelope(),
		EffectPostconditions: codexInstall.EffectPostconditions().Requirements(),
		Idempotency:          codexInstall.Idempotency(),
		Verification:         lock.VerificationInsufficient,
		TrustActivation:      codexInstall.TrustActivation(),
		Recovery:             codexInstall.Recovery(),
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		install    lock.OperationContract
		observe    lock.OperationContract
		hasObserve bool
	}{
		{name: "observer with insufficient install verification", install: insufficientCodexInstall, observe: claudeObserve, hasObserve: true},
		{name: "host relation verification without observer", install: claudeInstall},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := carrierRelationObservationPolicy(tt.install, tt.observe, tt.hasObserve)
			if err == nil || !strings.Contains(err.Error(), "contract shape is unsupported") {
				t.Fatalf("carrierRelationObservationPolicy error = %v, want unsupported shape", err)
			}
		})
	}
}

func TestBuildCarrierRelationActionsPreservesClaudeGlobalIdentity(t *testing.T) {
	locked, subject := statusClaudePluginExtensionLockfileWithScope(t, "context7-global", "context7@market", target.ScopeGlobal)
	actions, err := buildRelationActions(
		t,
		locked,
		statusClaudeSelection(t, "claude-code"),
		observerelation.Batch{},
	)
	if err != nil {
		t.Fatalf("BuildRelationActions returned error: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("actions = %#v, want one", actions)
	}
	action := actions[0]
	route := action.RouteRequest()
	routeProfile, ok := profile.Profile(target.TargetClaudeCode).DelegatedRoute(extension.CarrierClaudeCodePlugin)
	if !ok {
		t.Fatal("Claude Code target profile is missing plugin route")
	}
	installRoute, ok := routeProfile.OperationRoute(profile.OperationInstall)
	if !ok {
		t.Fatal("Claude Code target profile is missing plugin install route")
	}
	if action.Subject() != locked.Locked.Subjects()[0].SubjectID() ||
		action.Target() != target.TargetClaudeCode ||
		action.Scope() != target.ScopeGlobal ||
		action.RelationSubjectKey() != string(subject.ExpectedRelation().SubjectKey()) ||
		action.Kind() != reconciliation.ActionObserveOnly ||
		action.Execution() != reconciliation.ExecutionObserveOnly ||
		action.InvokesHostRoute() ||
		action.BlocksOrdinaryApply() ||
		route.RouteID() != installRoute.RouteID() ||
		route.ContractVersion() != installRoute.AdapterContractVersion() {
		t.Fatalf("action = %#v, route = %#v, want non-executing global Claude relation identity", action, route)
	}
	if !strings.Contains(string(subject.ExpectedRelation().ManagedInstanceKey()), `"scope":"global"`) ||
		strings.Contains(string(subject.ExpectedRelation().ManagedInstanceKey()), `"scope":"user"`) {
		t.Fatalf("managed key = %q, want daem global scope and no host user scope", subject.ExpectedRelation().ManagedInstanceKey())
	}
}

func TestBuildCarrierRelationActionsRespectsTargetSelection(t *testing.T) {
	locked, _ := statusClaudePluginExtensionLockfile(t, "context7", "context7@market")
	actions, err := buildRelationActions(
		t,
		locked,
		statusClaudeSelection(t, "codex"),
		observerelation.Batch{},
	)
	if err != nil {
		t.Fatalf("BuildRelationActions returned error: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("actions = %#v, want none for unselected Claude target", actions)
	}
}

func TestBuildCarrierRelationActionsTreatsZeroInventoryAsUnsupported(t *testing.T) {
	locked, _ := statusClaudePluginExtensionLockfile(t, "context7", "context7@market")
	actions, err := buildRelationActions(
		t,
		locked,
		statusClaudeSelection(t, "claude-code"),
		observerelation.Batch{},
	)
	if err != nil {
		t.Fatalf("BuildRelationActions returned error: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("actions = %#v, want one", actions)
	}
	action := actions[0]
	if action.Kind() != reconciliation.ActionObserveOnly ||
		action.CorrelationState() != observerelation.StateUnsupported ||
		action.BlocksOrdinaryApply() {
		t.Fatalf("action = kind %q state %q blocks %t, want unsupported observe-only", action.Kind(), action.CorrelationState(), action.BlocksOrdinaryApply())
	}
}

func TestBuildCarrierRelationActionsSortsMultipleSubjectsDeterministically(t *testing.T) {
	first, _ := statusClaudePluginExtensionLockfile(t, "alpha", "alpha@market")
	second, _ := statusClaudePluginExtensionLockfile(t, "zed", "zed@market")
	actions, err := buildRelationActions(
		t,
		statusLockfileFromRecords(t, second.Locked.Subjects()[0], first.Locked.Subjects()[0]),
		statusClaudeSelection(t, "claude-code"),
		observerelation.Batch{},
	)
	if err != nil {
		t.Fatalf("BuildRelationActions returned error: %v", err)
	}
	if len(actions) != 2 {
		t.Fatalf("actions = %#v, want two", actions)
	}
	if actions[0].Subject().Key() != "alpha" || actions[1].Subject().Key() != "zed" {
		t.Fatalf("action order = %q, %q; want alpha, zed", actions[0].Subject().Key(), actions[1].Subject().Key())
	}
}

func buildRelationActions(
	t *testing.T,
	locked lock.File,
	selectedTargets reconciliation.SelectedTargets,
	observations observerelation.Batch,
) ([]reconciliation.RelationAction, error) {
	t.Helper()
	return BuildRelationActions(RelationInput{
		Locked:          locked,
		SelectedTargets: selectedTargets,
		Observations:    observations,
		CurrentOwner:    relationTestOwner(t),
	})
}

func relationManagementFacts(
	t *testing.T,
	locked lock.File,
) (durablecarrier.PendingCarrierInstall, durablecarrier.ManagedCarrierClaim, stateauthority.Authority) {
	t.Helper()
	contract := locked.Locked.Subjects()[0]
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(contract)
	if err != nil || !admitted {
		t.Fatalf("ManagedCarrierIdentityFromLockedRecord = admitted %t, error %v", admitted, err)
	}
	request, err := lock.DelegatedOperationRequest(contract, lock.OperationInstall)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	owner, err := stateauthority.New(pathtest.Exact(
		filepath.Join(root, ".daem", "state.json"),
	),

		filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	pending, err := durablecarrier.NewPendingCarrierInstall(owner, identity, request)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		identity,
		request,
		durablecarrier.ClaimProvenanceInstalledObserved,
	)
	if err != nil {
		t.Fatal(err)
	}
	return pending, claim, owner
}

func relationTestOwner(t *testing.T) stateauthority.Authority {
	t.Helper()
	root := t.TempDir()
	owner, err := stateauthority.New(pathtest.Exact(
		filepath.Join(root, ".daem", "state.json"),
	),

		filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func statusLockfileFromRecords(t *testing.T, records ...lock.LockedSubjectContract) lock.File {
	t.Helper()
	return snapshottest.File(t, records...)
}

func statusClaudePluginExtensionLockfile(
	t *testing.T,
	declarationID string,
	pluginKey string,
) (lock.File, realization.DelegatedRelation) {
	t.Helper()
	return statusClaudePluginExtensionLockfileWithScope(t, declarationID, pluginKey, target.ScopeProject)
}

func statusClaudePluginExtensionLockfileWithScope(
	t *testing.T,
	declarationID string,
	pluginKey string,
	scope target.Scope,
) (lock.File, realization.DelegatedRelation) {
	t.Helper()
	value := desiredtest.Extension(t, extension.Spec{
		Name:    declarationID,
		Carrier: extension.CarrierClaudeCodePlugin,
		Target:  target.TargetClaudeCode,
		Scope:   scope,
		Source:  desiredtest.ExtensionSource(t, extension.SourceKindMarketplace, pluginKey),
	})
	return snapshottest.ExtensionCarrierFile(t, value)
}

func statusCodexPluginExtensionLockfile(
	t *testing.T,
	declarationID string,
	pluginKey string,
) (lock.File, realization.DelegatedRelation) {
	t.Helper()
	value := desiredtest.Extension(t, extension.Spec{
		Name:    declarationID,
		Carrier: extension.CarrierCodexPlugin,
		Target:  target.TargetCodex,
		Scope:   target.ScopeGlobal,
		Source:  desiredtest.ExtensionSource(t, extension.SourceKindMarketplace, pluginKey),
	})
	return snapshottest.ExtensionCarrierFile(t, value)
}

func statusAntigravityPluginExtensionLockfile(
	t *testing.T,
	declarationID string,
	sourceRef string,
) (lock.File, realization.DelegatedRelation) {
	t.Helper()
	value := desiredtest.Extension(t, extension.Spec{
		Name:    declarationID,
		Carrier: extension.CarrierAntigravityCLIPlugin,
		Target:  target.TargetAntigravityCLI,
		Scope:   target.ScopeGlobal,
		Source:  desiredtest.ExtensionSource(t, extension.SourceKindHostSource, sourceRef),
	})
	return snapshottest.ExtensionCarrierFile(t, value)
}

func statusClaudeSelection(t *testing.T, requested ...string) reconciliation.SelectedTargets {
	t.Helper()
	targets := make([]target.Target, 0, len(requested))
	for _, value := range requested {
		targets = append(targets, target.Target(value))
	}
	selection, err := reconciliation.NewSelectedTargets(targets)
	if err != nil {
		t.Fatalf("NewSelectedTargets returned error: %v", err)
	}
	return selection
}

func mustStatusClaudePluginInventory(t *testing.T, spec observeclaudeplugin.InventorySpec) observeclaudeplugin.Inventory {
	t.Helper()
	inventory, err := observeclaudeplugin.NewInventory(spec)
	if err != nil {
		t.Fatalf("NewInventory returned error: %v", err)
	}
	return inventory
}

func mustStatusGenericRelationInventory(t *testing.T, spec observerelation.InventorySpec) observerelation.Inventory {
	t.Helper()
	inventory, err := observerelation.NewInventory(spec)
	if err != nil {
		t.Fatalf("NewInventory returned error: %v", err)
	}
	return inventory
}

func mustStatusClaudeObservationBatch(
	t *testing.T,
	lockedSubject topology.SubjectID,
	subject realization.DelegatedRelation,
	inventory observeclaudeplugin.Inventory,
) observerelation.Batch {
	t.Helper()
	return mustStatusObservationBatch(
		t,
		lockedSubject,
		subject,
		observeclaudeplugin.Correlate(subject, inventory),
	)
}

func mustStatusObservationBatch(
	t *testing.T,
	lockedSubject topology.SubjectID,
	subject realization.DelegatedRelation,
	correlation observerelation.CorrelationResult,
) observerelation.Batch {
	t.Helper()
	key, err := observerelation.NewCorrelationKey(
		lockedSubject,
		subject.ExpectedRelation(),
	)
	if err != nil {
		t.Fatalf("observerelation.NewCorrelationKey returned error: %v", err)
	}
	batch, err := observerelation.NewBatch(observerelation.BatchSpec{
		Correlations: []observerelation.Correlation{{
			Key:    key,
			Result: correlation,
		}},
	})
	if err != nil {
		t.Fatalf("observerelation.NewBatch returned error: %v", err)
	}
	return batch
}

func mustStatusManagedRow(t *testing.T, subjectKey string, managedKey string) observeclaudeplugin.Row {
	t.Helper()
	return mustStatusManagedRowWithHostScope(t, subjectKey, managedKey, observeclaudeplugin.HostScopeProject)
}

func mustStatusManagedRowWithHostScope(
	t *testing.T,
	subjectKey string,
	managedKey string,
	scope observeclaudeplugin.HostScope,
) observeclaudeplugin.Row {
	t.Helper()
	return mustStatusRow(t, observeclaudeplugin.RowSpec{
		SubjectKey:            subjectKey,
		HasManagedInstanceKey: true,
		ManagedInstanceKey:    managedKey,
		Scope:                 scope,
	})
}

func mustStatusUnmanagedRow(t *testing.T, subjectKey string) observeclaudeplugin.Row {
	t.Helper()
	return mustStatusRow(t, observeclaudeplugin.RowSpec{
		SubjectKey: subjectKey,
		Scope:      observeclaudeplugin.HostScopeProject,
	})
}

func mustStatusRow(t *testing.T, spec observeclaudeplugin.RowSpec) observeclaudeplugin.Row {
	t.Helper()
	row, err := observeclaudeplugin.NewRow(spec)
	if err != nil {
		t.Fatalf("NewRow returned error: %v", err)
	}
	return row
}
