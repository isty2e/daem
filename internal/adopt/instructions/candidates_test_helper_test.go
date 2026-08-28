package instructions

import (
	"context"

	"github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/target"
)

func collectCandidates(
	ctx context.Context,
	sourceDirectory adopt.SourceDirectory,
	selected target.Target,
	scope target.Scope,
) ([]adopt.Source, []adopt.Skipped, error) {
	collector := adopt.NewSkippedCollector()
	var sources []adopt.Source
	err := collector.Collect(func(skipped adopt.SkipEmitter) error {
		var err error
		sources, err = Candidates(ctx, sourceDirectory, selected, scope, skipped.WithRoute(selected, scope))
		return err
	})
	return sources, collector.Skipped(), err
}
