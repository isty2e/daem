package durable_test

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/target"
)

func TestSnapshotCanonicalizesProjectCarrierFacts(t *testing.T) {
	root := t.TempDir()
	owner := mustStateAuthority(t, root, "daem.toml")
	alpha := carrierFixtureFor(t, "alpha", "alpha@official", target.ScopeProject)
	beta := carrierFixtureFor(t, "beta", "beta@official", target.ScopeProject)

	pending, err := durablecarrier.NewPendingCarrierInstall(owner, beta.identity, beta.installRequest)
	if err != nil {
		t.Fatalf("NewPendingCarrierInstall returned error: %v", err)
	}
	claim := claimForFixture(t, alpha, owner)
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{pending},
		ManagedCarrierClaims:   []durablecarrier.ManagedCarrierClaim{claim},
	})
	if err != nil {
		t.Fatalf("NewSnapshot returned error: %v", err)
	}
	if len(snapshot.PendingCarrierInstalls()) != 1 ||
		len(snapshot.ManagedCarrierClaims()) != 1 {
		t.Fatalf("carrier facts were not retained: %#v", snapshot)
	}
	if !snapshot.ManagedCarrierClaims()[0].ExactEqual(claim) {
		t.Fatal("project carrier claim changed during canonicalization")
	}
}

func TestSnapshotRejectsGlobalClaimAndAllowsExactReinstallOverlap(t *testing.T) {
	root := t.TempDir()
	owner := mustStateAuthority(t, root, "daem.toml")
	global := carrierFixtureFor(t, "global", "global@official", target.ScopeGlobal)
	globalClaim := claimForFixture(t, global, owner)
	if _, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedCarrierClaims: []durablecarrier.ManagedCarrierClaim{globalClaim},
	}); err == nil || !strings.Contains(err.Error(), "global carrier registry") {
		t.Fatalf("global claim error = %v", err)
	}

	project := carrierFixtureFor(t, "project", "project@official", target.ScopeProject)
	pending, err := durablecarrier.NewPendingCarrierInstall(owner, project.identity, project.installRequest)
	if err != nil {
		t.Fatalf("NewPendingCarrierInstall returned error: %v", err)
	}
	projectClaim := claimForFixture(t, project, owner)
	overlap, err := durable.NewSnapshot(durable.SnapshotInput{
		PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{pending},
		ManagedCarrierClaims:   []durablecarrier.ManagedCarrierClaim{projectClaim},
	})
	if err != nil {
		t.Fatalf("exact reinstall overlap rejected: %v", err)
	}
	if len(overlap.PendingCarrierInstalls()) != 1 ||
		len(overlap.ManagedCarrierClaims()) != 1 {
		t.Fatalf("exact reinstall overlap not retained: %#v", overlap)
	}
}

func TestSnapshotRejectsForeignCarrierAuthority(t *testing.T) {
	root := t.TempDir()
	firstOwner := mustStateAuthority(t, root, "first.toml")
	secondOwner := mustStateAuthority(t, t.TempDir(), "second.toml")
	first := carrierFixtureFor(t, "first", "first@official", target.ScopeProject)
	second := carrierFixtureFor(t, "second", "second@official", target.ScopeProject)
	firstPending, err := durablecarrier.NewPendingCarrierInstall(
		firstOwner,
		first.identity,
		first.installRequest,
	)
	if err != nil {
		t.Fatalf("NewPendingCarrierInstall returned error: %v", err)
	}
	secondPending, err := durablecarrier.NewPendingCarrierInstall(
		secondOwner,
		second.identity,
		second.installRequest,
	)
	if err != nil {
		t.Fatalf("NewPendingCarrierInstall returned error: %v", err)
	}
	if _, err := durable.NewSnapshot(durable.SnapshotInput{
		PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{firstPending, secondPending},
	}); err == nil || !strings.Contains(err.Error(), "foreign state authority") {
		t.Fatalf("foreign owner error = %v", err)
	}
}

func TestSnapshotPreparedCarrierInstallIsExactAndIdempotent(t *testing.T) {
	root := t.TempDir()
	owner := mustStateAuthority(t, root, "daem.toml")
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeProject)
	pending, err := durablecarrier.NewPendingCarrierInstall(owner, fixture.identity, fixture.installRequest)
	if err != nil {
		t.Fatal(err)
	}
	next, changed, err := durable.EmptySnapshot().WithPreparedCarrierInstalls(
		[]durablecarrier.PendingCarrierInstall{pending},
	)
	if err != nil || !changed {
		t.Fatalf("WithPreparedCarrierInstalls = (%#v, %t, %v)", next, changed, err)
	}
	again, changed, err := next.WithPreparedCarrierInstalls(
		[]durablecarrier.PendingCarrierInstall{pending},
	)
	if err != nil || changed || !again.Equal(next) {
		t.Fatalf("idempotent prepare = (%#v, %t, %v)", again, changed, err)
	}

	otherOwner, err := durablecarrier.NewStateAuthority(owner.StatefileKey(), root+"/other.toml")
	if err != nil {
		t.Fatal(err)
	}
	conflict, err := durablecarrier.NewPendingCarrierInstall(
		otherOwner,
		fixture.identity,
		fixture.installRequest,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := next.WithPreparedCarrierInstalls(
		[]durablecarrier.PendingCarrierInstall{conflict},
	); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("conflicting prepare error = %v", err)
	}
}

func TestSnapshotRetiresOnlyExactCompletedPendingCarrierInstall(t *testing.T) {
	root := t.TempDir()
	owner := mustStateAuthority(t, root, "daem.toml")
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeProject)
	pending, err := durablecarrier.NewPendingCarrierInstall(owner, fixture.identity, fixture.installRequest)
	if err != nil {
		t.Fatal(err)
	}
	current, _, err := durable.EmptySnapshot().WithPreparedCarrierInstalls(
		[]durablecarrier.PendingCarrierInstall{pending},
	)
	if err != nil {
		t.Fatal(err)
	}
	next, changed, err := current.WithoutPendingCarrierInstall(pending)
	if err != nil || !changed || len(next.PendingCarrierInstalls()) != 0 {
		t.Fatalf("WithoutPendingCarrierInstall = (%#v, %t, %v)", next, changed, err)
	}
	again, changed, err := next.WithoutPendingCarrierInstall(pending)
	if err != nil || changed || !again.Equal(next) {
		t.Fatalf("idempotent retire = (%#v, %t, %v)", again, changed, err)
	}

	otherOwner, err := durablecarrier.NewStateAuthority(owner.StatefileKey(), root+"/other.toml")
	if err != nil {
		t.Fatal(err)
	}
	conflict, err := durablecarrier.NewPendingCarrierInstall(
		otherOwner,
		fixture.identity,
		fixture.installRequest,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := current.WithoutPendingCarrierInstall(conflict); err == nil ||
		!strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("conflicting retire error = %v", err)
	}
}

func TestSnapshotPromotesOnlyExactPendingProjectClaim(t *testing.T) {
	root := t.TempDir()
	owner := mustStateAuthority(t, root, "daem.toml")
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeProject)
	pending, err := durablecarrier.NewPendingCarrierInstall(owner, fixture.identity, fixture.installRequest)
	if err != nil {
		t.Fatal(err)
	}
	claim := claimForFixture(t, fixture, owner)
	current, _, err := durable.EmptySnapshot().WithPreparedCarrierInstalls(
		[]durablecarrier.PendingCarrierInstall{pending},
	)
	if err != nil {
		t.Fatal(err)
	}
	next, changed, err := current.WithPromotedCarrierClaims(
		[]durablecarrier.ManagedCarrierClaim{claim},
	)
	if err != nil || !changed {
		t.Fatalf("WithPromotedCarrierClaims = (%#v, %t, %v)", next, changed, err)
	}
	if len(next.PendingCarrierInstalls()) != 0 ||
		len(next.ManagedCarrierClaims()) != 1 ||
		!next.ManagedCarrierClaims()[0].ExactEqual(claim) {
		t.Fatalf("promoted snapshot = %#v", next)
	}
	if _, _, err := durable.EmptySnapshot().WithPromotedCarrierClaims(
		[]durablecarrier.ManagedCarrierClaim{claim},
	); err == nil || !strings.Contains(err.Error(), "no exact pending") {
		t.Fatalf("orphan promotion error = %v", err)
	}
}

func TestSnapshotAdoptsOnlyExplicitProjectClaimWithoutPendingAcquisition(t *testing.T) {
	root := t.TempDir()
	owner := mustStateAuthority(t, root, "daem.toml")
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeProject)
	adopted, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		fixture.identity,
		fixture.installRequest,
		durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved,
	)
	if err != nil {
		t.Fatal(err)
	}

	next, changed, err := durable.EmptySnapshot().WithAdoptedCarrierClaims(
		[]durablecarrier.ManagedCarrierClaim{adopted},
	)
	if err != nil || !changed {
		t.Fatalf("WithAdoptedCarrierClaims = (%#v, %t, %v)", next, changed, err)
	}
	if claims := next.ManagedCarrierClaims(); len(claims) != 1 || !claims[0].ExactEqual(adopted) {
		t.Fatalf("adopted claims = %#v", claims)
	}
	again, changed, err := next.WithAdoptedCarrierClaims(
		[]durablecarrier.ManagedCarrierClaim{adopted},
	)
	if err != nil || changed || !again.Equal(next) {
		t.Fatalf("idempotent adoption = (%#v, %t, %v)", again, changed, err)
	}

	installed := claimForFixture(t, fixture, owner)
	if _, _, err := durable.EmptySnapshot().WithAdoptedCarrierClaims(
		[]durablecarrier.ManagedCarrierClaim{installed},
	); err == nil || !strings.Contains(err.Error(), "explicit-adoption provenance") {
		t.Fatalf("installed-provenance adoption error = %v", err)
	}
	if _, _, err := next.WithAdoptedCarrierClaims(
		[]durablecarrier.ManagedCarrierClaim{installed},
	); err == nil || !strings.Contains(err.Error(), "explicit-adoption provenance") {
		t.Fatalf("retained provenance conflict error = %v", err)
	}
	if _, _, err := durable.EmptySnapshot().WithAdoptedCarrierClaims(
		[]durablecarrier.ManagedCarrierClaim{adopted, adopted},
	); err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("duplicate adoption error = %v", err)
	}

	pending, err := durablecarrier.NewPendingCarrierInstall(owner, fixture.identity, fixture.installRequest)
	if err != nil {
		t.Fatal(err)
	}
	pendingOnly, _, err := durable.EmptySnapshot().WithPreparedCarrierInstalls(
		[]durablecarrier.PendingCarrierInstall{pending},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := pendingOnly.WithAdoptedCarrierClaims(
		[]durablecarrier.ManagedCarrierClaim{adopted},
	); err == nil || !strings.Contains(err.Error(), "pending acquisition") {
		t.Fatalf("pending-only adoption error = %v", err)
	}
	reinstall, err := durable.NewSnapshot(durable.SnapshotInput{
		PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{pending},
		ManagedCarrierClaims:   []durablecarrier.ManagedCarrierClaim{adopted},
	})
	if err != nil {
		t.Fatal(err)
	}
	unchanged, changed, err := reinstall.WithAdoptedCarrierClaims(
		[]durablecarrier.ManagedCarrierClaim{adopted},
	)
	if err != nil || changed || !unchanged.Equal(reinstall) {
		t.Fatalf("adopted reinstall retry = (%#v, %t, %v)", unchanged, changed, err)
	}
}

func TestSnapshotRetiresOnlyExactProjectCarrierClaim(t *testing.T) {
	root := t.TempDir()
	owner := mustStateAuthority(t, root, "daem.toml")
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeProject)
	claim := claimForFixture(t, fixture, owner)
	current, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedCarrierClaims: []durablecarrier.ManagedCarrierClaim{claim},
	})
	if err != nil {
		t.Fatal(err)
	}

	next, changed, err := current.WithoutManagedCarrierClaim(claim)
	if err != nil || !changed || len(next.ManagedCarrierClaims()) != 0 {
		t.Fatalf("WithoutManagedCarrierClaim = (%#v, %t, %v)", next, changed, err)
	}
	again, changed, err := next.WithoutManagedCarrierClaim(claim)
	if err != nil || changed || !again.Equal(next) {
		t.Fatalf("idempotent retirement = (%#v, %t, %v)", again, changed, err)
	}

	otherRequest, err := realizationdelegate.NewRequest(
		claim.InstallRequest().RouteID(),
		claim.InstallRequest().ContractVersion(),
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	)
	if err != nil {
		t.Fatal(err)
	}
	conflict, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		fixture.identity,
		otherRequest,
		durablecarrier.ClaimProvenanceInstalledObserved,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := current.WithoutManagedCarrierClaim(conflict); err == nil ||
		!strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("conflicting retirement error = %v", err)
	}
}

func TestSnapshotConvergesOnlyExactRegistryFirstGlobalClaim(t *testing.T) {
	root := t.TempDir()
	owner := mustStateAuthority(t, root, "daem.toml")
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeGlobal)
	pending, err := durablecarrier.NewPendingCarrierInstall(owner, fixture.identity, fixture.installRequest)
	if err != nil {
		t.Fatal(err)
	}
	current, err := durable.NewSnapshot(durable.SnapshotInput{
		PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{pending},
	})
	if err != nil {
		t.Fatal(err)
	}
	claim := claimForFixture(t, fixture, owner)
	registry, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{claim})
	if err != nil {
		t.Fatal(err)
	}
	next, changed, err := current.WithConvergedGlobalCarrierClaims(registry)
	if err != nil || !changed || len(next.PendingCarrierInstalls()) != 0 {
		t.Fatalf("WithConvergedGlobalCarrierClaims = (%#v, %t, %v)", next, changed, err)
	}

	foreignOwner := mustStateAuthority(t, t.TempDir(), "other.toml")
	foreignClaim := claimForFixture(t, fixture, foreignOwner)
	foreignRegistry, err := durablecarrier.NewGlobalCarrierClaims(
		[]durablecarrier.ManagedCarrierClaim{foreignClaim},
	)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, changed, err := current.WithConvergedGlobalCarrierClaims(foreignRegistry)
	if err != nil || changed || !unchanged.Equal(current) {
		t.Fatalf("foreign convergence = (%#v, %t, %v)", unchanged, changed, err)
	}
}
