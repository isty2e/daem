package lock

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/supply/artifact"
)

// NewHostConfigDocumentSubjectContract projects static declaration intent into its whole-file subject.
func NewHostConfigDocumentSubjectContract(id entity.ID, placement configuration.Placement) (LockedSubjectContract, error) {
	if id.Kind() != entity.KindHostConfig {
		return LockedSubjectContract{}, fmt.Errorf("native document requires a host configuration entity")
	}
	spec, err := profile.HostConfigDocumentProjection(placement)
	if err != nil {
		return LockedSubjectContract{}, err
	}
	subject, err := aggregate.HostConfigDocumentSubject(id, placement.Target(), placement.Scope())
	if err != nil {
		return LockedSubjectContract{}, err
	}
	write, err := profile.HostConfigOperationRoute(placement, profile.OperationWrite)
	if err != nil {
		return LockedSubjectContract{}, err
	}
	remove, err := profile.HostConfigOperationRoute(placement, profile.OperationRemove)
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
		return nil
	}
	// The complete collection correlates this projection with its source intent.
	if spec, present := contract.Realization(); present {
		if _, path := spec.ManagedPathProjection(); path {
			return nil
		}
		if contribution, selected := spec.ManagedAggregateContribution(); selected {
			return validateNativeKeySubject(contract, contribution)
		}
	}
	return fmt.Errorf("unsupported native document subject")
}

func validateNativeLockedCollection(index lockedCollectionIndex) error {
	if err := validateLockedCollectionAdmission(index); err != nil {
		return err
	}
	whole := make(map[managedPathOccupancy]struct{})
	for id, source := range index.exactSupplyByEntity {
		if id.Kind() != entity.KindHostConfig {
			continue
		}
		placement, _ := source.HostConfigPlacement()
		if placement.Ownership() == hostconfig.OwnershipKeys {
			if index.pathProjectionCountByEntity[id] != 0 {
				return fmt.Errorf("native key source %q cannot supply a whole-document projection", id)
			}
			continue
		}
		expected, err := NewHostConfigDocumentSubjectContract(id, placement)
		if err != nil {
			return err
		}
		actual, present := index.pathProjectionContracts[expected.SubjectID()]
		if !present || index.pathProjectionCountByEntity[id] != 1 || !actual.Equal(expected) {
			return fmt.Errorf("host configuration %q requires its one canonical whole-document projection", id)
		}
		whole[managedPathOccupancy{scope: placement.Scope(), destination: placement.Destination().String()}] = struct{}{}
	}
	keys := make(map[managedPathOccupancy][]output.KeyPath)
	for id, contracts := range index.nativeKeyProjections {
		source, supplied := index.exactSupplyByEntity[id]
		placement, placed := source.HostConfigPlacement()
		if !supplied || !placed || placement.Ownership() != hostconfig.OwnershipKeys {
			return fmt.Errorf("native key projections for %q require their key-mode exact source", id)
		}
		for _, contract := range contracts {
			spec, _ := contract.Realization()
			contribution, _ := spec.ManagedAggregateContribution()
			expected, err := profile.HostConfigProjectionContract(placement, contribution.Footprint())
			if err != nil || !expected.Equal(contribution.Contract()) {
				return fmt.Errorf("native key projection for %q does not match its source placement", id)
			}
			key := managedPathOccupancy{scope: contribution.Scope(), destination: contribution.AggregateRoot().String()}
			if _, occupied := whole[key]; occupied {
				return fmt.Errorf("native key projection overlaps a whole-document owner")
			}
			path, _ := contribution.Footprint().Key()
			keys[key] = append(keys[key], path)
		}
	}
	for _, paths := range keys {
		if len(output.TopmostKeyPaths(paths)) != len(paths) {
			return fmt.Errorf("native key projections overlap at a literal key ancestor")
		}
	}
	return nil
}
