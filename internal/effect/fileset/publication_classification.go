package fileset

import (
	"context"
	"errors"
	"fmt"
	"os"

	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

func classifyPublicationTargets(ctx context.Context, intent PublicationIntent) (targetClassification, error) {
	if ctx == nil {
		return targetClassification{}, fmt.Errorf("publication classification context is required")
	}
	if err := ctx.Err(); err != nil {
		return targetClassification{}, err
	}
	if err := intent.Validate(); err != nil {
		return targetClassification{}, err
	}
	result := targetClassification{cleanAfter: true, recoverable: true}
	for _, target := range intent.targets {
		position, err := classifyPublicationTarget(ctx, target)
		if err != nil {
			return targetClassification{}, fmt.Errorf("inspect publication target %q: %w", target.path, err)
		}
		result.cleanAfter = result.cleanAfter && position.cleanAfter
		result.recoverable = result.recoverable && position.recoverable
	}
	if err := ctx.Err(); err != nil {
		return targetClassification{}, err
	}
	return result, nil
}

func classifyPublicationTarget(ctx context.Context, target PublicationTarget) (targetClassification, error) {
	if err := ctx.Err(); err != nil {
		return targetClassification{}, err
	}
	info, err := os.Lstat(target.path)
	if canceled := ctx.Err(); canceled != nil {
		return targetClassification{}, canceled
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return targetClassification{}, err
	}
	if err == nil {
		beforeSize := target.before.exists && info.Size() == target.before.size
		afterSize := target.after.exists && info.Size() == target.after.size
		if !info.Mode().IsRegular() || !(beforeSize || afterSize) {
			return targetClassification{}, nil
		}
	}

	snapshot, err := storagecommit.ReadRegularFileSnapshotUpTo(ctx, target.path, max(target.before.size, target.after.size))
	if canceled := ctx.Err(); canceled != nil {
		return targetClassification{}, canceled
	}
	if errors.Is(err, os.ErrNotExist) {
		return classifyPublicationImage(target, AbsentPublicationImage()), nil
	}
	if errors.Is(err, storagecommit.ErrRegularFileReadLimitExceeded) {
		return targetClassification{}, nil
	}
	if err != nil {
		return targetClassification{}, err
	}
	image, err := NewPublicationImage(snapshot.Content(), snapshot.Mode().Perm())
	if canceled := ctx.Err(); canceled != nil {
		return targetClassification{}, canceled
	}
	if err != nil {
		return targetClassification{}, err
	}
	return classifyPublicationImage(target, image), nil
}

func classifyPublicationImage(target PublicationTarget, current PublicationImage) targetClassification {
	after := current.Equal(target.after)
	return targetClassification{cleanAfter: after, recoverable: after || current.Equal(target.before)}
}
