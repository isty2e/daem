package carrier_test

import (
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
)

func piPinFixture(t *testing.T, name, source string, scope target.Scope) carrierFixture {
	t.Helper()
	return carrierFixtureForSpec(t, name, desiredextension.CarrierPiPackage, target.TargetPi, scope, desiredextension.SourceKindHostSource, source, source)
}

func TestPinTransitionPreservesOldAuthorityUntilObservedCompletion(t *testing.T) {
	oldSource := "git:github.com/example/package@" + strings.Repeat("a", 40)
	newSource := "git:github.com/example/package@" + strings.Repeat("b", 40)
	before := piPinFixture(t, "tools", oldSource, target.ScopeGlobal)
	after := piPinFixture(t, "tools", newSource, target.ScopeGlobal)
	owner := mustAuthority(t, "/project", "daem.toml")
	claim := claimForFixture(t, before, owner)
	transition, err := claim.PinTransitionTo(after.contract)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := durablecarrier.NewPinTransition(claim, after.identity, before.installRequest); err == nil {
		t.Fatal("target accepted the previous pin's acquisition request")
	}
	wrongBefore, err := durablecarrier.NewManagedCarrierClaim(owner, before.identity, after.installRequest, claim.Provenance())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := durablecarrier.NewPinTransition(wrongBefore, after.identity, after.installRequest); err == nil {
		t.Fatal("transition accepted a noncanonical retained acquisition request")
	}

	reserved, err := transition.Reserve([]durablecarrier.ManagedCarrierClaim{claim})
	if err != nil {
		t.Fatal(err)
	}
	pending, exists := reserved[0].PendingPinTransition()
	if !exists || !pending.Before().ExactEqual(claim) || !pending.ExactEqual(transition) {
		t.Fatal("reservation lost prior exact management")
	}
	if reserved[0].RequireStable() == nil || reserved[0].ExactEqual(claim) || reserved[0].MatchesLockedRecord(before.contract) {
		t.Fatal("pending transition was treated as ordinary stable management")
	}
	if _, err := transition.Complete([]durablecarrier.ManagedCarrierClaim{claim}, exactCorrelation(t, after)); err == nil {
		t.Fatal("completion without retained write-ahead reservation succeeded")
	}
	if _, err := transition.Complete(reserved, exactCorrelation(t, before)); err == nil {
		t.Fatal("old observation completed new pin")
	}
	if _, err := transition.Complete(reserved, unkeyedCorrelation(t, after)); err == nil {
		t.Fatal("equivalent but inexact observation completed new pin")
	}
	completed, err := transition.Complete(reserved, exactCorrelation(t, after))
	if err != nil {
		t.Fatal(err)
	}
	if !completed[0].MatchesLockedRecord(after.contract) || completed[0].Provenance() != durablecarrier.ClaimProvenancePinTransitionObserved {
		t.Fatal("completed claim lost the target or transition provenance")
	}
	if _, pending := completed[0].PendingPinTransition(); pending {
		t.Fatal("completed claim retained reservation")
	}

	changedOwner := mustAuthority(t, "/elsewhere", "daem.toml")
	moved, err := reserved[0].WithOwner(changedOwner)
	if err != nil {
		t.Fatal(err)
	}
	movedPending, ok := moved.PendingPinTransition()
	if !ok || !movedPending.Identity().ExactEqual(after.identity) || !movedPending.Before().Owner().ExactEqual(changedOwner) {
		t.Fatal("authority transfer lost the pending pair")
	}
}

func TestPinTransitionRefusesSharedConsumerAndChangedResumeTarget(t *testing.T) {
	oldSource := "git:github.com/example/package@" + strings.Repeat("a", 40)
	newSource := "git:github.com/example/package@" + strings.Repeat("b", 40)
	before := piPinFixture(t, "tools", oldSource, target.ScopeGlobal)
	after := piPinFixture(t, "tools", newSource, target.ScopeGlobal)
	claim := claimForFixture(t, before, mustAuthority(t, "/project", "daem.toml"))
	transition, err := claim.PinTransitionTo(after.contract)
	if err != nil {
		t.Fatal(err)
	}
	foreign := claimForFixture(t, after, mustAuthority(t, "/other", "daem.toml"))
	if _, err := transition.Reserve([]durablecarrier.ManagedCarrierClaim{claim, foreign}); err == nil {
		t.Fatal("different exact pin hid a shared native checkout consumer")
	}
	pending, err := transition.PendingClaim()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pending.PinTransitionTo(before.contract); err == nil {
		t.Fatal("reverted declaration abandoned pending target")
	}
	third := piPinFixture(t, "tools", "git:github.com/example/package@"+strings.Repeat("c", 40), target.ScopeGlobal)
	if _, err := pending.PinTransitionTo(third.contract); err == nil {
		t.Fatal("new target replaced pending target")
	}
	resumed, err := pending.PinTransitionTo(after.contract)
	if err != nil || !resumed.ExactEqual(transition) {
		t.Fatalf("same-target resume = %v", err)
	}
	for _, changed := range []carrierFixture{
		piPinFixture(t, "other-id", newSource, target.ScopeGlobal),
		piPinFixture(t, "tools", newSource, target.ScopeProject),
		piPinFixture(t, "tools", "git:https://github.com/example/package@"+strings.Repeat("b", 40), target.ScopeGlobal),
	} {
		if _, err := claim.PinTransitionTo(changed.contract); err == nil {
			t.Fatal("non-pin contract change was admitted")
		}
	}
}
