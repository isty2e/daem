package statefile

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	lock "github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestSnapshotPinTransitionRoundTripTransferAndRetirementFence(t *testing.T) {
	before := testPiPinContract(t, "git:github.com/example/package@"+strings.Repeat("a", 40))
	after := testPiPinContract(t, "git:github.com/example/package@"+strings.Repeat("b", 40))
	identity, _, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(before)
	if err != nil {
		t.Fatal(err)
	}
	request, err := lock.DelegatedOperationRequest(before, lock.OperationInstall)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	owner, err := stateauthority.New(pathtest.Exact(filepath.Join(root, "state.json")), filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	claim, err := durablecarrier.NewManagedCarrierClaim(owner, identity, request, durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved)
	if err != nil {
		t.Fatal(err)
	}
	transition, err := claim.PinTransitionTo(after)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{ManagedCarrierClaims: []durablecarrier.ManagedCarrierClaim{claim}})
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := snapshot.WithReservedCarrierPinTransition(transition)
	if err != nil {
		t.Fatal(err)
	}
	content, err := Marshal(reserved)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(content)
	if err != nil {
		t.Fatal(err)
	}
	pending := decoded.ManagedCarrierClaims()[0]
	pair, present := pending.PendingPinTransition()
	if !present || !pair.ExactEqual(transition) || !pair.Before().ExactEqual(claim) {
		t.Fatal("state codec discarded the reserved target or prior acquisition")
	}
	if _, _, err := decoded.WithoutManagedCarrierClaim(pending); err == nil {
		t.Fatal("ordinary claim retirement erased a pending transition")
	}
	if _, _, err := decoded.WithoutCarrierManagement(owner, identity); err == nil {
		t.Fatal("unmanage erased a pending transition")
	}
	install, err := durablecarrier.NewPendingCarrierInstall(owner, identity, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := decoded.WithPreparedCarrierInstalls([]durablecarrier.PendingCarrierInstall{install}); err == nil {
		t.Fatal("ordinary install overlapped a pending transition")
	}

	newOwner, err := owner.WithStatefile(pathtest.Exact(filepath.Join(root, "moved", "state.json")))
	if err != nil {
		t.Fatal(err)
	}
	moved, err := decoded.TransferAuthority(owner, newOwner)
	if err != nil {
		t.Fatal(err)
	}
	movedContent, err := Marshal(moved)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := Decode(movedContent)
	if err != nil {
		t.Fatal(err)
	}
	movedPair, present := reloaded.ManagedCarrierClaims()[0].PendingPinTransition()
	if !present || !movedPair.Identity().ExactEqual(pair.Identity()) || !movedPair.Before().Owner().ExactEqual(newOwner) || movedPair.Before().Provenance() != claim.Provenance() {
		t.Fatal("authority transfer changed or discarded pending facts")
	}
}

func testPiPinContract(t *testing.T, sourceValue string) lock.LockedSubjectContract {
	t.Helper()
	source, err := desiredextension.NewSourceRef(desiredextension.SourceKindHostSource, sourceValue)
	if err != nil {
		t.Fatal(err)
	}
	value, err := desiredextension.New(desiredextension.Spec{Name: "tools", Carrier: desiredextension.CarrierPiPackage, Target: target.TargetPi, Scope: target.ScopeProject, Source: source})
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
	return contract
}
