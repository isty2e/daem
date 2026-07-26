package execute

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
)

func TestAdoptedCarrierClaimUsesStateOnlyRecoveryBoundary(t *testing.T) {
	for _, visible := range []bool{false, true} {
		t.Run(fmt.Sprintf("visible=%t", visible), func(t *testing.T) {
			fixture := newApplyEventFixture(t)
			_, pending := statefileCommitRelationAction(
				t,
				fixture.paths.StatefilePath,
				filepath.Join(fixture.root, "daem.toml"),
			)
			claim, err := durablecarrier.NewManagedCarrierClaim(
				pending.Owner(),
				pending.Identity(),
				pending.InstallRequest(),
				durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved,
			)
			if err != nil {
				t.Fatal(err)
			}
			input := fixture.input(nil)
			input.AdoptedProjectCarrierClaims = []durablecarrier.ManagedCarrierClaim{claim}

			_, err = ApplyWithOptions(context.Background(), input, ApplyOptions{
				commitStatefile: func(ctx context.Context, path string, content []byte, mode os.FileMode) statefileCommitOutcome {
					if visible {
						if err := commitFile(ctx, testFilesystem(), path, content, mode); err != nil {
							t.Fatalf("commit visible adopted claim: %v", err)
						}
					}
					return statefileCommitOutcome{
						status: statefileCommitIndeterminate,
						err:    errors.New("injected adoption statefile uncertainty"),
					}
				},
			})
			if err == nil || !strings.Contains(err.Error(), "statefile commit is indeterminate") {
				t.Fatalf("ApplyWithOptions error = %v, want indeterminate adoption commit", err)
			}

			recoveryPlan, planErr := journal.LoadActivePlanWithOptions(
				context.Background(),
				fixture.paths.journalPaths(),
				testPlanLoadOptions(fixture.paths),
			)
			if planErr != nil {
				t.Fatalf("LoadActivePlan: %v", planErr)
			}
			wantClass := recovery.ClassificationCleanBefore
			if visible {
				wantClass = recovery.ClassificationCleanAfter
				state, loadErr := statefile.Load(t.Context(), fixture.paths.StatefilePath)
				if loadErr != nil {
					t.Fatalf("load visible adopted claim: %v", loadErr)
				}
				claims := state.ManagedCarrierClaims()
				if len(claims) != 1 || !claims[0].ExactEqual(claim) {
					t.Fatalf("visible carrier claims = %#v, want exact adopted claim", claims)
				}
			} else if _, statErr := os.Stat(fixture.paths.StatefilePath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("invisible adoption statefile stat = %v, want absent", statErr)
			}
			if recoveryPlan.Classification() != wantClass || recoveryPlan.HasErrors() {
				t.Fatalf(
					"adoption recovery plan = %q errors=%t, want %q",
					recoveryPlan.Classification(),
					recoveryPlan.HasErrors(),
					wantClass,
				)
			}
			if guarded := recoveryPlan.GuardedActions(); len(guarded) != 0 {
				t.Fatalf("adoption recovery acquired host path authority: %#v", guarded)
			}
		})
	}
}

func TestMixedProjectionAndAdoptionShareOneStateCommitBoundary(t *testing.T) {
	newInput := func(t *testing.T, fixture *applyEventFixture) ApplyInput {
		t.Helper()
		action := fixture.createAction("create", "CREATE.md", "created\n")
		_, pending := statefileCommitRelationAction(
			t,
			fixture.paths.StatefilePath,
			filepath.Join(fixture.root, "daem.toml"),
		)
		claim, err := durablecarrier.NewManagedCarrierClaim(
			pending.Owner(),
			pending.Identity(),
			pending.InstallRequest(),
			durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved,
		)
		if err != nil {
			t.Fatal(err)
		}
		input := fixture.input([]applyEventAction{action})
		input.AdoptedProjectCarrierClaims = []durablecarrier.ManagedCarrierClaim{claim}
		return input
	}

	t.Run("commit", func(t *testing.T) {
		fixture := newApplyEventFixture(t)
		result, err := ApplyWithOptions(context.Background(), newInput(t, fixture), ApplyOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if result.ActionCount != 2 {
			t.Fatalf("ActionCount = %d, want projection plus adoption", result.ActionCount)
		}
		assertHostFileContent(t, fixture.hostPath("CREATE.md"), "created\n")
		state, err := statefile.Load(t.Context(), fixture.paths.StatefilePath)
		if err != nil {
			t.Fatal(err)
		}
		if len(state.ManagedPaths()) != 1 ||
			len(state.ManagedCarrierClaims()) != 1 ||
			state.ManagedCarrierClaims()[0].Provenance() != durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved {
			t.Fatalf("committed mixed state = %#v", state)
		}
	})

	t.Run("proven uncommitted", func(t *testing.T) {
		fixture := newApplyEventFixture(t)
		_, err := ApplyWithOptions(context.Background(), newInput(t, fixture), ApplyOptions{
			commitStatefile: func(context.Context, string, []byte, os.FileMode) statefileCommitOutcome {
				return statefileCommitOutcome{
					status: statefileUncommitted,
					err:    errors.New("injected mixed statefile failure"),
				}
			},
		})
		if err == nil || !strings.Contains(err.Error(), "host changes rolled back") {
			t.Fatalf("ApplyWithOptions error = %v, want rollback", err)
		}
		assertHostMissing(t, fixture.hostPath("CREATE.md"))
		assertHostMissing(t, fixture.paths.StatefilePath)
		assertNoActiveRecoveryOperation(t, fixture.paths.RecoveryDir)
	})
}
