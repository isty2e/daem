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
	collector := adopt.NewSkippedCollector()
	var hooks []adopt.Hook
	err := collector.Collect(func(skipped adopt.SkipEmitter) error {
		var err error
		hooks, err = Candidates(ctx, selected, scope, skipped.WithRoute(selected, scope))
		return err
	})
	return hooks, collector.Skipped(), err
}
