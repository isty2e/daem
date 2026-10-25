package lock

import (
	"fmt"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/supply/artifact"
)

const NativeVersion = contractversion.NativeLockfileSchema

// NewNativeLockedSection selects native document admission without widening the default family set.
func NewNativeLockedSection(subjects []LockedSubjectContract, order []hostrelation.RelationOrderConstraint) (LockedSection, error) {
	return newLockedSection(subjects, order, validateNativeLockedSubject, validateNativeLockedCollection)
}

// ValidateNative validates only the explicitly selected native lock generation.
func ValidateNative(file File) error {
	return validateFile(file, NativeVersion, NewNativeLockedSection)
}

// NewHostConfigDocumentSubjectContract projects static declaration intent into its whole-file subject.
func NewHostConfigDocumentSubjectContract(id entity.ID, placement profile.HostConfigPlacement) (LockedSubjectContract, error) {
	if id.Kind() != entity.KindHostConfig {
		return LockedSubjectContract{}, fmt.Errorf("native document requires a host configuration entity")
	}
	spec, err := placement.DocumentProjection()
	if err != nil {
		return LockedSubjectContract{}, err
	}
	subject, err := aggregate.HostConfigDocumentSubject(id, placement.Target(), placement.Scope())
	if err != nil {
		return LockedSubjectContract{}, err
	}
	write, err := placement.DocumentOperationRoute(profile.OperationWrite)
	if err != nil {
		return LockedSubjectContract{}, err
	}
	remove, err := placement.DocumentOperationRoute(profile.OperationRemove)
	if err != nil {
		return LockedSubjectContract{}, err
	}
	return NewManagedPathSubjectContract(ManagedPathSubjectInput{
		EntityID: id, SubjectID: subject, Realization: spec,
		WriteRouteID: write.RouteID(), RemoveRouteID: remove.RouteID(),
	})
}

func validateNativeLockedSubject(contract LockedSubjectContract) error {
	if contract.EntityID().Kind() != entity.KindHostConfig {
		return validateAdmittedLockedSubject(contract)
	}
	if _, supplied := contract.ExactSupply(); supplied {
		shape := exactSupplyFamilyShape{kind: entity.KindHostConfig, label: "HostConfig", artifactKind: artifact.ArtifactKindFile, requiresExactFileUse: true}
		if _, err := shape.validate(contract); err != nil {
			return err
		}
		placement, _ := contract.HostConfigPlacement()
		if placement.Ownership() != hostconfig.OwnershipDocument {
			return fmt.Errorf("native key source admission is not implemented")
		}
		return nil
	}
	// The complete collection correlates this projection with its source intent.
	if spec, present := contract.Realization(); present {
		if _, path := spec.ManagedPathProjection(); path {
			return nil
		}
	}
	return fmt.Errorf("unsupported native document subject")
}

func validateNativeLockedCollection(index lockedCollectionIndex) error {
	if err := validateLockedCollectionAdmission(index); err != nil {
		return err
	}
	for id, source := range index.exactSupplyByEntity {
		if id.Kind() != entity.KindHostConfig {
			continue
		}
		placement, _ := source.HostConfigPlacement()
		expected, err := NewHostConfigDocumentSubjectContract(id, placement)
		if err != nil {
			return err
		}
		actual, present := index.pathProjectionContracts[expected.SubjectID()]
		if !present || index.pathProjectionCountByEntity[id] != 1 || !actual.Equal(expected) {
			return fmt.Errorf("host configuration %q requires its one canonical whole-document projection", id)
		}
	}
	return nil
}
