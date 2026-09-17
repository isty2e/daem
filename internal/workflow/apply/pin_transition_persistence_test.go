package apply

import (
	"context"
	"fmt"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func TestPiPinTransitionPreservesManagementAtMetadataFailureFrontiers(t *testing.T) {
	for _, test := range []struct {
		name          string
		scope         target.Scope
		failCommit    int
		wantCalls     int
		wantPending   bool
		wantCompleted bool
	}{
		{name: "project reservation", scope: target.ScopeProject, failCommit: 1},
		{name: "project completion", scope: target.ScopeProject, failCommit: 2, wantCalls: 1, wantPending: true},
		{name: "project history", scope: target.ScopeProject, failCommit: 3, wantCalls: 1, wantCompleted: true},
		{name: "global history", scope: target.ScopeGlobal, failCommit: 1, wantCalls: 1, wantCompleted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPiPinApplyFixture(t, test.scope)
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
			global := durablecarrier.EmptyGlobalCarrierClaims()
			if test.scope == target.ScopeGlobal {
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
				acceptVisibilityChanges: func(context.Context) error { return nil },
				HostRouteObserver:       passiveHostRouteObserver(paths, fixture.locked, selection),
				HostRouteExecutor: subprocess.NewCommandExecutor(subprocess.CommandOptions{
					Clock: fixedApplyHostRouteClock,
					Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
						calls++
						writeApplyFile(t, fixture.settings, fmt.Sprintf("{\"packages\":[%q]}", fixture.after))
						return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
					},
				}),
			})
			reserve := options.reserveStatefileAuthority
			options.reserveStatefileAuthority = func(path string, plan statefileEffectPlan) (statefileEffectReservation, error) {
				reservation, err := reserve(path, plan)
				return pinCommitFailureReservation{statefileEffectReservation: reservation, failAt: test.failCommit, t: t}, err
			}
			next, nextGlobal, _, err := runHostRoutesAndPersistAttemptRecords(t.Context(), paths, fixture.locked, paths.StatefilePath, current, fixture.claim.Owner(), global, planned.Reconciliation.Relations(), options)
			if err == nil || calls != test.wantCalls {
				t.Fatalf("metadata refusal: calls=%d err=%v", calls, err)
			}

			persisted := loadCarrierClaimsForScope(t, fixture.root, fixture.manifest, test.scope)
			returned := next.ManagedCarrierClaims()
			if test.scope == target.ScopeGlobal {
				returned = nextGlobal.Claims()
			}
			if len(persisted) != 1 || len(returned) != 1 || !persisted[0].ExactEqual(returned[0]) {
				t.Fatal("metadata failure discarded or rolled back the latest retained management")
			}
			pending, present := persisted[0].PendingPinTransition()
			if present != test.wantPending {
				t.Fatalf("pending=%t want=%t", present, test.wantPending)
			}
			switch {
			case test.wantPending:
				if !pending.Before().ExactEqual(fixture.claim) {
					t.Fatal("completion failure lost original management")
				}
			case test.wantCompleted:
				if !persisted[0].MatchesLockedRecord(fixture.locked.Locked.Subjects()[0]) {
					t.Fatal("history failure reverted completed management")
				}
			default:
				if !persisted[0].ExactEqual(fixture.claim) {
					t.Fatal("reservation failure changed management")
				}
			}
			saved, err := statefile.LoadOptional(t.Context(), paths.StatefilePath)
			if err != nil || len(saved.HostRouteAttempts()) != 0 {
				t.Fatalf("unexpected history publication: %v", err)
			}
		})
	}
}

// Close one commit authority before publication while retaining real rooted I/O
// for every earlier metadata frontier. This injects refusal, not power loss.
type pinCommitFailureReservation struct {
	statefileEffectReservation
	failAt int
	t      *testing.T
}

func (reservation pinCommitFailureReservation) Bind(ctx context.Context) (boundStatefileEffectAuthority, error) {
	bound, err := reservation.statefileEffectReservation.Bind(ctx)
	if err != nil {
		return nil, err
	}
	return &pinCommitFailureAuthority{boundStatefileEffectAuthority: bound, failAt: reservation.failAt, t: reservation.t}, nil
}

type pinCommitFailureAuthority struct {
	boundStatefileEffectAuthority
	failAt, commits int
	t               *testing.T
}

func (authority *pinCommitFailureAuthority) Entry() *rootedpath.EntryAuthority {
	authority.commits++
	entry := authority.boundStatefileEffectAuthority.Entry()
	if authority.commits == authority.failAt {
		if err := entry.Close(); err != nil {
			authority.t.Fatalf("inject closed commit authority: %v", err)
		}
	}
	return entry
}
