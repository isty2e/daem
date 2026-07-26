// Package snapshottest provides invariant-preserving lock fixtures for tests
// outside the lock package.
package snapshottest

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/supply/artifact"
	resourcetopology "github.com/isty2e/daem/internal/topology/resource"
)

// ExactSupplyInput carries already-selected exact-Supply facts for one test fixture.
type ExactSupplyInput struct {
	Kind         entity.Kind
	Name         string
	SourceID     artifact.SourceID
	ResolvedRef  artifact.ResolvedRef
	ArtifactKind artifact.ArtifactKind
	ContentHash  artifact.ContentHash
	ExactFileUse *lock.ExactFileUse
}

// ExactSupplyContract constructs one canonical direct-resolution contract
// without admitting it as a complete locked collection. Callers assembling
// correlated subjects must pass the final collection through Section or File.
func ExactSupplyContract(t testing.TB, input ExactSupplyInput) lock.LockedSubjectContract {
	t.Helper()
	entityID, err := entity.New(input.Kind, input.Name)
	if err != nil {
		t.Fatalf("entity.New: %v", err)
	}
	identity, err := artifact.NewExactIdentity(
		input.SourceID,
		input.ResolvedRef,
		input.ArtifactKind,
		input.ContentHash,
	)
	if err != nil {
		t.Fatalf("artifact.NewExactIdentity: %v", err)
	}
	derivation, err := lock.NewDirectResolutionDerivation(identity)
	if err != nil {
		t.Fatalf("lock.NewDirectResolutionDerivation: %v", err)
	}
	subjectID, err := resourcetopology.Subject(entityID)
	if err != nil {
		t.Fatalf("resource topology subject: %v", err)
	}
	contract, err := lock.NewExactSupplySubjectContract(lock.ExactSupplySubjectInput{
		EntityID:     entityID,
		SubjectID:    subjectID,
		ExactSupply:  identity,
		ExactFileUse: input.ExactFileUse,
		Derivation:   derivation,
	})
	if err != nil {
		t.Fatalf("lock.NewExactSupplySubjectContract: %v", err)
	}
	return contract
}

// ExactSupply constructs and admits one standalone direct-resolution subject fixture.
func ExactSupply(t testing.TB, input ExactSupplyInput) lock.LockedSubjectContract {
	t.Helper()
	contract := ExactSupplyContract(t, input)
	section, err := lock.NewLockedSection([]lock.LockedSubjectContract{contract})
	if err != nil {
		t.Fatalf("lock.NewLockedSection: %v", err)
	}
	admitted, ok := section.Subject(contract.SubjectID())
	if !ok {
		t.Fatalf("lock.NewLockedSection omitted subject %q", contract.SubjectID())
	}
	return admitted
}

// Section constructs one validated canonical subject collection.
func Section(t testing.TB, subjects ...lock.LockedSubjectContract) lock.LockedSection {
	t.Helper()
	section, err := lock.NewLockedSection(subjects)
	if err != nil {
		t.Fatalf("lock.NewLockedSection: %v", err)
	}
	return section
}

// File constructs one current-version canonical lock file.
func File(t testing.TB, subjects ...lock.LockedSubjectContract) lock.File {
	t.Helper()
	return lock.File{
		Version: lock.CurrentVersion,
		Locked:  Section(t, subjects...),
	}
}
