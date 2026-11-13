package refresh

import (
	"context"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/recoverygate"
)

func replanAfterMetadataConversion(ctx context.Context, execution *preparedExecution) error {
	original := execution.planned
	inputs, err := original.revisions.Subset(original.authority.nonMetadataRevisions...)
	if err != nil {
		return err
	}
	if matches, err := inputs.MatchesCurrent(ctx); err != nil {
		return err
	} else if !matches {
		return mutation.StalePlanError{}
	}
	barrier, err := recoverygate.NewEffectAuthority(ctx, original.paths)
	if err != nil {
		return err
	}
	if !original.barrier.Equal(barrier) {
		return mutation.StalePlanError{}
	}
	current, err := planAtPathsWithBarrier(
		ctx, execution.input, original.timeout, execution.options,
		original.paths, original.result.Mode, &barrier,
	)
	if err != nil {
		return err
	}
	current, err = stabilizePlan(ctx, execution.input, execution.options, current, execution.root)
	if err != nil {
		return err
	}
	if !original.currentState.Equal(current.currentState) ||
		!original.fingerprint.Equal(current.fingerprint) ||
		!original.authority.authorityFingerprint.Equal(current.authority.authorityFingerprint) {
		return mutation.StalePlanError{}
	}
	if matches, err := inputs.MatchesCurrent(ctx); err != nil {
		return err
	} else if !matches {
		return mutation.StalePlanError{}
	}
	execution.planned = current
	return nil
}
