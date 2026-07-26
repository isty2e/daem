package durable_test

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/target"
)

func TestSnapshotPreparedProjectCarrierRemovalRequiresExactClaim(t *testing.T) {
	owner := mustStateAuthority(t, t.TempDir(), "daem.toml")
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeProject)
	claim := claimForFixture(t, fixture, owner)
	pending, err := durablecarrier.NewPendingCarrierRemoval(
		claim,
		removalRequestForTest(t, "context7"),
		relationOnlyRemovalPostconditions(),
		durablecarrier.EffectBaselineSet{},
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := durable.NewSnapshot(durable.SnapshotInput{
		PendingCarrierRemovals: []durablecarrier.PendingCarrierRemoval{pending},
	}); err == nil || !strings.Contains(err.Error(), "requires its exact managed claim") {
		t.Fatalf("orphan project removal error = %v", err)
	}

	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		PendingCarrierRemovals: []durablecarrier.PendingCarrierRemoval{pending},
		ManagedCarrierClaims:   []durablecarrier.ManagedCarrierClaim{claim},
	})
	if err != nil {
		t.Fatalf("exact project removal rejected: %v", err)
	}
	if len(snapshot.PendingCarrierRemovals()) != 1 {
		t.Fatal("exact project removal was not retained")
	}
}

func TestSnapshotRejectsConcurrentInstallAndRemovalForOneRelation(t *testing.T) {
	owner := mustStateAuthority(t, t.TempDir(), "daem.toml")
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeProject)
	claim := claimForFixture(t, fixture, owner)
	install, err := durablecarrier.NewPendingCarrierInstall(owner, fixture.identity, fixture.installRequest)
	if err != nil {
		t.Fatal(err)
	}
	removal, err := durablecarrier.NewPendingCarrierRemoval(
		claim,
		removalRequestForTest(t, "context7"),
		relationOnlyRemovalPostconditions(),
		durablecarrier.EffectBaselineSet{},
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := durable.NewSnapshot(durable.SnapshotInput{
		PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{install},
		PendingCarrierRemovals: []durablecarrier.PendingCarrierRemoval{removal},
		ManagedCarrierClaims:   []durablecarrier.ManagedCarrierClaim{claim},
	}); err == nil || !strings.Contains(err.Error(), "conflicts with pending install") {
		t.Fatalf("concurrent transition error = %v", err)
	}
}

func TestSnapshotPreparedCarrierRemovalIsExactAndIdempotent(t *testing.T) {
	owner := mustStateAuthority(t, t.TempDir(), "daem.toml")
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeProject)
	claim := claimForFixture(t, fixture, owner)
	current, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedCarrierClaims: []durablecarrier.ManagedCarrierClaim{claim},
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := durablecarrier.NewPendingCarrierRemoval(
		claim,
		removalRequestForTest(t, "context7"),
		relationOnlyRemovalPostconditions(),
		durablecarrier.EffectBaselineSet{},
	)
	if err != nil {
		t.Fatal(err)
	}

	next, changed, err := current.WithPreparedCarrierRemovals(
		[]durablecarrier.PendingCarrierRemoval{pending},
		durablecarrier.EmptyGlobalCarrierClaims(),
	)
	if err != nil || !changed {
		t.Fatalf("WithPreparedCarrierRemovals = (%#v, %t, %v)", next, changed, err)
	}
	again, changed, err := next.WithPreparedCarrierRemovals(
		[]durablecarrier.PendingCarrierRemoval{pending},
		durablecarrier.EmptyGlobalCarrierClaims(),
	)
	if err != nil || changed || !again.Equal(next) {
		t.Fatalf("idempotent prepare = (%#v, %t, %v)", again, changed, err)
	}

	conflicting, err := durablecarrier.NewPendingCarrierRemoval(
		claim,
		removalRequestForTest(t, "changed"),
		relationOnlyRemovalPostconditions(),
		durablecarrier.EffectBaselineSet{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := next.WithPreparedCarrierRemovals(
		[]durablecarrier.PendingCarrierRemoval{conflicting},
		durablecarrier.EmptyGlobalCarrierClaims(),
	); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("conflicting prepare error = %v", err)
	}
}

func TestSnapshotRetiresProjectClaimAndPendingRemovalAtomically(t *testing.T) {
	owner := mustStateAuthority(t, t.TempDir(), "daem.toml")
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeProject)
	claim := claimForFixture(t, fixture, owner)
	pending, err := durablecarrier.NewPendingCarrierRemoval(
		claim,
		removalRequestForTest(t, "context7"),
		relationOnlyRemovalPostconditions(),
		durablecarrier.EffectBaselineSet{},
	)
	if err != nil {
		t.Fatal(err)
	}
	current, err := durable.NewSnapshot(durable.SnapshotInput{
		PendingCarrierRemovals: []durablecarrier.PendingCarrierRemoval{pending},
		ManagedCarrierClaims:   []durablecarrier.ManagedCarrierClaim{claim},
	})
	if err != nil {
		t.Fatal(err)
	}

	next, changed, err := current.WithRetiredProjectCarrierRemoval(pending)
	if err != nil || !changed {
		t.Fatalf("WithRetiredProjectCarrierRemoval = (%#v, %t, %v)", next, changed, err)
	}
	if len(next.PendingCarrierRemovals()) != 0 || len(next.ManagedCarrierClaims()) != 0 {
		t.Fatalf("retired snapshot retained removal authority: %#v", next)
	}
}

func TestSnapshotAllowsGlobalPendingRemovalWithoutLocalClaim(t *testing.T) {
	owner := mustStateAuthority(t, t.TempDir(), "daem.toml")
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeGlobal)
	claim := claimForFixture(t, fixture, owner)
	pending, err := durablecarrier.NewPendingCarrierRemoval(
		claim,
		removalRequestForTest(t, "context7"),
		relationOnlyRemovalPostconditions(),
		durablecarrier.EffectBaselineSet{},
	)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		PendingCarrierRemovals: []durablecarrier.PendingCarrierRemoval{pending},
	})
	if err != nil {
		t.Fatalf("global pending removal rejected: %v", err)
	}
	if _, _, err := durable.EmptySnapshot().WithPreparedCarrierRemovals(
		[]durablecarrier.PendingCarrierRemoval{pending},
		durablecarrier.EmptyGlobalCarrierClaims(),
	); err == nil || !strings.Contains(err.Error(), "no exact active claim") {
		t.Fatalf("unowned global prepare error = %v", err)
	}
	registry, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{claim})
	if err != nil {
		t.Fatal(err)
	}
	prepared, changed, err := durable.EmptySnapshot().WithPreparedCarrierRemovals(
		[]durablecarrier.PendingCarrierRemoval{pending},
		registry,
	)
	if err != nil || !changed || len(prepared.PendingCarrierRemovals()) != 1 {
		t.Fatalf("global prepare = (%#v, %t, %v)", prepared, changed, err)
	}
	next, changed, err := snapshot.WithoutPendingCarrierRemoval(pending)
	if err != nil || !changed || len(next.PendingCarrierRemovals()) != 0 {
		t.Fatalf("WithoutPendingCarrierRemoval = (%#v, %t, %v)", next, changed, err)
	}
}
