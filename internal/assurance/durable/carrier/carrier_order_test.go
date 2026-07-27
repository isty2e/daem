package carrier_test

import (
	"path/filepath"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/target"
)

func TestCarrierModelComparisonsPreserveEveryTieBreaker(t *testing.T) {
	root := t.TempDir()
	ownerA := mustAuthority(t, filepath.Join(root, "a"), "daem.toml")
	ownerB := mustAuthority(t, filepath.Join(root, "b"), "daem.toml")
	relationA := carrierFixtureFor(t, "alpha", "shared@official", target.ScopeProject)
	relationB := carrierFixtureFor(t, "beta", "shared@official", target.ScopeProject)
	carrierA := carrierFixtureFor(t, "same", "alpha@official", target.ScopeProject)
	carrierB := carrierFixtureFor(t, "same", "beta@official", target.ScopeProject)

	hashA := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hashB := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	requestA := orderRequest(t, "route.a", "1", hashA)
	requestRouteB := orderRequest(t, "route.b", "1", hashA)
	requestVersionB := orderRequest(t, "route.a", "2", hashA)
	requestHashB := orderRequest(t, "route.a", "1", hashB)

	pending := func(
		owner stateauthority.Authority,
		fixture carrierFixture,
		request realizationdelegate.Request,
	) durablecarrier.PendingCarrierInstall {
		value, err := durablecarrier.NewPendingCarrierInstall(owner, fixture.identity, request)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	pendingBase := pending(ownerA, carrierA, requestA)
	assertCarrierOrder(
		t, "pending owner",
		pendingBase,
		pending(ownerB, carrierA, requestA),
		durablecarrier.PendingCarrierInstall.Compare,
	)
	assertCarrierOrder(
		t, "pending relation subject",
		pending(ownerA, relationA, requestA),
		pending(ownerA, relationB, requestA),
		durablecarrier.PendingCarrierInstall.Compare,
	)
	assertCarrierOrder(
		t, "pending carrier subject",
		pendingBase,
		pending(ownerA, carrierB, requestA),
		durablecarrier.PendingCarrierInstall.Compare,
	)
	assertCarrierOrder(
		t, "pending route id",
		pendingBase,
		pending(ownerA, carrierA, requestRouteB),
		durablecarrier.PendingCarrierInstall.Compare,
	)
	assertCarrierOrder(
		t, "pending route contract",
		pendingBase,
		pending(ownerA, carrierA, requestVersionB),
		durablecarrier.PendingCarrierInstall.Compare,
	)
	assertCarrierOrder(
		t, "pending request hash",
		pendingBase,
		pending(ownerA, carrierA, requestHashB),
		durablecarrier.PendingCarrierInstall.Compare,
	)

	claim := func(
		owner stateauthority.Authority,
		fixture carrierFixture,
		request realizationdelegate.Request,
		provenance durablecarrier.ClaimProvenance,
	) durablecarrier.ManagedCarrierClaim {
		value, err := durablecarrier.NewManagedCarrierClaim(
			owner,
			fixture.identity,
			request,
			provenance,
		)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	claimBase := claim(
		ownerA,
		carrierA,
		requestA,
		durablecarrier.ClaimProvenanceInstalledObserved,
	)
	assertCarrierOrder(
		t, "claim owner",
		claimBase,
		claim(ownerB, carrierA, requestA, durablecarrier.ClaimProvenanceInstalledObserved),
		durablecarrier.ManagedCarrierClaim.Compare,
	)
	assertCarrierOrder(
		t, "claim relation subject",
		claim(ownerA, relationA, requestA, durablecarrier.ClaimProvenanceInstalledObserved),
		claim(ownerA, relationB, requestA, durablecarrier.ClaimProvenanceInstalledObserved),
		durablecarrier.ManagedCarrierClaim.Compare,
	)
	assertCarrierOrder(
		t, "claim carrier subject",
		claimBase,
		claim(ownerA, carrierB, requestA, durablecarrier.ClaimProvenanceInstalledObserved),
		durablecarrier.ManagedCarrierClaim.Compare,
	)
	assertCarrierOrder(
		t, "claim route id",
		claimBase,
		claim(ownerA, carrierA, requestRouteB, durablecarrier.ClaimProvenanceInstalledObserved),
		durablecarrier.ManagedCarrierClaim.Compare,
	)
	assertCarrierOrder(
		t, "claim route contract",
		claimBase,
		claim(ownerA, carrierA, requestVersionB, durablecarrier.ClaimProvenanceInstalledObserved),
		durablecarrier.ManagedCarrierClaim.Compare,
	)
	assertCarrierOrder(
		t, "claim request hash",
		claimBase,
		claim(ownerA, carrierA, requestHashB, durablecarrier.ClaimProvenanceInstalledObserved),
		durablecarrier.ManagedCarrierClaim.Compare,
	)
	assertCarrierOrder(
		t, "claim provenance",
		claim(ownerA, carrierA, requestA, durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved),
		claimBase,
		durablecarrier.ManagedCarrierClaim.Compare,
	)

	removal := func(
		value durablecarrier.ManagedCarrierClaim,
		request realizationdelegate.Request,
	) durablecarrier.PendingCarrierRemoval {
		pending, err := durablecarrier.NewPendingCarrierRemoval(
			value,
			request,
			relationOnlyRemovalPostconditions(),
			durablecarrier.EffectBaselineSet{},
		)
		if err != nil {
			t.Fatal(err)
		}
		return pending
	}
	removalBase := removal(claimBase, requestA)
	assertCarrierOrder(
		t, "removal owner",
		removalBase,
		removal(claim(ownerB, carrierA, requestA, durablecarrier.ClaimProvenanceInstalledObserved), requestA),
		durablecarrier.PendingCarrierRemoval.Compare,
	)
	assertCarrierOrder(
		t, "removal relation subject",
		removal(claim(ownerA, relationA, requestA, durablecarrier.ClaimProvenanceInstalledObserved), requestA),
		removal(claim(ownerA, relationB, requestA, durablecarrier.ClaimProvenanceInstalledObserved), requestA),
		durablecarrier.PendingCarrierRemoval.Compare,
	)
	assertCarrierOrder(
		t, "removal carrier subject",
		removalBase,
		removal(claim(ownerA, carrierB, requestA, durablecarrier.ClaimProvenanceInstalledObserved), requestA),
		durablecarrier.PendingCarrierRemoval.Compare,
	)
	assertCarrierOrder(
		t, "removal route id",
		removalBase,
		removal(claimBase, requestRouteB),
		durablecarrier.PendingCarrierRemoval.Compare,
	)
	assertCarrierOrder(
		t, "removal route contract",
		removalBase,
		removal(claimBase, requestVersionB),
		durablecarrier.PendingCarrierRemoval.Compare,
	)
	assertCarrierOrder(
		t, "removal request hash",
		removalBase,
		removal(claimBase, requestHashB),
		durablecarrier.PendingCarrierRemoval.Compare,
	)
}

func orderRequest(
	t *testing.T,
	routeID string,
	contractVersion string,
	hash string,
) realizationdelegate.Request {
	t.Helper()
	request, err := realizationdelegate.NewRequest(routeID, contractVersion, hash)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func assertCarrierOrder[T any](
	t *testing.T,
	name string,
	left T,
	right T,
	compare func(T, T) int,
) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		if order := compare(left, right); order >= 0 {
			t.Fatalf("left.Compare(right) = %d, want negative", order)
		}
		if order := compare(right, left); order <= 0 {
			t.Fatalf("right.Compare(left) = %d, want positive", order)
		}
		if order := compare(left, left); order != 0 {
			t.Fatalf("left.Compare(left) = %d, want zero", order)
		}
	})
}
