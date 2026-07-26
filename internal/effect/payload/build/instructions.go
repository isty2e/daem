package build

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	instructionsresource "github.com/isty2e/daem/internal/desired/instructions"
	"github.com/isty2e/daem/internal/effect/payload"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func buildInstructionPayloads(
	ctx context.Context,
	resolvers *sourceResolverOnce,
	instructionsValues []instructionsresource.Instructions,
	locked lock.File,
	subjects []topology.SubjectID,
) ([]payload.Payload, error) {
	required, err := requiredInstructionProjectionSubjects(subjects)
	if err != nil {
		return nil, err
	}
	if len(required) == 0 {
		return []payload.Payload{}, nil
	}
	resolver, err := resolvers.get()
	if err != nil {
		return nil, err
	}

	payloads := make([]payload.Payload, 0, len(instructionsValues))
	for _, instructions := range instructionsValues {
		requiredSubjects, selected := required[instructions.ID()]
		if !selected {
			continue
		}

		lockedContract, ok := locked.Locked.ExactSupplySubject(instructions.ID())
		if !ok {
			return nil, fmt.Errorf("instructions %q: missing lockfile entry", instructions.ID().Name())
		}

		fileUse, ok := lockedContract.ExactFileUse()
		if !ok || fileUse.Scope() != instructions.Scope() || fileUse.Executable() {
			return nil, fmt.Errorf("instructions %q: exact file use does not match lockfile entry", instructions.ID().Name())
		}
		materialized, err := materializeLockedFile(ctx, resolver, instructions.Source(), lockedContract, false)
		if err != nil {
			return nil, fmt.Errorf("instructions %q: %w", instructions.ID().Name(), err)
		}
		for subject := range requiredSubjects {
			projectionContract, ok := locked.Locked.Subject(subject)
			if !ok || projectionContract.EntityID() != instructions.ID() {
				return nil, fmt.Errorf("instructions %q: required projection %q is missing", instructions.ID().Name(), subject)
			}
			spec, ok := projectionContract.Realization()
			if !ok {
				return nil, fmt.Errorf("instructions %q: required projection %q has no realization", instructions.ID().Name(), subject)
			}
			projection, ok := spec.ManagedPathProjection()
			if !ok || projection.ContentKind() != realization.PathProjectionFile ||
				projection.PlacementMode() != realization.PathProjectionCopy {
				return nil, fmt.Errorf("instructions %q: required projection %q is not a copy file", instructions.ID().Name(), subject)
			}
			delete(requiredSubjects, subject)
			built, err := payload.NewFilePayload(subject, materialized.content.Bytes(), 0o600)
			if err != nil {
				return nil, fmt.Errorf("instructions %q: construct payload: %w", instructions.ID().Name(), err)
			}
			if built.Hash() != materialized.transformation.OutputIdentity().ContentHash() {
				return nil, fmt.Errorf("instructions %q: payload hash does not match materialized output", instructions.ID().Name())
			}
			payloads = append(payloads, built)
		}
	}
	for entityID, subjects := range required {
		if len(subjects) != 0 {
			return nil, fmt.Errorf("required Instructions projection for %q was not materialized", entityID.Name())
		}
	}

	return payloads, nil
}

func requiredInstructionProjectionSubjects(
	values []topology.SubjectID,
) (map[entity.ID]map[topology.SubjectID]struct{}, error) {
	result := make(map[entity.ID]map[topology.SubjectID]struct{})
	for index, subject := range values {
		entityID, ok := topologyprojection.EntityID(subject)
		if !ok || entityID.Kind() != entity.KindInstructions {
			continue
		}
		subjects := result[entityID]
		if subjects == nil {
			subjects = make(map[topology.SubjectID]struct{})
			result[entityID] = subjects
		}
		if _, duplicate := subjects[subject]; duplicate {
			return nil, fmt.Errorf("required Instructions projection subject[%d] duplicates %q", index, subject)
		}
		subjects[subject] = struct{}{}
	}
	return result, nil
}
