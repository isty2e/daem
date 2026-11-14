package apply

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/statefile"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func invocationNativeState(t *testing.T, current durable.Snapshot) (durable.Snapshot, durable.SnapshotCodec, statefile.PhysicalCodec) {
	t.Helper()
	_, baseline := nativeAggregatePlanningInput(t, reconcile.AggregateNoOp)
	current, err := current.WithManagedAggregates(baseline.ManagedAggregates())
	if err != nil {
		t.Fatal(err)
	}
	current, err = current.AdvanceNativeDocumentCommit()
	if err != nil {
		t.Fatal(err)
	}
	_, journalCodec, codec, err := nativePersistenceCodecs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return current, journalCodec, codec
}

func saveInvocationState(t *testing.T, path string, state durable.Snapshot, codec statefile.PhysicalCodec) {
	t.Helper()
	content, err := codec.EncodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func loadInvocationState(t *testing.T, path string, codec statefile.PhysicalCodec) durable.Snapshot {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	state, err := codec.DecodeState(content)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func requireInvocationNativeState(t *testing.T, before, after durable.Snapshot) {
	t.Helper()
	if len(after.ManagedAggregates()) != 1 || !after.ManagedAggregates()[0].Equal(before.ManagedAggregates()[0]) ||
		after.NativeDocumentCommit() != before.NativeDocumentCommit() {
		t.Fatal("invocation checkpoint changed the native state or publication identity")
	}
}

func TestInvocationStateCodecPreservesNativeStateAroundHostRoute(t *testing.T) {
	_, manifest, lockfile, missing, _, _ := writeApplyAntigravityCLIPluginCarrierCommandFixture(t)
	planned, err := PlanDryRun(t.Context(), CommandInput{ManifestPath: manifest, LockfilePath: lockfile, RelationObservations: &missing})
	if err != nil {
		t.Fatal(err)
	}
	paths := planned.planned.context.Paths
	current, journalCodec, codec := invocationNativeState(t, planned.planned.assessment.CurrentState)
	assessment := planned.planned.assessment
	assessment.CurrentState = current
	if err := checkNativeCommandSupport(assessment); err != nil {
		t.Fatal("admitted opaque route lost native codec custody", err)
	}
	options := applyDelegateRunOptions(t, paths, runOptions{journalStateCodec: journalCodec, physicalStateCodec: codec})
	saveInvocationState(t, paths.StatefilePath, current, codec)
	calls := 0
	options.HostRouteExecutor = subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
			calls++
			saved := loadInvocationState(t, paths.StatefilePath, codec)
			requireInvocationNativeState(t, current, saved)
			if len(saved.PendingCarrierInstalls()) != 1 {
				t.Fatal("host invocation ran without its pending install")
			}
			return subprocess.CommandResult{Started: true, HasExitCode: true}
		},
	})
	next, _, records, err := runHostRoutesAndPersistAttemptRecords(t.Context(), paths,
		planned.planned.context.Lockfile, paths.StatefilePath, current, planned.planned.assessment.Owner,
		planned.planned.assessment.GlobalCarrierClaims, planned.Reconciliation.Relations(), options)
	if err != nil {
		t.Fatal(err)
	}
	requireInvocationNativeState(t, current, next)
	saved := loadInvocationState(t, paths.StatefilePath, codec)
	if calls != 1 || len(records) != 1 || len(next.PendingCarrierInstalls()) != 0 || !saved.Equal(next) {
		t.Fatalf("host-route checkpoint did not preserve its complete successor: calls=%d records=%d", calls, len(records))
	}
}

func TestInvocationStateCodecPreservesNativeStateAroundCarrierRemoval(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, scenario := range []struct {
			name       string
			settlement bool
		}{{"invoke", false}, {"settle", true}} {
			t.Run(string(scope)+"/"+scenario.name, func(t *testing.T) {
				fixture := newCoupledWorkflowFixture(t, scope)
				if scenario.settlement {
					fixture.preparePendingSettlement(t)
				}
				current, _, codec := invocationNativeState(t, fixture.current)
				fixture.current = current
				saveInvocationState(t, fixture.statePath, current, codec)
				input := fixture.input(t)
				input.StatefileCodec = codec
				plan := scheduledCarrierRemovalTestPlan(t, input.Actions)
				result, err := runScheduledCarrierRemovals(t.Context(), input, plan, plan)
				if err != nil {
					t.Fatal(err)
				}
				requireInvocationNativeState(t, current, result.State)
				if !loadInvocationState(t, fixture.statePath, codec).Equal(result.State) ||
					len(result.State.PendingCarrierRemovals()) != 0 || len(result.State.ManagedCarrierClaims()) != 0 {
					t.Fatal("removal checkpoint did not retain the complete settled successor")
				}
				wantCalls := 1
				if scenario.settlement {
					wantCalls = 0
				}
				if fixture.executorCalls != wantCalls {
					t.Fatalf("host calls=%d want=%d", fixture.executorCalls, wantCalls)
				}
			})
		}
	}
}

func TestInvocationStateCodecPreservesNativeStateAroundPinTransition(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture := newPiPinApplyFixture(t, scope)
			planned, err := PlanWrite(t.Context(), fixture.input())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := planned.Close(); err != nil {
					t.Error(err)
				}
			})
			paths, err := daempaths.Resolve(fixture.manifest)
			if err != nil {
				t.Fatal(err)
			}
			current, err := statefile.LoadOptional(t.Context(), paths.StatefilePath)
			if err != nil {
				t.Fatal(err)
			}
			current, journalCodec, codec := invocationNativeState(t, current)
			saveInvocationState(t, paths.StatefilePath, current, codec)
			global := durablecarrier.EmptyGlobalCarrierClaims()
			if scope == target.ScopeGlobal {
				global, err = durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{fixture.claim})
				if err != nil {
					t.Fatal(err)
				}
			}
			selection, err := targetselection.ForDiagnostics([]string{"pi"})
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			options := applyDelegateRunOptions(t, paths, runOptions{
				journalStateCodec:       journalCodec,
				physicalStateCodec:      codec,
				acceptVisibilityChanges: func(context.Context) error { return nil },
				HostRouteObserver:       passiveHostRouteObserver(paths, fixture.locked, selection),
				HostRouteExecutor: subprocess.NewCommandExecutor(subprocess.CommandOptions{
					Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
						calls++
						requireInvocationNativeState(t, current, loadInvocationState(t, paths.StatefilePath, codec))
						writeApplyFile(t, fixture.settings, fmt.Sprintf("{\"packages\":[%q]}", fixture.after))
						return subprocess.CommandResult{Started: true, HasExitCode: true}
					},
				}),
			})
			next, nextGlobal, records, err := runHostRoutesAndPersistAttemptRecords(t.Context(), paths, fixture.locked,
				paths.StatefilePath, current, fixture.claim.Owner(), global, planned.Reconciliation.Relations(), options)
			if err != nil {
				t.Fatal(err)
			}
			requireInvocationNativeState(t, current, next)
			claims := next.ManagedCarrierClaims()
			if scope == target.ScopeGlobal {
				claims = nextGlobal.Claims()
			}
			if calls != 1 || len(records) != 1 || len(claims) != 1 || !claims[0].MatchesLockedRecord(fixture.locked.Locked.Subjects()[0]) ||
				!loadInvocationState(t, paths.StatefilePath, codec).Equal(next) {
				t.Fatal("pin transition failed to preserve the complete new acquisition")
			}
		})
	}
}
