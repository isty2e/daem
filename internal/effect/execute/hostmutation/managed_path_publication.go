package hostmutation

import (
	"context"
	"fmt"
	"io/fs"

	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/payload"
	"github.com/isty2e/daem/internal/realization"
)

func (operation *Operation) ApplyManagedPathStep(ctx context.Context, step ManagedPathOperation, payloads payload.PayloadSet) (EffectProgress, error) {
	if step.phase != ManagedPathPublishPhase && step.phase != ManagedPathRetirePhase {
		return EffectNotStarted, fmt.Errorf("managed path operation is required")
	}
	effect := step.effect
	if effect.Kind() == effectpreparation.ManagedPathEffectRecord {
		destination, err := operation.destinations.Resolve(effect.Scope(), effect.Destination())
		if err != nil {
			return EffectNotStarted, err
		}
		if err := operation.verifyInstalledPath(ctx, destination, true, effect.LiveHash(), effect.ContentKind()); err != nil {
			return EffectNotStarted, err
		}
		return EffectExpectedAfter, nil
	}
	if step.relocation {
		if step.phase == ManagedPathPublishPhase {
			return operation.publishManagedPathRelocation(ctx, effect, payloads)
		}
		return operation.retireManagedPathRelocation(ctx, effect)
	}
	return operation.applyManagedPathMutation(ctx, effect, payloads)
}

func (operation *Operation) applyManagedPathMutation(ctx context.Context, effect effectpreparation.ManagedPathEffect, payloads payload.PayloadSet) (EffectProgress, error) {
	destination, err := operation.destinations.Resolve(effect.Scope(), effect.Destination())
	if err != nil {
		return EffectNotStarted, err
	}
	precondition, err := operation.CapturePath(ctx, destination, effect.Kind() != effectpreparation.ManagedPathEffectCreate, effect.LiveHash(), effect.ContentKind(), nil)
	if err != nil {
		return EffectNotStarted, err
	}
	defer precondition.Close()

	if effect.Kind() == effectpreparation.ManagedPathEffectRemove {
		if err := precondition.Remove(ctx); err != nil {
			return ProgressAfterMutationError(err), fmt.Errorf("remove managed destination %q: %w", effect.Destination(), err)
		}
		if err := operation.verifyInstalledPath(ctx, destination, false, "", effect.ContentKind()); err != nil {
			return EffectIndeterminate, mutationfs.MarkEffectIndeterminate(fmt.Errorf("verify removed managed destination %q: %w", effect.Destination(), err))
		}
		return EffectExpectedAfter, nil
	}

	prepared, err := effectpreparation.ManagedPathPayload(effect, payloads)
	if err != nil {
		return EffectNotStarted, err
	}
	if err := commitManagedPathDestination(ctx, effect, prepared, precondition); err != nil {
		return ProgressAfterMutationError(err), fmt.Errorf("write destination %q: %w", effect.Destination(), err)
	}
	if err := operation.verifyInstalledPath(ctx, destination, true, effect.DesiredHash(), effect.ContentKind()); err != nil {
		return EffectIndeterminate, mutationfs.MarkEffectIndeterminate(fmt.Errorf("verify managed destination %q: %w", effect.Destination(), err))
	}
	return EffectExpectedAfter, nil
}

func (operation *Operation) publishManagedPathRelocation(ctx context.Context, effect effectpreparation.ManagedPathEffect, payloads payload.PayloadSet) (EffectProgress, error) {
	destination, err := operation.destinations.Resolve(effect.Scope(), effect.Destination())
	if err != nil {
		return EffectNotStarted, err
	}
	precondition, err := operation.CapturePath(ctx, destination, false, "", effect.ContentKind(), nil)
	if err != nil {
		return EffectNotStarted, err
	}
	defer precondition.Close()
	prepared, err := effectpreparation.ManagedPathPayload(effect, payloads)
	if err != nil {
		return EffectNotStarted, err
	}
	if err := commitManagedPathDestination(ctx, effect, prepared, precondition); err != nil {
		return ProgressAfterMutationError(err), fmt.Errorf("write relocated managed destination %q: %w", effect.Destination(), err)
	}
	if err := operation.verifyInstalledPath(ctx, destination, true, effect.DesiredHash(), effect.ContentKind()); err != nil {
		return EffectIndeterminate, mutationfs.MarkEffectIndeterminate(fmt.Errorf("verify relocated managed destination %q: %w", effect.Destination(), err))
	}
	return EffectExpectedAfter, nil
}

func (operation *Operation) retireManagedPathRelocation(ctx context.Context, effect effectpreparation.ManagedPathEffect) (EffectProgress, error) {
	previous, _ := effect.PreviousState()
	destination, err := operation.destinations.Resolve(previous.Scope(), previous.Destination())
	if err != nil {
		return EffectNotStarted, err
	}
	precondition, err := operation.CapturePath(ctx, destination, true, effect.PreviousLiveHash(), previous.ContentKind(), nil)
	if err != nil {
		return EffectNotStarted, err
	}
	defer precondition.Close()
	if err := precondition.Remove(ctx); err != nil {
		return ProgressAfterMutationError(err), fmt.Errorf("remove previous managed destination %q: %w", previous.Destination(), err)
	}
	if err := operation.verifyInstalledPath(ctx, destination, false, "", previous.ContentKind()); err != nil {
		return EffectIndeterminate, mutationfs.MarkEffectIndeterminate(fmt.Errorf("verify previous managed destination removal %q: %w", previous.Destination(), err))
	}
	return EffectExpectedAfter, nil
}

func commitManagedPathDestination(ctx context.Context, effect effectpreparation.ManagedPathEffect, prepared payload.Payload, precondition PreparedPath) error {
	switch effect.ContentKind() {
	case realization.PathProjectionFile:
		file, ok := prepared.File()
		if !ok {
			return fmt.Errorf("managed file effect for %q has a non-file payload", effect.Destination())
		}
		return precondition.WriteFile(ctx, file.Bytes(), precondition.publishMode(effect.DesiredFileMode()))
	case realization.PathProjectionDirectory:
		directory, ok := prepared.Directory()
		if !ok {
			return fmt.Errorf("managed directory effect for %q has a non-directory payload", effect.Destination())
		}
		precondition.state.previousMembers = effect.PreviousMembers()
		return precondition.WriteDirectory(ctx, directory.Identity(), directory.View())
	default:
		return fmt.Errorf("managed path content kind %q is unsupported", effect.ContentKind())
	}
}

func (path PreparedPath) publishMode(createMode fs.FileMode) fs.FileMode {
	if path.state.existed {
		return path.state.fileMode
	}
	return createMode
}
