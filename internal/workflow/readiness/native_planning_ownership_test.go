package readiness

import (
	"bytes"
	"context"
	"os"
	"testing"

	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"

	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
)

func nativePlanningKey(t *testing.T, names ...string) output.Footprint {
	t.Helper()
	key, err := output.NewKeyPath(names...)
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	return footprint
}

func TestNativeSourcePreparationJoinsOnlySuccessfulWholeAdoptions(t *testing.T) {
	for _, variant := range []string{"matched", "no consent", "foreign claim", "reserved claim"} {
		t.Run(variant, func(t *testing.T) {
			fixture := newNativePlanningFixture(t, target.ScopeGlobal, "extension-settings/test.json")
			if variant == "foreign claim" || variant == "reserved claim" {
				fixture.persistBefore(t, fixture.observation.Before, []ownership.Claim{fixture.foreignClaim(t, variant == "reserved claim")})
			}
			fixture.input.ManageUnmanagedMatches = variant != "no consent"
			before := fixture.input.Documents.Evidence().BeforeState()
			reads, remaining := fixture.reader.reads, fixture.observation.Budget.RemainingBytes()
			prepared, err := PrepareNativeSources(t.Context(), fixture.input)
			if err != nil || prepared.Validate() != nil || len(prepared.WholeAdoptions()) != 1 {
				t.Fatal("source partition did not join its real whole-adoption decision", err)
			}
			adoption := prepared.WholeAdoptions()[0]
			if variant == "matched" {
				if adoption.Decision().Kind() != reconcile.ManagedPathRecord || len(adoption.RetiredSubjects()) != 1 || len(prepared.RemainingManagedAggregates()) != 0 {
					t.Fatal("matched adoption sent retired ownership through generic key deletion")
				}
			} else if !adoption.Decision().IsBlocked() || len(adoption.RetiredSubjects()) != 0 || len(prepared.RemainingManagedAggregates()) != 1 || !prepared.RemainingManagedAggregates()[0].Equal(before.ManagedAggregates()[0]) {
				t.Fatal("blocked adoption consumed its prior ownership baseline")
			}
			if !fixture.input.Documents.Evidence().BeforeState().Equal(before) || fixture.reader.reads != reads || fixture.observation.Budget.RemainingBytes() != remaining || fixture.observation.Budget.RequireCapturePending() != nil {
				t.Fatal("source partition replaced complete Before, reread ownership or advanced capture")
			}
		})
	}
}

func TestNativeWholeProducerUsesActualBeforeWithoutHostMutation(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, oldKeyDrift := range []bool{false, true} {
			t.Run(string(scope)+map[bool]string{false: "/unchanged", true: "/old-key-drift"}[oldKeyDrift], func(t *testing.T) {
				fixture := newNativePlanningFixture(t, scope, "extension-settings/test.json")
				if oldKeyDrift {
					fixture.original = []byte(`{"a":99,"released":9}`)
					requireNativePreparationFile(t, fixture.hostPath, fixture.original)
					requireNativePreparationFile(t, fixture.sourcePath, fixture.original)
					fixture.refreshEpoch(t)
					fixture.observe(t)
				}
				registryBefore, err := os.ReadFile(fixture.registryPath)
				if err != nil {
					t.Fatal(err)
				}
				input := fixture.adoption(t, true)
				reads, remaining := fixture.reader.reads, fixture.observation.Budget.RemainingBytes()
				plan, err := PrepareNativeWholeAdoption(t.Context(), input)
				if err != nil || plan.Validate() != nil || plan.Decision().Kind() != reconcile.ManagedPathRecord || len(plan.RetiredSubjects()) != 1 {
					t.Fatal("actual qualified source did not produce exact Record adoption", err)
				}
				state, err := effectpreparation.PrepareNativeApplyState(t.Context(), effectpreparation.NativeApplyStateInput{
					Before: fixture.observation.Before, ManagedPaths: []reconcile.ManagedPathDecision{plan.Decision()},
					WholeSources: []lock.LockedSubjectContract{input.Source.SourceContract()}, StateCodec: fixture.codec,
				})
				if err != nil || len(state.AfterState().ManagedAggregates()) != 0 || len(state.AfterState().ManagedPaths()) != 1 || len(state.AggregateEffects()) != 0 || len(state.WholeAdoptions()) != 1 || len(state.WholeAdoptions()[0].RetiredKeys()) != 1 {
					t.Fatal("actual Record did not retire only its real keys", err)
				}
				if len(state.ManagedPathEffects()) != 1 || state.ManagedPathEffects()[0].Kind() != effectpreparation.ManagedPathEffectRecord {
					t.Fatal("adoption became a content replacement or removal")
				}
				retired := plan.RetiredSubjects()
				retired[0] = plan.Decision().Subject()
				if plan.RetiredSubjects()[0] != fixture.observation.Before.ManagedAggregates()[0].Subject() {
					t.Fatal("retirement list was mutable through a returned slice")
				}
				hostAfter, hostErr := os.ReadFile(fixture.hostPath)
				registryAfter, registryErr := os.ReadFile(fixture.registryPath)
				if hostErr != nil || registryErr != nil || !bytes.Equal(hostAfter, fixture.original) || !bytes.Equal(registryBefore, registryAfter) || fixture.reader.reads != reads || fixture.observation.Budget.RemainingBytes() != remaining || fixture.observation.Budget.RequireCapturePending() != nil {
					t.Fatal("read-only adoption mutated files, reread Registry or changed the allocation")
				}
			})
		}
	}
}

func TestNativeWholeProducerRefusesForcedAdoptionAndLaterBlockers(t *testing.T) {
	for _, fault := range []string{"no consent", "different bytes", "mode mismatch", "missing file", "foreign later", "reserved later"} {
		t.Run(fault, func(t *testing.T) {
			fixture := newNativePlanningFixture(t, target.ScopeGlobal, "extension-settings/test.json")
			switch fault {
			case "different bytes":
				requireNativePreparationFile(t, fixture.hostPath, []byte("{ \"released\":9, \"a\":1 }"))
				fixture.observe(t)
			case "mode mismatch":
				if err := os.Chmod(fixture.hostPath, 0o644); err != nil {
					t.Fatal(err)
				}
				fixture.observe(t)
			case "missing file":
				if err := os.Remove(fixture.hostPath); err != nil {
					t.Fatal(err)
				}
				fixture.observe(t)
			case "foreign later", "reserved later":
				claim := fixture.foreignClaim(t, fault == "reserved later")
				fixture.persistBefore(t, fixture.observation.Before, []ownership.Claim{claim})
				projection, err := fixture.input.Documents.Evidence().Ownership(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				whole, err := projection.Document(t.Context(), fixture.observation.Aliases[0])
				if err != nil || len(whole.CurrentClaims()) != 2 || !whole.CurrentClaims()[0].OwnedBy(fixture.observation.Owner) || whole.CurrentClaims()[1].OwnedBy(fixture.observation.Owner) {
					t.Fatal("fixture did not put the allowed own claim before the blocker", err)
				}
			}
			plan, err := PrepareNativeWholeAdoption(t.Context(), fixture.adoption(t, fault != "no consent"))
			if err != nil || plan.Validate() != nil || !plan.Decision().IsBlocked() || len(plan.RetiredSubjects()) != 0 {
				t.Fatal("native widening forced creation/replacement or ignored ownership", err)
			}
			if state, err := effectpreparation.PrepareNativeApplyState(t.Context(), effectpreparation.NativeApplyStateInput{Before: fixture.observation.Before, ManagedPaths: []reconcile.ManagedPathDecision{plan.Decision()}, WholeSources: []lock.LockedSubjectContract{fixture.adoption(t, true).Source.SourceContract()}, StateCodec: fixture.codec}); err == nil || state.Validate() == nil {
				t.Fatal("blocked adoption exposed usable successor state")
			}
		})
	}
}

func TestNativeOwnershipProjectionRetainsHistoricalMissingScopes(t *testing.T) {
	fixture := newNativePlanningFixture(t, target.ScopeGlobal, "extension-settings/Café.json")
	if err := os.Remove(fixture.hostPath); err != nil {
		t.Fatal(err)
	}
	fixture.observe(t)
	projection, err := fixture.input.Documents.Evidence().Ownership(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	alias := fixture.observation.Aliases[0]
	old, err := projection.Global(t.Context(), alias, nativePlanningKey(t, "a"))
	if err != nil || !old.CorrelatesClaimAddress() {
		t.Fatal("missing historical ownership failed actual projection", err)
	}
	if _, provisional := old.ProvisionalPath(); !provisional {
		t.Skip("host observes exact namespace for this missing path")
	}
	if _, exact := old.ExactAddress(); exact {
		t.Fatal("native planning turned retained history into current exact authority")
	}
	newKey, err := projection.Global(t.Context(), alias, nativePlanningKey(t, "new"))
	if _, claimed := newKey.Claim().Get(); err != nil || claimed || newKey.CorrelatesClaimAddress() {
		t.Fatal("disjoint new key borrowed the old key's claim", err)
	}
	if _, err := fixture.observation.Registry.Load(t.Context()); err == nil {
		t.Fatal("ordinary strict Registry reading was widened for native history")
	}
}

func TestNativeOwnershipProjectionRefusesUnknownOrInactiveQueries(t *testing.T) {
	fixture := newNativePlanningFixture(t, target.ScopeGlobal, "extension-settings/test.json")
	projection, err := fixture.input.Documents.Evidence().Ownership(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projection.Document(t.Context(), aggregate.DocumentAddress{}); err == nil {
		t.Fatal("unknown alias obtained whole ownership facts")
	}
	if _, err := projection.Global(t.Context(), aggregate.DocumentAddress{}, nativePlanningKey(t, "a")); err == nil {
		t.Fatal("unknown alias became NoClaim")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := projection.Document(ctx, fixture.observation.Aliases[0]); err == nil {
		t.Fatal("canceled projection yielded usable whole claims")
	}
	if err := fixture.observation.Budget.AdmitRemovalDemands(recovery.RemovalDemandSet{}); err != nil {
		t.Fatal(err)
	}
	if _, err := projection.Global(t.Context(), fixture.observation.Aliases[0], nativePlanningKey(t, "a")); err == nil {
		t.Fatal("admitted allocation yielded new planning observations")
	}
	if result, err := PrepareNativeWholeAdoption(t.Context(), fixture.adoption(t, true)); err == nil || result.Validate() == nil {
		t.Fatal("inactive capture phase yielded a whole adoption")
	}
	var empty journalobservation.OwnershipProjection
	if _, err := empty.Document(t.Context(), fixture.observation.Aliases[0]); err == nil {
		t.Fatal("zero ownership projection was usable")
	}
}
