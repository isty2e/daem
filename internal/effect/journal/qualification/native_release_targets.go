package qualification

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/fileset"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

// MatchNativeReleaseTargets joins independently selected control paths to the
// complete recorded intent. Recorded comparison keys never supply I/O paths.
func MatchNativeReleaseTargets(ctx context.Context, release journalrecord.NativeRelease, selected journalrecord.NativeReleaseControlPaths, budget rootedpath.PhysicalTraversalBudget) ([]fileset.RecoveryTarget, error) {
	if ctx == nil || budget == nil {
		return nil, fmt.Errorf("native release target selection requires context and budget")
	}
	paths := []string{selected.Manifest, selected.DefaultLock, selected.State}
	references := make([]journalrecord.NativeReleaseNamespace, len(paths))
	for index, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if path == "" {
			return nil, fmt.Errorf("native release local target was not independently selected")
		}
		if index < 2 {
			referent, err := rootedpath.ResolveReferentPathBounded(path, physicalwork.MaximumPhysicalPathDepth, budget)
			if err != nil {
				return nil, err
			}
			path = referent
		}
		path, err := mutation.CanonicalDirectoryEntryPathBounded(path, physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return nil, err
		}
		paths[index] = path
		observation, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return nil, err
		}
		if exact, present := observation.Exact(); present {
			references[index], err = journalrecord.NewNativeReleaseExactNamespace(exact)
		} else {
			missing, _ := observation.Provisional()
			references[index], err = journalrecord.NewNativeReleaseProvisionalNamespace(missing)
		}
		if err != nil {
			return nil, err
		}
	}
	if err := release.MatchLocalTargets(references[0], references[1], references[2]); err != nil {
		return nil, err
	}
	files := release.LocalFiles()
	targets := make([]fileset.RecoveryTarget, len(files))
	for index, file := range files {
		targets[index] = fileset.RecoveryTarget{
			Path: paths[index], BeforeExists: file.BeforeExists(), BeforeHash: file.BeforeSHA256(),
			BeforeMode: file.BeforeMode(), AfterHash: file.AfterSHA256(), Write: file.Writes(), CommitPoint: file.Role() == journalrecord.NativeReleaseState,
		}
	}
	return targets, nil
}
