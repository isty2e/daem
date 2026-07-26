package build

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source/directfile"
	resourcetopology "github.com/isty2e/daem/internal/topology/resource"
)

func lockResolvedInstructions(
	ctx context.Context,
	inputs []instructionLockAssemblyInput,
	options Options,
) ([]lock.LockedSubjectContract, error) {
	lockedInstructions := make([]lock.LockedSubjectContract, 0, len(inputs))

	for _, input := range inputs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		instruction := input.value
		resolution := input.artifact.resolution
		identity := resolution.Identity()
		if identity.Kind() != artifact.ArtifactKindFile {
			err := fmt.Errorf("validate instructions %q source: expected file artifact", instruction.ID().Name())
			options.Events.Emit(input.event(EventResourceLockFailed, err))
			return nil, err
		}
		content, err := directfile.ReadExact(ctx, resolution.View(), identity)
		if err != nil {
			wrapped := fmt.Errorf("read instructions %q source: %w", instruction.ID().Name(), err)
			options.Events.Emit(input.event(EventResourceLockFailed, wrapped))
			return nil, wrapped
		}
		materialization, err := artifact.NewFileMaterialization(
			identity,
			content.Bytes(),
			content.Mode().Perm()&0o111 != 0,
			false,
		)
		if err != nil {
			wrapped := fmt.Errorf("materialize instructions %q: %w", instruction.ID().Name(), err)
			options.Events.Emit(input.event(EventResourceLockFailed, wrapped))
			return nil, wrapped
		}
		fileUse, err := lock.NewExactFileUse(instruction.Scope(), false)
		if err != nil {
			wrapped := fmt.Errorf("lock instructions %q file use: %w", instruction.ID().Name(), err)
			options.Events.Emit(input.event(EventResourceLockFailed, wrapped))
			return nil, wrapped
		}
		derivation, err := lock.NewFileMaterializationDerivation(materialization)
		if err != nil {
			wrapped := fmt.Errorf("lock instructions %q derivation: %w", instruction.ID().Name(), err)
			options.Events.Emit(input.event(EventResourceLockFailed, wrapped))
			return nil, wrapped
		}
		subjectID, err := resourcetopology.Subject(instruction.ID())
		if err != nil {
			wrapped := fmt.Errorf("lower instructions %q topology: %w", instruction.ID().Name(), err)
			options.Events.Emit(input.event(EventResourceLockFailed, wrapped))
			return nil, wrapped
		}
		lockedInstruction, err := lock.NewExactSupplySubjectContract(lock.ExactSupplySubjectInput{
			EntityID:     instruction.ID(),
			SubjectID:    subjectID,
			ExactSupply:  identity,
			ExactFileUse: &fileUse,
			Derivation:   derivation,
		})
		if err != nil {
			wrapped := fmt.Errorf("lock instructions %q source: %w", instruction.ID().Name(), err)
			options.Events.Emit(input.event(EventResourceLockFailed, wrapped))
			return nil, wrapped
		}

		if err := lockedInstruction.ValidateFileMaterialization(materialization); err != nil {
			wrapped := fmt.Errorf("lock instructions %q materialization: %w", instruction.ID().Name(), err)
			options.Events.Emit(input.event(EventResourceLockFailed, wrapped))
			return nil, wrapped
		}
		projections, err := refine.InstructionsPathProjections(instruction)
		if err != nil {
			wrapped := fmt.Errorf("lock instructions %q projections: %w", instruction.ID().Name(), err)
			options.Events.Emit(input.event(EventResourceLockFailed, wrapped))
			return nil, wrapped
		}

		options.Events.Emit(input.event(EventResourceLocked, nil))
		lockedInstructions = append(lockedInstructions, lockedInstruction)
		lockedInstructions = append(lockedInstructions, projections...)
	}

	return lockedInstructions, nil
}
