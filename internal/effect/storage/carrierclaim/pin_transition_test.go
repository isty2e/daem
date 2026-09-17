package carrierclaim

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/effect/mutation"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	lock "github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestStorePinTransitionRetainsReservationAcrossFailedCompletion(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	claim, _ := testPiPinClaim(t, root, "tools", strings.Repeat("a", 40))
	_, nextContract := testPiPinClaim(t, root, "tools", strings.Repeat("b", 40))
	transition, err := claim.PinTransitionTo(nextContract)
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(filepath.Join(root, "claims.json"))
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := store.Upsert(ctx, claim)
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := store.ReservePinTransitionIfCurrent(ctx, baseline, transition)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(ctx)
	if err != nil || !loaded.Equal(reserved) {
		t.Fatalf("reservation round trip = %v", err)
	}
	pair, present := loaded.Claims()[0].PendingPinTransition()
	if !present || !pair.ExactEqual(transition) {
		t.Fatal("global codec lost the pin reservation")
	}
	if _, err := store.ReservePinTransitionIfCurrent(ctx, baseline, transition); err == nil {
		t.Fatal("stale registry baseline was accepted")
	}
	repeated, err := store.ReservePinTransitionIfCurrent(ctx, reserved, transition)
	if err != nil || !repeated.Equal(reserved) {
		t.Fatalf("repeat reservation = %v", err)
	}
	other, _ := testPiPinClaim(t, root, "consumer", strings.Repeat("b", 40))
	if _, err := store.Upsert(ctx, other); err == nil {
		t.Fatal("another declaration acquired the reserved native footprint")
	}
	if _, err := store.Remove(ctx, reserved.Claims()[0]); err == nil {
		t.Fatal("retirement erased uncertain native effects")
	}
	if _, err := store.RetireAllIfCurrent(ctx, reserved, reserved.Claims()); err == nil {
		t.Fatal("batch retirement erased uncertain native effects")
	}

	relation := transition.Identity().ExpectedRelation()
	row, err := observerelation.NewRow(observerelation.RowSpec{SubjectKey: string(relation.SubjectKey()), HasManagedInstanceKey: true, ManagedInstanceKey: string(relation.ManagedInstanceKey())})
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := observerelation.NewInventory(observerelation.InventorySpec{Availability: observerelation.InventorySupported, Freshness: observerelation.EvidenceFresh, Rows: []observerelation.Row{row}})
	if err != nil {
		t.Fatal(err)
	}
	correlation := observerelation.Correlate(relation, inventory)
	fault := errors.New("injected claim publication failure")
	broken := store
	broken.commitFile = func(context.Context, storagecommit.FileCommit) error { return fault }
	if _, err := broken.CompletePinTransitionIfCurrent(ctx, reserved, transition, correlation); !errors.Is(err, fault) {
		t.Fatalf("completion fault = %v", err)
	}
	loaded, err = store.Load(ctx)
	if err != nil || !loaded.Equal(reserved) {
		t.Fatalf("failed completion lost durable reservation: %v", err)
	}
	completed, err := store.CompletePinTransitionIfCurrent(ctx, loaded, transition, correlation)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err = store.Load(ctx)
	if err != nil || !loaded.Equal(completed) || !loaded.Claims()[0].MatchesLockedRecord(nextContract) {
		t.Fatalf("completed management round trip = %v", err)
	}
	if loaded.Claims()[0].Provenance() != durablecarrier.ClaimProvenancePinTransitionObserved {
		t.Fatal("completion reused absent-before installation provenance")
	}
}

func testPiPinClaim(t *testing.T, root, name, pin string) (durablecarrier.ManagedCarrierClaim, lock.LockedSubjectContract) {
	t.Helper()
	sourceValue := "git:github.com/example/package@" + pin
	source, err := desiredextension.NewSourceRef(desiredextension.SourceKindHostSource, sourceValue)
	if err != nil {
		t.Fatal(err)
	}
	value, err := desiredextension.New(desiredextension.Spec{Name: name, Carrier: desiredextension.CarrierPiPackage, Target: target.TargetPi, Scope: target.ScopeGlobal, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	subject, err := extensiontopology.Relation(value)
	if err != nil {
		t.Fatal(err)
	}
	key, err := hostrelation.NewSubjectKey(sourceValue)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := lock.NewDelegatedRelationCarrierContract(value.ID(), value.CarrierKey(), subject, key)
	if err != nil {
		t.Fatal(err)
	}
	identity, _, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(contract)
	if err != nil {
		t.Fatal(err)
	}
	request, err := lock.DelegatedOperationRequest(contract, lock.OperationInstall)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := mutation.ObservePersistedDirectoryEntryAuthority(filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(entry.Exact(), filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	claim, err := durablecarrier.NewManagedCarrierClaim(owner, identity, request, durablecarrier.ClaimProvenanceInstalledObserved)
	if err != nil {
		t.Fatal(err)
	}
	return claim, contract
}
