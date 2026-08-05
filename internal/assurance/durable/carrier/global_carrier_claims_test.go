package carrier_test

import (
	"path/filepath"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
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

func TestGlobalCarrierClaimsRetireBatchIsStrictAtomicAndCanonical(t *testing.T) {
	firstFixture := carrierFixtureFor(t, "alpha", "alpha@official", target.ScopeGlobal)
	secondFixture := carrierFixtureFor(t, "beta", "beta@official", target.ScopeGlobal)
	retainedFixture := carrierFixtureFor(t, "retained", "retained@official", target.ScopeGlobal)
	first := claimForFixture(t, firstFixture, mustAuthority(t, t.TempDir(), "first.toml"))
	second := claimForFixture(t, secondFixture, mustAuthority(t, t.TempDir(), "second.toml"))
	retained := claimForFixture(t, retainedFixture, mustAuthority(t, t.TempDir(), "retained.toml"))
	registry, err := durablecarrier.NewGlobalCarrierClaims(
		[]durablecarrier.ManagedCarrierClaim{second, retained, first},
	)
	if err != nil {
		t.Fatal(err)
	}

	forward, err := registry.RetireClaims(
		[]durablecarrier.ManagedCarrierClaim{first, second},
	)
	if err != nil {
		t.Fatal(err)
	}
	reverse, err := registry.RetireClaims(
		[]durablecarrier.ManagedCarrierClaim{second, first},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !forward.Equal(reverse) {
		t.Fatalf("retirement order changed successor: forward=%#v reverse=%#v", forward, reverse)
	}
	sequential := registry
	for _, claim := range []durablecarrier.ManagedCarrierClaim{first, second} {
		sequential, _, err = sequential.WithoutClaim(claim)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !forward.Equal(sequential) {
		t.Fatalf("batch successor = %#v, want sequential semantics %#v", forward, sequential)
	}
	claims := forward.Claims()
	if len(claims) != 1 || !claims[0].ExactEqual(retained) {
		t.Fatalf("retained claims = %#v, want exact retained claim", claims)
	}
	empty, err := registry.RetireClaims(nil)
	if err != nil || !empty.Equal(registry) {
		t.Fatalf("empty retirement = (%#v, %v)", empty, err)
	}
	single, err := registry.RetireClaims([]durablecarrier.ManagedCarrierClaim{first})
	if err != nil || len(single.Claims()) != 2 {
		t.Fatalf("single retirement = (%#v, %v)", single, err)
	}
}

func TestGlobalCarrierClaimsRetireBatchRejectsEveryInexactSetWithoutSuccessor(t *testing.T) {
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeGlobal)
	owner := mustAuthority(t, t.TempDir(), "daem.toml")
	claim := claimForFixture(t, fixture, owner)
	registry, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{claim})
	if err != nil {
		t.Fatal(err)
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
	otherOwner, err := stateauthority.New(
		owner.StatefileAuthority(),
		filepath.Join(filepath.Dir(owner.ManifestPath()), "other.toml"),
	)
	if err != nil {
		t.Fatal(err)
	}
	provenanceConflict, err := durablecarrier.NewManagedCarrierClaim(
		otherOwner,
		fixture.identity,
		claim.InstallRequest(),
		claim.Provenance(),
	)
	if err != nil {
		t.Fatal(err)
	}
	absentFixture := carrierFixtureFor(t, "absent", "absent@official", target.ScopeGlobal)
	absent := claimForFixture(t, absentFixture, mustAuthority(t, t.TempDir(), "absent.toml"))
	projectFixture := carrierFixtureFor(t, "project", "project@official", target.ScopeProject)
	project := claimForFixture(t, projectFixture, mustAuthority(t, t.TempDir(), "project.toml"))

	tests := []struct {
		name   string
		claims []durablecarrier.ManagedCarrierClaim
		want   string
	}{
		{name: "duplicate", claims: []durablecarrier.ManagedCarrierClaim{claim, claim}, want: "duplicates"},
		{name: "batch conflict", claims: []durablecarrier.ManagedCarrierClaim{claim, conflict}, want: "conflicts within"},
		{name: "retained conflict", claims: []durablecarrier.ManagedCarrierClaim{conflict}, want: "conflicts with retained"},
		{name: "absent", claims: []durablecarrier.ManagedCarrierClaim{absent}, want: "is absent"},
		{name: "project", claims: []durablecarrier.ManagedCarrierClaim{project}, want: "global scope"},
		{name: "invalid", claims: []durablecarrier.ManagedCarrierClaim{{}}, want: "claim[0]"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := registry.RetireClaims(test.claims); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("RetireClaims error = %v, want %q", err, test.want)
			}
			if claims := registry.Claims(); len(claims) != 1 || !claims[0].ExactEqual(claim) {
				t.Fatalf("failed retirement mutated source registry: %#v", claims)
			}
		})
	}

	permutations := [][]durablecarrier.ManagedCarrierClaim{
		{claim, claim, provenanceConflict},
		{claim, provenanceConflict, claim},
		{provenanceConflict, claim, claim},
		{claim, provenanceConflict, provenanceConflict},
		{provenanceConflict, claim, provenanceConflict},
		{provenanceConflict, provenanceConflict, claim},
	}
	const wantConflict = "global carrier retirement conflicts within one owner relation"
	for index, permutation := range permutations {
		if _, err := registry.RetireClaims(permutation); err == nil || err.Error() != wantConflict {
			t.Fatalf("RetireClaims permutation[%d] error = %v, want deterministic conflict", index, err)
		}
	}
}
