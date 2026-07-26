package lock

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

type (
	lockedSubjectAdmission    func(LockedSubjectContract) (bool, error)
	lockedCollectionAdmission func(lockedCollectionIndex) error
)

type lockedCollectionIndex struct {
	exactSupplyByEntity         map[entity.ID]LockedSubjectContract
	pathProjectionCountByEntity map[entity.ID]int
	pathProjectionContracts     map[topology.SubjectID]LockedSubjectContract
}

type managedPathOccupancy struct {
	scope       target.Scope
	destination string
}

type managedAggregateOccupancy struct {
	scope         target.Scope
	aggregateRoot string
	contentPath   string
}

type managedAggregateOccupant struct {
	subject  topology.SubjectID
	contract aggregate.ProjectionContract
}

type delegatedRelationOccupancy struct {
	target             target.Target
	scope              target.Scope
	managedInstanceKey hostrelation.ManagedInstanceKey
}

func validateLockedCollection(subjects []LockedSubjectContract) (lockedCollectionIndex, error) {
	seenSubjects := make(map[topology.SubjectID]entity.ID, len(subjects))
	seenPaths := make(map[managedPathOccupancy]topology.SubjectID)
	seenContributions := make(map[managedAggregateOccupancy]managedAggregateOccupant)
	seenRelations := make(map[delegatedRelationOccupancy]topology.SubjectID)
	exactSupplyByEntity := make(map[entity.ID]LockedSubjectContract)
	pathProjectionEntities := make(map[topology.SubjectID]entity.ID)
	pathProjectionContracts := make(map[topology.SubjectID]LockedSubjectContract)
	pathProjectionCountByEntity := make(map[entity.ID]int)

	for _, subject := range subjects {
		if existing, duplicate := seenSubjects[subject.SubjectID()]; duplicate {
			return lockedCollectionIndex{}, fmt.Errorf(
				"duplicate locked subject %q for entities %q and %q",
				subject.SubjectID(),
				existing,
				subject.EntityID(),
			)
		}
		seenSubjects[subject.SubjectID()] = subject.EntityID()
		if _, supplied := subject.ExactSupply(); supplied {
			exactSupplyByEntity[subject.EntityID()] = subject
		}

		realization, realized := subject.Realization()
		if !realized {
			continue
		}
		if projection, ok := realization.ManagedPathProjection(); ok {
			pathProjectionEntities[subject.SubjectID()] = subject.EntityID()
			pathProjectionContracts[subject.SubjectID()] = subject
			pathProjectionCountByEntity[subject.EntityID()]++
			key := managedPathOccupancy{
				scope: projection.Scope(), destination: projection.Destination(),
			}
			if existing, duplicate := seenPaths[key]; duplicate {
				return lockedCollectionIndex{}, fmt.Errorf(
					"duplicate managed path occupancy scope=%q destination=%q for subjects %q and %q",
					key.scope, key.destination, existing, subject.SubjectID(),
				)
			}
			seenPaths[key] = subject.SubjectID()
			continue
		}
		if contribution, ok := realization.ManagedAggregateContribution(); ok {
			key := managedAggregateOccupancy{
				scope:         contribution.Scope(),
				aggregateRoot: contribution.AggregateRoot(), contentPath: contribution.ContentPath(),
			}
			if existing, duplicate := seenContributions[key]; duplicate {
				if !existing.contract.Equal(contribution.Contract()) {
					return lockedCollectionIndex{}, fmt.Errorf(
						"conflicting managed aggregate occupancy scope=%q aggregate_root=%q content_path=%q for subjects %q and %q",
						key.scope, key.aggregateRoot, key.contentPath, existing.subject, subject.SubjectID(),
					)
				}
				if contribution.Cardinality() == aggregate.ContributionExclusive {
					return lockedCollectionIndex{}, fmt.Errorf(
						"duplicate exclusive managed aggregate occupancy scope=%q aggregate_root=%q content_path=%q for subjects %q and %q",
						key.scope, key.aggregateRoot, key.contentPath, existing.subject, subject.SubjectID(),
					)
				}
				continue
			}
			seenContributions[key] = managedAggregateOccupant{
				subject: subject.SubjectID(), contract: contribution.Contract(),
			}
			continue
		}
		if relation, ok := realization.DelegatedRelation(); ok {
			key := delegatedRelationOccupancy{
				target: relation.Target(), scope: relation.Scope(),
				managedInstanceKey: relation.ExpectedRelation().ManagedInstanceKey(),
			}
			if existing, duplicate := seenRelations[key]; duplicate {
				return lockedCollectionIndex{}, fmt.Errorf(
					"duplicate delegated relation occupancy target=%q scope=%q managed_instance_key=%q for subjects %q and %q",
					key.target, key.scope, key.managedInstanceKey, existing, subject.SubjectID(),
				)
			}
			seenRelations[key] = subject.SubjectID()
		}
	}
	for subjectID, entityID := range pathProjectionEntities {
		supply, supplied := exactSupplyByEntity[entityID]
		if !supplied {
			return lockedCollectionIndex{}, fmt.Errorf("managed path subject %q has no exact-Supply subject for entity %q", subjectID, entityID)
		}
		projectionContract := pathProjectionContracts[subjectID]
		spec, _ := projectionContract.Realization()
		projection, _ := spec.ManagedPathProjection()
		if projection.ContentKind() != realization.PathProjectionFile {
			continue
		}
		fileUse, ok := supply.ExactFileUse()
		if !ok {
			return lockedCollectionIndex{}, fmt.Errorf("managed file subject %q has no exact file use for entity %q", subjectID, entityID)
		}
		if fileUse.Scope() != projection.Scope() {
			return lockedCollectionIndex{}, fmt.Errorf("managed file subject %q scope does not match exact file use", subjectID)
		}
		if _, ok := supply.MaterializedFileIdentity(); !ok {
			return lockedCollectionIndex{}, fmt.Errorf("managed file subject %q has no materialized file identity", subjectID)
		}
	}
	return lockedCollectionIndex{
		exactSupplyByEntity:         exactSupplyByEntity,
		pathProjectionCountByEntity: pathProjectionCountByEntity,
		pathProjectionContracts:     pathProjectionContracts,
	}, nil
}

func validateLockedCollectionAdmission(index lockedCollectionIndex) error {
	for _, admit := range [...]lockedCollectionAdmission{
		validateInstructionsPathProjectionCollection,
		validateHookAssetPathProjectionCollection,
	} {
		if err := admit(index); err != nil {
			return err
		}
	}
	return nil
}

func validateAdmittedLockedSubject(contract LockedSubjectContract) error {
	_, realized := contract.Realization()
	if !realized {
		return validateAdmittedExactSupplySubject(contract)
	}
	// This fixed O(N) composition root invokes family-owned refinement adapters;
	// it does not select builders, profiles, routes, or host behavior.
	for _, admit := range [...]lockedSubjectAdmission{
		validateAdmittedSkillPathProjection,
		validateAdmittedInstructionsPathProjection,
		validateAdmittedHookAssetPathProjection,
		validateAdmittedHookProjection,
		validateAdmittedMCPProjection,
		validateAdmittedDelegatedRelationCarrier,
	} {
		admitted, err := admit(contract)
		if err != nil {
			return err
		}
		if admitted {
			return nil
		}
	}
	return fmt.Errorf("subject has no current topology refinement")
}

func validateAdmittedDelegatedRelationCarrier(contract LockedSubjectContract) (bool, error) {
	_, admitted, err := DelegatedRelationCarrier(contract)
	return admitted, err
}
