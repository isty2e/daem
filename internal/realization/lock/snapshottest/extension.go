package snapshottest

import (
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/refine"
)

// ExtensionCarrierFile locks one desired extension and returns its delegated
// relation realization.
func ExtensionCarrierFile(
	t testing.TB,
	value desiredextension.Extension,
) (lock.File, realization.DelegatedRelation) {
	t.Helper()
	contracts, err := refine.Extensions([]desiredextension.Extension{value})
	if err != nil {
		t.Fatalf("refine.Extensions: %v", err)
	}
	section, err := lock.NewLockedSection(contracts)
	if err != nil {
		t.Fatalf("lock.NewLockedSection: %v", err)
	}
	if len(contracts) != 1 {
		t.Fatalf("locked subjects = %#v, want one extension-derived carrier", contracts)
	}
	file := lock.File{Version: lock.CurrentVersion, Locked: section}
	return file, DelegatedRelation(t, contracts[0])
}

// DelegatedRelation validates and returns one locked delegated relation.
func DelegatedRelation(
	t testing.TB,
	contract lock.LockedSubjectContract,
) realization.DelegatedRelation {
	t.Helper()
	_, ok, err := lock.DelegatedRelationCarrier(contract)
	if err != nil {
		t.Fatalf("lock.DelegatedRelationCarrier: %v", err)
	}
	if !ok {
		t.Fatal("lock.DelegatedRelationCarrier returned ok=false")
	}
	realization, _ := contract.Realization()
	relation, _ := realization.DelegatedRelation()
	return relation
}
