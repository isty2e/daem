package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
)

func TestPlanDryRunSurfacesClaudePluginCarrierWithoutDelegateAttempt(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			root := t.TempDir()
			manifestPath := filepath.Join(root, "daem.toml")
			lockfilePath := filepath.Join(root, "daem.lock.toml")
			writeApplyFile(t, manifestPath, `
version = 1
targets = ["claude-code"]
`)
			writeApplyLockfile(t, lockfilePath, applyClaudePluginCarrierLockfileForScope(t, scope))

			result, err := PlanDryRun(context.Background(), CommandInput{
				ManifestPath: manifestPath,
				LockfilePath: lockfilePath,
				TargetValues: []string{"claude-code"},
			})
			if err != nil {
				t.Fatalf("PlanDryRun returned error: %v", err)
			}
			if len(result.Reconciliation.Delegates()) != 0 {
				t.Fatalf("delegate actions = %#v, want none for Claude plugin carrier", result.Reconciliation.Delegates())
			}
			if len(result.HostRouteAttempts) != 0 {
				t.Fatalf("host route attempts = %#v, want none during dry-run", result.HostRouteAttempts)
			}
			if _, err := os.Stat(filepath.Join(root, ".daem", "state.json")); err == nil {
				t.Fatal("dry-run wrote a statefile")
			} else if !os.IsNotExist(err) {
				t.Fatalf("stat dry-run statefile: %v", err)
			}
			if len(result.Reconciliation.Relations()) != 1 {
				t.Fatalf("RelationActions = %#v, want one disclosed create action", result.Reconciliation.Relations())
			}
			action := result.Reconciliation.Relations()[0]
			if action.Kind() != reconciliation.ActionCreate ||
				action.Execution() != reconciliation.ExecutionHostRoute ||
				action.BlocksOrdinaryApply() ||
				!action.InvokesHostRoute() {
				t.Fatalf(
					"action = kind %q execution %q blocks %t invokes %t",
					action.Kind(),
					action.Execution(),
					action.BlocksOrdinaryApply(),
					action.InvokesHostRoute(),
				)
			}
		})
	}
}

func TestPlanWriteRejectsBlockedCarrierRelationActionsWithClaudeFixture(t *testing.T) {
	record, subject := applyClaudePluginCarrierContractWithDeclarationID(t, target.ScopeProject, "context7")
	tests := []struct {
		name string
		spec observeclaudeplugin.InventorySpec
	}{
		{
			name: "stale exact-looking relation",
			spec: observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceStale,
				Rows: []observeclaudeplugin.Row{
					applyClaudePluginCarrierManagedRow(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey())),
				},
			},
		},
		{
			name: "unmanaged same-name relation",
			spec: observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceFresh,
				Rows: []observeclaudeplugin.Row{
					applyClaudePluginCarrierUnmanagedRow(t, "context7@market"),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := applyClaudePluginCarrierActionForSubject(t, record, subject, tt.spec)
			err := rejectBlockedRelationActions(reconciliationWithRelations(t, action))
			if !errors.Is(err, ErrRelationActionBlock) {
				t.Fatalf("rejectBlockedRelationActions error = %v, want ErrRelationActionBlock", err)
			}
		})
	}
}

func TestPlanWriteGatesClaudePluginCarrierOnScopedInventory(t *testing.T) {
	_, manifestPath, lockfilePath, _, locked, subject := writeApplyClaudePluginCarrierCommandFixture(t)
	tests := []struct {
		name        string
		spec        observeclaudeplugin.InventorySpec
		wantError   bool
		wantKind    reconciliation.RelationActionKind
		wantState   observerelation.CorrelationState
		wantInvokes bool
	}{
		{
			name: "unsupported scoped inventory cannot authorize host route",
			spec: observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventoryUnsupported,
				Freshness:    observerelation.EvidenceFresh,
			},
			wantKind:  reconciliation.ActionObserveOnly,
			wantState: observerelation.StateUnsupported,
		},
		{
			name: "unavailable scoped inventory cannot authorize host route",
			spec: observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventoryUnavailable,
				Freshness:    observerelation.EvidenceFresh,
			},
			wantKind:  reconciliation.ActionObserveOnly,
			wantState: observerelation.StateUnavailableEvidence,
		},
		{
			name: "stale exact-looking scoped inventory blocks",
			spec: observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceStale,
				Rows: []observeclaudeplugin.Row{
					applyClaudePluginCarrierManagedRow(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey())),
				},
			},
			wantError: true,
			wantKind:  reconciliation.ActionBlock,
			wantState: observerelation.StateStaleEvidence,
		},
		{
			name: "ambiguous scoped inventory blocks",
			spec: observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceFresh,
				Rows: []observeclaudeplugin.Row{
					applyClaudePluginCarrierManagedRow(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey())),
					applyClaudePluginCarrierManagedRow(t, "context7-copy", string(subject.ExpectedRelation().ManagedInstanceKey())),
				},
			},
			wantError: true,
			wantKind:  reconciliation.ActionBlock,
			wantState: observerelation.StateAmbiguous,
		},
		{
			name: "wrong host scope rows do not replace scoped project evidence",
			spec: observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceFresh,
				Rows: []observeclaudeplugin.Row{
					applyClaudePluginCarrierManagedRowWithScope(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeUser),
				},
			},
			wantKind:    reconciliation.ActionCreate,
			wantState:   observerelation.StateMissing,
			wantInvokes: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inventory := applyClaudePluginCarrierInventory(t, tt.spec)
			observations := applyClaudeObservationBatch(t, locked, subject, inventory)
			result, err := PlanWrite(context.Background(), CommandInput{
				ManifestPath:         manifestPath,
				LockfilePath:         lockfilePath,
				TargetValues:         []string{"claude-code"},
				RelationObservations: &observations,
			})
			if tt.wantError {
				if !errors.Is(err, ErrRelationActionBlock) {
					t.Fatalf("PlanWrite error = %v, want ErrRelationActionBlock", err)
				}
			} else if err != nil {
				t.Fatalf("PlanWrite returned error: %v", err)
			}
			if len(result.Reconciliation.Relations()) != 1 {
				t.Fatalf("RelationActions = %#v, want one", result.Reconciliation.Relations())
			}
			action := result.Reconciliation.Relations()[0]
			if action.Kind() != tt.wantKind ||
				action.CorrelationState() != tt.wantState ||
				action.InvokesHostRoute() != tt.wantInvokes {
				t.Fatalf(
					"action = kind %q state %q invokes %t, want kind %q state %q invokes %t",
					action.Kind(),
					action.CorrelationState(),
					action.InvokesHostRoute(),
					tt.wantKind,
					tt.wantState,
					tt.wantInvokes,
				)
			}
		})
	}
}

func TestPlanWriteIgnoresPersistedClaudePluginHostRouteAttemptHistory(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			root, manifestPath, lockfilePath, missingInventory, locked, _ := writeApplyClaudePluginCarrierCommandFixtureForScope(t, scope)
			prior := applyPriorHostRouteAttemptRecord(t, locked.Locked.Subjects()[0])
			writeApplyStatefile(t, filepath.Join(root, ".daem", "state.json"), applyStateSnapshot(t, durable.SnapshotInput{
				HostRouteAttempts: []durableattempt.HostRouteAttempt{prior},
			}))

			result, err := PlanWrite(context.Background(), CommandInput{
				ManifestPath:         manifestPath,
				LockfilePath:         lockfilePath,
				TargetValues:         []string{"claude-code"},
				RelationObservations: &missingInventory,
			})
			if err != nil {
				t.Fatalf("PlanWrite returned error: %v", err)
			}
			if len(result.HostRouteAttempts) != 0 {
				t.Fatalf("PlanWrite host route attempts = %#v, want none before execution", result.HostRouteAttempts)
			}
			if len(result.Reconciliation.Relations()) != 1 {
				t.Fatalf("RelationActions = %#v, want one", result.Reconciliation.Relations())
			}
			action := result.Reconciliation.Relations()[0]
			if action.Kind() != reconciliation.ActionCreate ||
				action.CorrelationState() != observerelation.StateMissing ||
				action.Target() != target.TargetClaudeCode ||
				action.Scope() != scope ||
				!action.InvokesHostRoute() {
				t.Fatalf(
					"action = kind %q state %q target %q scope %q invokes %t, want create/missing claude-code/%s invoking host route",
					action.Kind(),
					action.CorrelationState(),
					action.Target(),
					action.Scope(),
					action.InvokesHostRoute(),
					scope,
				)
			}
		})
	}
}

func TestPlanWriteDoesNotRepairStaleClaudePluginEvidenceWithPriorHistory(t *testing.T) {
	tests := []struct {
		name         string
		scope        target.Scope
		rowScope     observeclaudeplugin.HostScope
		availability observerelation.InventoryAvailability
		freshness    observerelation.EvidenceFreshness
		withExactRow bool
		wantError    bool
		wantKind     reconciliation.RelationActionKind
		wantState    observerelation.CorrelationState
		wantInvokes  bool
	}{
		{
			name:         "project stale exact evidence stays blocked",
			scope:        target.ScopeProject,
			rowScope:     observeclaudeplugin.HostScopeProject,
			availability: observerelation.InventorySupported,
			freshness:    observerelation.EvidenceStale,
			withExactRow: true,
			wantError:    true,
			wantKind:     reconciliation.ActionBlock,
			wantState:    observerelation.StateStaleEvidence,
		},
		{
			name:         "global stale exact evidence stays blocked",
			scope:        target.ScopeGlobal,
			rowScope:     observeclaudeplugin.HostScopeUser,
			availability: observerelation.InventorySupported,
			freshness:    observerelation.EvidenceStale,
			withExactRow: true,
			wantError:    true,
			wantKind:     reconciliation.ActionBlock,
			wantState:    observerelation.StateStaleEvidence,
		},
		{
			name:         "project unavailable evidence stays observe-only",
			scope:        target.ScopeProject,
			rowScope:     observeclaudeplugin.HostScopeProject,
			availability: observerelation.InventoryUnavailable,
			freshness:    observerelation.EvidenceFresh,
			wantKind:     reconciliation.ActionObserveOnly,
			wantState:    observerelation.StateUnavailableEvidence,
		},
		{
			name:         "global unavailable evidence stays observe-only",
			scope:        target.ScopeGlobal,
			rowScope:     observeclaudeplugin.HostScopeUser,
			availability: observerelation.InventoryUnavailable,
			freshness:    observerelation.EvidenceFresh,
			wantKind:     reconciliation.ActionObserveOnly,
			wantState:    observerelation.StateUnavailableEvidence,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, manifestPath, lockfilePath, _, locked, subject := writeApplyClaudePluginCarrierCommandFixtureForScope(t, test.scope)
			prior := applyPriorHostRouteAttemptRecord(t, locked.Locked.Subjects()[0])
			writeApplyStatefile(t, filepath.Join(root, ".daem", "state.json"), applyStateSnapshot(t, durable.SnapshotInput{
				HostRouteAttempts: []durableattempt.HostRouteAttempt{prior},
			}))
			spec := observeclaudeplugin.InventorySpec{
				Availability: test.availability,
				Freshness:    test.freshness,
			}
			if test.withExactRow {
				spec.Rows = []observeclaudeplugin.Row{
					applyClaudePluginCarrierManagedRowWithScope(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey()), test.rowScope),
				}
			}
			inventory := applyClaudePluginCarrierInventory(t, spec)
			observations := applyClaudeObservationBatch(t, locked, subject, inventory)

			result, err := PlanWrite(context.Background(), CommandInput{
				ManifestPath:         manifestPath,
				LockfilePath:         lockfilePath,
				TargetValues:         []string{"claude-code"},
				RelationObservations: &observations,
			})
			if test.wantError {
				if !errors.Is(err, ErrRelationActionBlock) {
					t.Fatalf("PlanWrite error = %v, want ErrRelationActionBlock", err)
				}
			} else if err != nil {
				t.Fatalf("PlanWrite returned error: %v", err)
			}
			if len(result.HostRouteAttempts) != 0 {
				t.Fatalf("PlanWrite host route attempts = %#v, want none before execution", result.HostRouteAttempts)
			}
			if len(result.Reconciliation.Relations()) != 1 {
				t.Fatalf("RelationActions = %#v, want one", result.Reconciliation.Relations())
			}
			action := result.Reconciliation.Relations()[0]
			if action.Kind() != test.wantKind ||
				action.CorrelationState() != test.wantState ||
				action.InvokesHostRoute() != test.wantInvokes {
				t.Fatalf(
					"action = kind %q state %q invokes %t, want kind %q state %q invokes %t",
					action.Kind(),
					action.CorrelationState(),
					action.InvokesHostRoute(),
					test.wantKind,
					test.wantState,
					test.wantInvokes,
				)
			}
		})
	}
}

func TestPlanWriteBlocksStaleClaudePluginCarrierInventoryByScope(t *testing.T) {
	tests := []struct {
		name     string
		scope    target.Scope
		rowScope observeclaudeplugin.HostScope
	}{
		{
			name:     "project stale host project row",
			scope:    target.ScopeProject,
			rowScope: observeclaudeplugin.HostScopeProject,
		},
		{
			name:     "global stale host user row",
			scope:    target.ScopeGlobal,
			rowScope: observeclaudeplugin.HostScopeUser,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, manifestPath, lockfilePath, _, locked, subject := writeApplyClaudePluginCarrierCommandFixtureForScope(t, test.scope)
			inventory := applyClaudePluginCarrierInventory(t, observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceStale,
				Rows: []observeclaudeplugin.Row{
					applyClaudePluginCarrierManagedRowWithScope(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey()), test.rowScope),
				},
			})
			observations := applyClaudeObservationBatch(t, locked, subject, inventory)

			result, err := PlanWrite(context.Background(), CommandInput{
				ManifestPath:         manifestPath,
				LockfilePath:         lockfilePath,
				TargetValues:         []string{"claude-code"},
				RelationObservations: &observations,
			})
			if !errors.Is(err, ErrRelationActionBlock) {
				t.Fatalf("PlanWrite error = %v, want ErrRelationActionBlock", err)
			}
			if len(result.Reconciliation.Relations()) != 1 {
				t.Fatalf("RelationActions = %#v, want one", result.Reconciliation.Relations())
			}
			action := result.Reconciliation.Relations()[0]
			if action.Kind() != reconciliation.ActionBlock ||
				action.CorrelationState() != observerelation.StateStaleEvidence ||
				action.InvokesHostRoute() {
				t.Fatalf(
					"action = kind %q state %q invokes %t, want stale block without host route",
					action.Kind(),
					action.CorrelationState(),
					action.InvokesHostRoute(),
				)
			}
		})
	}
}

func TestPlanWriteClassifiesClaudePluginScopeCollisionsByScope(t *testing.T) {
	tests := []struct {
		name        string
		scope       target.Scope
		rows        func(*testing.T, realization.DelegatedRelation) []observeclaudeplugin.Row
		wantError   bool
		wantKind    reconciliation.RelationActionKind
		wantState   observerelation.CorrelationState
		wantInvokes bool
	}{
		{
			name:  "project same-scope unmanaged row blocks",
			scope: target.ScopeProject,
			rows: func(t *testing.T, subject realization.DelegatedRelation) []observeclaudeplugin.Row {
				return []observeclaudeplugin.Row{
					applyClaudePluginCarrierUnmanagedRowWithScope(t, "context7@market", observeclaudeplugin.HostScopeProject),
				}
			},
			wantError: true,
			wantKind:  reconciliation.ActionBlock,
			wantState: observerelation.StateUnkeyedSameSubject,
		},
		{
			name:  "global same-scope unmanaged row blocks",
			scope: target.ScopeGlobal,
			rows: func(t *testing.T, subject realization.DelegatedRelation) []observeclaudeplugin.Row {
				return []observeclaudeplugin.Row{
					applyClaudePluginCarrierUnmanagedRowWithScope(t, "context7@market", observeclaudeplugin.HostScopeUser),
				}
			},
			wantError: true,
			wantKind:  reconciliation.ActionBlock,
			wantState: observerelation.StateUnkeyedSameSubject,
		},
		{
			name:  "project same plugin key different declaration id blocks",
			scope: target.ScopeProject,
			rows: func(t *testing.T, subject realization.DelegatedRelation) []observeclaudeplugin.Row {
				otherDeclaration := applyClaudePluginCarrierSubjectWithDeclarationID(t, target.ScopeProject, "context7-renamed")
				return []observeclaudeplugin.Row{
					applyClaudePluginCarrierManagedRowWithScope(t, "context7@market", string(otherDeclaration.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeProject),
				}
			},
			wantError: true,
			wantKind:  reconciliation.ActionBlock,
			wantState: observerelation.StateSameSubjectShadow,
		},
		{
			name:  "global same plugin key different declaration id blocks",
			scope: target.ScopeGlobal,
			rows: func(t *testing.T, subject realization.DelegatedRelation) []observeclaudeplugin.Row {
				otherDeclaration := applyClaudePluginCarrierSubjectWithDeclarationID(t, target.ScopeGlobal, "context7-renamed")
				return []observeclaudeplugin.Row{
					applyClaudePluginCarrierManagedRowWithScope(t, "context7@market", string(otherDeclaration.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeUser),
				}
			},
			wantError: true,
			wantKind:  reconciliation.ActionBlock,
			wantState: observerelation.StateSameSubjectShadow,
		},
		{
			name:  "project ambiguous managed rows block",
			scope: target.ScopeProject,
			rows: func(t *testing.T, subject realization.DelegatedRelation) []observeclaudeplugin.Row {
				return []observeclaudeplugin.Row{
					applyClaudePluginCarrierManagedRowWithScope(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeProject),
					applyClaudePluginCarrierManagedRowWithScope(t, "context7-copy", string(subject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeProject),
				}
			},
			wantError: true,
			wantKind:  reconciliation.ActionBlock,
			wantState: observerelation.StateAmbiguous,
		},
		{
			name:  "global ambiguous managed rows block",
			scope: target.ScopeGlobal,
			rows: func(t *testing.T, subject realization.DelegatedRelation) []observeclaudeplugin.Row {
				return []observeclaudeplugin.Row{
					applyClaudePluginCarrierManagedRowWithScope(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeUser),
					applyClaudePluginCarrierManagedRowWithScope(t, "context7-copy", string(subject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeUser),
				}
			},
			wantError: true,
			wantKind:  reconciliation.ActionBlock,
			wantState: observerelation.StateAmbiguous,
		},
		{
			name:  "project wrong-scope same-name row stays missing",
			scope: target.ScopeProject,
			rows: func(t *testing.T, subject realization.DelegatedRelation) []observeclaudeplugin.Row {
				return []observeclaudeplugin.Row{
					applyClaudePluginCarrierUnmanagedRowWithScope(t, "context7@market", observeclaudeplugin.HostScopeUser),
				}
			},
			wantKind:    reconciliation.ActionCreate,
			wantState:   observerelation.StateMissing,
			wantInvokes: true,
		},
		{
			name:  "global wrong-scope same-name row stays missing",
			scope: target.ScopeGlobal,
			rows: func(t *testing.T, subject realization.DelegatedRelation) []observeclaudeplugin.Row {
				return []observeclaudeplugin.Row{
					applyClaudePluginCarrierUnmanagedRowWithScope(t, "context7@market", observeclaudeplugin.HostScopeProject),
				}
			},
			wantKind:    reconciliation.ActionCreate,
			wantState:   observerelation.StateMissing,
			wantInvokes: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, manifestPath, lockfilePath, _, locked, subject := writeApplyClaudePluginCarrierCommandFixtureForScope(t, test.scope)
			inventory := applyClaudePluginCarrierInventory(t, observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceFresh,
				Rows:         test.rows(t, subject),
			})
			observations := applyClaudeObservationBatch(t, locked, subject, inventory)

			result, err := PlanWrite(context.Background(), CommandInput{
				ManifestPath:         manifestPath,
				LockfilePath:         lockfilePath,
				TargetValues:         []string{"claude-code"},
				RelationObservations: &observations,
			})
			if test.wantError {
				if !errors.Is(err, ErrRelationActionBlock) {
					t.Fatalf("PlanWrite error = %v, want ErrRelationActionBlock", err)
				}
			} else if err != nil {
				t.Fatalf("PlanWrite returned error: %v", err)
			}
			if len(result.Reconciliation.Relations()) != 1 {
				t.Fatalf("RelationActions = %#v, want one", result.Reconciliation.Relations())
			}
			action := result.Reconciliation.Relations()[0]
			if action.Kind() != test.wantKind ||
				action.CorrelationState() != test.wantState ||
				action.InvokesHostRoute() != test.wantInvokes {
				t.Fatalf(
					"action = kind %q state %q invokes %t, want kind %q state %q invokes %t",
					action.Kind(),
					action.CorrelationState(),
					action.InvokesHostRoute(),
					test.wantKind,
					test.wantState,
					test.wantInvokes,
				)
			}
		})
	}
}

func TestPlanWriteKeepsExactExternalRelationUnclaimedDespiteWrongScopeNoise(t *testing.T) {
	tests := []struct {
		name  string
		scope target.Scope
		rows  func(*testing.T, realization.DelegatedRelation) []observeclaudeplugin.Row
	}{
		{
			name:  "project exact row ignores host user local managed noise",
			scope: target.ScopeProject,
			rows: func(t *testing.T, subject realization.DelegatedRelation) []observeclaudeplugin.Row {
				return []observeclaudeplugin.Row{
					applyClaudePluginCarrierManagedRowWithScope(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeProject),
					applyClaudePluginCarrierManagedRowWithScope(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeUser),
					applyClaudePluginCarrierUnmanagedRowWithScope(t, "context7@market", observeclaudeplugin.HostScopeLocal),
					applyClaudePluginCarrierManagedRowWithScope(t, "context7-copy", string(subject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeManaged),
				}
			},
		},
		{
			name:  "global exact host user row ignores project local managed noise",
			scope: target.ScopeGlobal,
			rows: func(t *testing.T, subject realization.DelegatedRelation) []observeclaudeplugin.Row {
				return []observeclaudeplugin.Row{
					applyClaudePluginCarrierManagedRowWithScope(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeUser),
					applyClaudePluginCarrierManagedRowWithScope(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeProject),
					applyClaudePluginCarrierUnmanagedRowWithScope(t, "context7@market", observeclaudeplugin.HostScopeLocal),
					applyClaudePluginCarrierManagedRowWithScope(t, "context7-copy", string(subject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeManaged),
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, manifestPath, lockfilePath, _, locked, subject := writeApplyClaudePluginCarrierCommandFixtureForScope(t, test.scope)
			inventory := applyClaudePluginCarrierInventory(t, observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceFresh,
				Rows:         test.rows(t, subject),
			})
			observations := applyClaudeObservationBatch(t, locked, subject, inventory)

			_, err := PlanWrite(context.Background(), CommandInput{
				ManifestPath:         manifestPath,
				LockfilePath:         lockfilePath,
				TargetValues:         []string{"claude-code"},
				RelationObservations: &observations,
			})
			if !errors.Is(err, ErrRelationActionBlock) ||
				!strings.Contains(err.Error(), "reason=present_unclaimed") {
				t.Fatalf("PlanWrite error = %v, want present-unclaimed relation block", err)
			}
		})
	}
}

func TestRejectBlockedRelationActionsAllowsNonBlockingRelationActions(t *testing.T) {
	record, subject := applyClaudePluginCarrierContractWithDeclarationID(t, target.ScopeProject, "context7")
	tests := []struct {
		name string
		spec observeclaudeplugin.InventorySpec
	}{
		{
			name: "admitted missing relation invokes host route",
			spec: observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceFresh,
			},
		},
		{
			name: "observe only unsupported inventory",
			spec: observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventoryUnsupported,
				Freshness:    observerelation.EvidenceFresh,
			},
		},
		{
			name: "observe only unavailable inventory",
			spec: observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventoryUnavailable,
				Freshness:    observerelation.EvidenceFresh,
			},
		},
		{
			name: "exact managed no-op",
			spec: observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceFresh,
				Rows: []observeclaudeplugin.Row{
					applyClaudePluginCarrierManagedRow(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey())),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := applyClaudePluginCarrierActionForSubject(t, record, subject, tt.spec)
			if err := rejectBlockedRelationActions(reconciliationWithRelations(t, action)); err != nil {
				t.Fatalf("rejectBlockedRelationActions returned error: %v", err)
			}
			if tt.name == "admitted missing relation invokes host route" && !action.InvokesHostRoute() {
				t.Fatalf("action = %#v, want host route invocation", action)
			}
		})
	}
}

func TestRejectBlockedRelationActionsReportsFirstBlockedRelationAction(t *testing.T) {
	nonBlockingRecord, nonBlockingSubject := applyClaudePluginCarrierContractWithDeclarationID(
		t,
		target.ScopeProject,
		"alpha",
	)
	nonBlocking := applyClaudePluginCarrierActionForSubject(t, nonBlockingRecord, nonBlockingSubject, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			applyClaudePluginCarrierManagedRow(t, "context7@market", string(nonBlockingSubject.ExpectedRelation().ManagedInstanceKey())),
		},
	})
	blockedRecord, blockedSubject := applyClaudePluginCarrierContractWithDeclarationID(
		t,
		target.ScopeProject,
		"context7",
	)
	blocked := applyClaudePluginCarrierActionForSubject(t, blockedRecord, blockedSubject, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			applyClaudePluginCarrierUnmanagedRow(t, "context7@market"),
		},
	})

	err := rejectBlockedRelationActions(reconciliationWithRelations(t, nonBlocking, blocked))
	if !errors.Is(err, ErrRelationActionBlock) {
		t.Fatalf("rejectBlockedRelationActions error = %v, want ErrRelationActionBlock", err)
	}
	message := err.Error()
	for _, want := range []string{
		"subject=host_relation/claude-code.plugin-carrier/",
		"kind=block",
		"reason=unkeyed_same_subject",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("rejectBlockedRelationActions error = %q, want substring %q", message, want)
		}
	}
}

func reconciliationWithRelations(
	t testing.TB,
	actions ...reconciliation.RelationAction,
) reconciliation.Result {
	t.Helper()
	result, err := reconciliation.NewResult(reconciliation.ResultInput{
		Context:   reconciliation.ContextApply,
		Relations: actions,
	})
	if err != nil {
		t.Fatalf("assemble relation reconciliation result: %v", err)
	}
	return result
}

func TestPlanWriteAntigravityObserverRejectsInjectedUnmanagedInventory(t *testing.T) {
	tests := []struct {
		name        string
		targetValue string
		fixture     func(*testing.T) (string, string, string, observerelation.Batch, lock.File, realization.DelegatedRelation)
	}{
		{
			name:        "antigravity-cli",
			targetValue: "antigravity-cli",
			fixture:     writeApplyAntigravityCLIPluginCarrierCommandFixture,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, manifestPath, lockfilePath, _, locked, subject := test.fixture(t)
			inventory := applyRelationInventory(t, observerelation.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceFresh,
				Rows: []observerelation.Row{
					applyRelationUnmanagedRow(t, string(subject.ExpectedRelation().SubjectKey())),
				},
			})
			observations := applyRelationObservationBatch(t, locked, subject, inventory)
			_, err := PlanWrite(context.Background(), CommandInput{
				ManifestPath:         manifestPath,
				LockfilePath:         lockfilePath,
				TargetValues:         []string{test.targetValue},
				RelationObservations: &observations,
			})
			if !errors.Is(err, ErrRelationActionBlock) ||
				!strings.Contains(err.Error(), "reason=unkeyed_same_subject") {
				t.Fatalf(
					"PlanWrite error = %v, want unkeyed Antigravity relation block",
					err,
				)
			}
		})
	}
}

func TestPlanWriteOpenCodeObserverRejectsInjectedUnmanagedInventory(t *testing.T) {
	_, manifestPath, lockfilePath, _, locked, subject := writeApplyOpenCodePluginCarrierCommandFixture(t)
	inventory := applyRelationInventory(t, observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observerelation.Row{
			applyRelationUnmanagedRow(t, string(subject.ExpectedRelation().SubjectKey())),
		},
	})
	observations := applyRelationObservationBatch(t, locked, subject, inventory)
	_, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath:         manifestPath,
		LockfilePath:         lockfilePath,
		TargetValues:         []string{"opencode"},
		RelationObservations: &observations,
	})
	if !errors.Is(err, ErrRelationActionBlock) {
		t.Fatalf("PlanWrite error = %v, want ErrRelationActionBlock", err)
	}
}

func TestPlanWritePiObserverRejectsInjectedUnmanagedInventory(t *testing.T) {
	_, manifestPath, lockfilePath, _, locked, subject := writeApplyPiPackageCarrierCommandFixture(t)
	inventory := applyRelationInventory(t, observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observerelation.Row{
			applyRelationUnmanagedRow(t, string(subject.ExpectedRelation().SubjectKey())),
		},
	})
	observations := applyRelationObservationBatch(t, locked, subject, inventory)
	_, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath:         manifestPath,
		LockfilePath:         lockfilePath,
		TargetValues:         []string{"pi"},
		RelationObservations: &observations,
	})
	if !errors.Is(err, ErrRelationActionBlock) {
		t.Fatalf("PlanWrite error = %v, want ErrRelationActionBlock", err)
	}
}
