package carrier_test

import (
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/target"
)

func TestGlobalCarrierClaimsAllowSharedCarrierAcrossAuthorities(t *testing.T) {
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeGlobal)
	first := claimForFixture(t, fixture, mustAuthority(t, t.TempDir(), "first.toml"))
	second := claimForFixture(t, fixture, mustAuthority(t, t.TempDir(), "second.toml"))

	registry, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{second, first})
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Claims()) != 2 {
		t.Fatalf("claims = %#v", registry.Claims())
	}
	occupancy, err := durablecarrier.NewCarrierOccupancy(fixture.carrier, registry.Claims())
	if err != nil {
		t.Fatal(err)
	}
	if occupancy.DaemKnownConsumerCount() != 2 {
		t.Fatalf("daem-known consumers = %d", occupancy.DaemKnownConsumerCount())
	}
}

func TestGlobalCarrierClaimsUpsertBatchIsAtomicCanonicalAndIdempotent(t *testing.T) {
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeGlobal)
	first := claimForFixture(t, fixture, mustAuthority(t, t.TempDir(), "first.toml"))
	second := claimForFixture(t, fixture, mustAuthority(t, t.TempDir(), "second.toml"))

	registry, changed, err := durablecarrier.EmptyGlobalCarrierClaims().WithClaims(
		[]durablecarrier.ManagedCarrierClaim{second, first},
	)
	if err != nil || !changed || len(registry.Claims()) != 2 {
		t.Fatalf("WithClaims = (%#v, %t, %v)", registry, changed, err)
	}
	again, changed, err := registry.WithClaims(
		[]durablecarrier.ManagedCarrierClaim{first, second},
	)
	if err != nil || changed || !again.Equal(registry) {
		t.Fatalf("idempotent WithClaims = (%#v, %t, %v)", again, changed, err)
	}
	if _, _, err := durablecarrier.EmptyGlobalCarrierClaims().WithClaims(
		[]durablecarrier.ManagedCarrierClaim{first, first},
	); err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("duplicate batch error = %v", err)
	}
}

func TestGlobalCarrierClaimsRejectProjectAndConflictingOwnerRelation(t *testing.T) {
	project := carrierFixtureFor(t, "project", "project@official", target.ScopeProject)
	projectClaim := claimForFixture(t, project, mustAuthority(t, t.TempDir(), "daem.toml"))
	if _, err := durablecarrier.NewGlobalCarrierClaims(
		[]durablecarrier.ManagedCarrierClaim{projectClaim},
	); err == nil || !strings.Contains(err.Error(), "global scope") {
		t.Fatalf("project claim error = %v", err)
	}

	global := carrierFixtureFor(t, "global", "global@official", target.ScopeGlobal)
	owner := mustAuthority(t, t.TempDir(), "daem.toml")
	claim := claimForFixture(t, global, owner)
	registry, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{claim})
	if err != nil {
		t.Fatal(err)
	}
	other := carrierFixtureFor(t, "global", "other@official", target.ScopeGlobal)
	conflict, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		other.identity,
		other.installRequest,
		durablecarrier.ClaimProvenanceInstalledObserved,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := registry.WithClaim(conflict); err == nil ||
		!strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("conflicting claim error = %v", err)
	}
}

func TestGlobalCarrierClaimsRetireOnlyExactClaim(t *testing.T) {
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeGlobal)
	owner := mustAuthority(t, t.TempDir(), "daem.toml")
	claim := claimForFixture(t, fixture, owner)
	registry, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{claim})
	if err != nil {
		t.Fatal(err)
	}

	next, changed, err := registry.WithoutClaim(claim)
	if err != nil || !changed || len(next.Claims()) != 0 {
		t.Fatalf("WithoutClaim = (%#v, %t, %v)", next, changed, err)
	}
	again, changed, err := next.WithoutClaim(claim)
	if err != nil || changed || !again.Equal(next) {
		t.Fatalf("idempotent WithoutClaim = (%#v, %t, %v)", again, changed, err)
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
	if _, _, err := registry.WithoutClaim(conflict); err == nil ||
		!strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("conflicting retirement error = %v", err)
	}
}
