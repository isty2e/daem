package recoveryoperation

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestRecoveryClaimRemovalCandidatesIncludeRollbackDeletionsOnly(t *testing.T) {
	address, err := ownership.NewManagedAddressFromLegacy(pathtest.Exact("/tmp/daem-global-config.json"), "")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(pathtest.Exact("/tmp/daem-state.json"), "/tmp/daem.toml")
	if err != nil {
		t.Fatal(err)
	}
	acquire, err := ownershipmutation.NewAcquireTransition(address, owner, "20260625T000000.000000000Z-apply")
	if err != nil {
		t.Fatal(err)
	}
	reserved, _ := acquire.Prepared().Get()
	active, _ := acquire.After().Get()
	release, err := ownershipmutation.NewReleaseTransition(active)
	if err != nil {
		t.Fatal(err)
	}
	retain, err := ownershipmutation.NewRetainTransition(active)
	if err != nil {
		t.Fatal(err)
	}

	candidates := recoveryClaimRemovalCandidates(
		[]ownershipmutation.ClaimTransition{retain, release, acquire},
	)
	if len(candidates) != 2 || !candidates[0].Equal(active) || !candidates[1].Equal(reserved) {
		t.Fatalf("removal candidates = %#v, want release active and acquire reserved", candidates)
	}
}
