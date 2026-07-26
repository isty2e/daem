package durable_test

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/target"
)

func TestSnapshotWithoutCarrierManagementRetiresExactProjectFacts(t *testing.T) {
	owner := mustStateAuthority(t, t.TempDir(), "daem.toml")
	selected := carrierFixtureFor(t, "context7", "context7@official", target.ScopeProject)
	retained := carrierFixtureFor(t, "other", "other@official", target.ScopeProject)
	selectedClaim := claimForFixture(t, selected, owner)
	retainedClaim := claimForFixture(t, retained, owner)
	selectedRemoval, err := durablecarrier.NewPendingCarrierRemoval(
		selectedClaim,
		removalRequestForTest(t, "context7"),
		relationOnlyRemovalPostconditions(),
		durablecarrier.EffectBaselineSet{},
	)
	if err != nil {
		t.Fatal(err)
	}
	current, err := durable.NewSnapshot(durable.SnapshotInput{
		PendingCarrierRemovals: []durablecarrier.PendingCarrierRemoval{selectedRemoval},
		ManagedCarrierClaims:   []durablecarrier.ManagedCarrierClaim{selectedClaim, retainedClaim},
	})
	if err != nil {
		t.Fatal(err)
	}

	next, changed, err := current.WithoutCarrierManagement(owner, selected.identity)
	if err != nil || !changed {
		t.Fatalf("WithoutCarrierManagement = (%#v, %t, %v)", next, changed, err)
	}
	if len(next.PendingCarrierRemovals()) != 0 {
		t.Fatalf("pending removals = %#v, want none", next.PendingCarrierRemovals())
	}
	claims := next.ManagedCarrierClaims()
	if len(claims) != 1 || !claims[0].ExactEqual(retainedClaim) {
		t.Fatalf("retained claims = %#v, want only unrelated claim", claims)
	}
	again, changed, err := next.WithoutCarrierManagement(owner, selected.identity)
	if err != nil || changed || !again.Equal(next) {
		t.Fatalf("idempotent retirement = (%#v, %t, %v)", again, changed, err)
	}
}

func TestSnapshotWithoutCarrierManagementRetiresGlobalPendingInstallOnly(t *testing.T) {
	owner := mustStateAuthority(t, t.TempDir(), "daem.toml")
	selected := carrierFixtureFor(t, "context7", "context7@official", target.ScopeGlobal)
	pending, err := durablecarrier.NewPendingCarrierInstall(owner, selected.identity, selected.installRequest)
	if err != nil {
		t.Fatal(err)
	}
	current, err := durable.NewSnapshot(durable.SnapshotInput{
		PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{pending},
	})
	if err != nil {
		t.Fatal(err)
	}

	next, changed, err := current.WithoutCarrierManagement(owner, selected.identity)
	if err != nil || !changed || len(next.PendingCarrierInstalls()) != 0 {
		t.Fatalf("WithoutCarrierManagement = (%#v, %t, %v)", next, changed, err)
	}
}

func TestSnapshotWithoutCarrierManagementRejectsSameRelationDrift(t *testing.T) {
	owner := mustStateAuthority(t, t.TempDir(), "daem.toml")
	selected := carrierFixtureFor(t, "context7", "context7@official", target.ScopeProject)
	replacement := carrierFixtureFor(t, "context7", "context7@other", target.ScopeProject)
	replacementClaim := claimForFixture(t, replacement, owner)
	current, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedCarrierClaims: []durablecarrier.ManagedCarrierClaim{replacementClaim},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := current.WithoutCarrierManagement(owner, selected.identity); err == nil ||
		!strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("same-relation drift error = %v", err)
	}
}

func TestSnapshotWithoutCarrierManagementRejectsOwnerProvenanceDrift(t *testing.T) {
	root := t.TempDir()
	selectedOwner := mustStateAuthority(t, root, "selected.toml")
	retainedOwner, err := durablecarrier.NewStateAuthority(
		selectedOwner.StatefileKey(),
		selectedOwner.ManifestPath()+".other",
	)
	if err != nil {
		t.Fatal(err)
	}
	selected := carrierFixtureFor(t, "context7", "context7@official", target.ScopeProject)
	retainedClaim := claimForFixture(t, selected, retainedOwner)
	current, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedCarrierClaims: []durablecarrier.ManagedCarrierClaim{retainedClaim},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := current.WithoutCarrierManagement(selectedOwner, selected.identity); err == nil ||
		!strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("owner provenance drift error = %v", err)
	}
}
