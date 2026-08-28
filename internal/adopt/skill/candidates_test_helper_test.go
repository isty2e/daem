package skill

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
	importedDestinations DestinationClaims,
	sourceIdentities *SourceIdentityCache,
	searchRoots *SearchRootCache,
) ([]adopt.Skill, []adopt.Scan, []adopt.Skipped, error) {
	collector := adopt.NewSkippedCollector()
	var skills []adopt.Skill
	var scans []adopt.Scan
	err := collector.Collect(func(skipped adopt.SkipEmitter) error {
		var err error
		skills, scans, err = Candidates(
			ctx,
			sourceDirectory,
			selected,
			scope,
			importedDestinations,
			sourceIdentities,
			searchRoots,
			skipped.WithRoute(selected, scope),
		)
		return err
	})
	return skills, scans, collector.Skipped(), err
}
