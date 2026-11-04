package observe

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/test/outputtest"
)

func TestOwnershipCorrelationPreservesMissingHistoryAsProvisional(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "Café.json")
	exact := pathtest.DarwinCaseSensitive(physical)
	missing, err := pathauthority.NewProvisional(physical, exact.Witness(), root, pathtest.DarwinCaseSensitive(root).Witness())
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(pathtest.DarwinCaseSensitive(filepath.Join(root, "state")), filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	old := outputtest.KeyFootprint(t, "old")
	address, err := ownership.NewManagedAddress(exact, old)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := ownership.NewActiveClaim(address, owner)
	if err != nil {
		t.Fatal(err)
	}
	value, err := ownership.PresentClaim(claim)
	if err != nil {
		t.Fatal(err)
	}
	destination := outputtest.Parse(t, "@pi-agent/extension-settings/test.json")
	current, err := NewExactOwnershipObservation(destination, old, address, value)
	if err != nil || !current.CorrelatesClaimAddress() {
		t.Fatal("current exact claim failed correlation", err)
	}
	history, err := NewProvisionalOwnershipObservation(destination, old, missing, value)
	if err != nil || !history.CorrelatesClaimAddress() {
		t.Fatal("matching missing history failed correlation", err)
	}
	if _, promoted := history.ExactAddress(); promoted {
		t.Fatal("history comparison restored current exact authority")
	}
	if retained, present := history.ProvisionalPath(); !present || !retained.Equal(missing) {
		t.Fatal("history comparison lost the actual missing observation")
	}
	disjoint, err := NewProvisionalOwnershipObservation(destination, outputtest.KeyFootprint(t, "new"), missing, value)
	if err != nil || disjoint.CorrelatesClaimAddress() {
		t.Fatal("an unrelated key's claim became matching history", err)
	}
	ancestorAddress, err := ownership.NewManagedAddress(pathtest.DarwinCaseSensitive(root), old)
	if err != nil {
		t.Fatal(err)
	}
	ancestor, err := ownership.NewActiveClaim(ancestorAddress, owner)
	if err != nil {
		t.Fatal(err)
	}
	ancestorValue, _ := ownership.PresentClaim(ancestor)
	conflict, err := NewProvisionalOwnershipObservation(destination, old, missing, ancestorValue)
	if err != nil || conflict.CorrelatesClaimAddress() {
		t.Fatal("conservative ancestor conflict became a matching native baseline", err)
	}
	otherAddress, err := ownership.NewManagedAddress(pathtest.Exact(physical), old)
	if err != nil {
		t.Fatal(err)
	}
	otherClaim, err := ownership.NewActiveClaim(otherAddress, owner)
	if err != nil {
		t.Fatal(err)
	}
	otherValue, _ := ownership.PresentClaim(otherClaim)
	wrongWitness, err := NewProvisionalOwnershipObservation(destination, old, missing, otherValue)
	if err != nil || wrongWitness.CorrelatesClaimAddress() {
		t.Fatal("incompatible namespace history became matching authority", err)
	}
	if (OwnershipObservation{}).CorrelatesClaimAddress() {
		t.Fatal("zero observation correlated with ownership")
	}
}
