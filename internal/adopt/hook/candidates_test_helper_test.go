package hook

import (
	"context"

	"github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/target"
)

func collectCandidates(
	ctx context.Context,
	selected target.Target,
	scope target.Scope,
) ([]adopt.Hook, []adopt.Skipped, error) {
	return collectCandidatesWithHooks(ctx, selected, scope, candidateHooks{})
}

func collectCandidatesWithHooks(
	ctx context.Context,
	selected target.Target,
	scope target.Scope,
	hooks candidateHooks,
) ([]adopt.Hook, []adopt.Skipped, error) {
	collector := adopt.NewSkippedCollector()
	var imported []adopt.Hook
	err := collector.Collect(func(skipped adopt.SkipEmitter) error {
		var err error
		imported, err = candidatesWithHooks(ctx, selected, scope, skipped.WithRoute(selected, scope), hooks)
		return err
	})
	return imported, collector.Skipped(), err
}
