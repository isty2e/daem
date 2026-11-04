package journal

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/supply/artifact"
)

// ObserveRootedRemovalEntry performs one bounded, no-follow observation of the
// exact residue selected by a recovery intent. It does not select a path,
// interpret a resource, or mutate the filesystem. The capability remains owned
// by the caller.
func ObserveRootedRemovalEntry(
	ctx context.Context,
	filesystem mutationfs.RootedReader,
	capability rootedpath.CommitCapability,
	budget *physicalwork.Budget,
	maximumWork workmeasure.ArtifactWork,
) (recovery.RemovalResidueEntryObservation, mutationfs.EntryIdentity, workmeasure.ArtifactWork, error) {
	if ctx == nil {
		return recovery.RemovalResidueEntryObservation{}, nil, workmeasure.ArtifactWork{}, fmt.Errorf("removal residue observation context is required")
	}
	if filesystem == nil || capability == nil {
		return recovery.RemovalResidueEntryObservation{}, nil, workmeasure.ArtifactWork{}, fmt.Errorf("removal residue observation authority is required")
	}
	if err := ctx.Err(); err != nil {
		return recovery.RemovalResidueEntryObservation{}, nil, workmeasure.ArtifactWork{}, err
	}
	if err := budget.AdmitObservation(); err != nil {
		return recovery.RemovalResidueEntryObservation{}, nil, workmeasure.ArtifactWork{}, err
	}
	identity, err := filesystem.CaptureRootedEntryIdentity(ctx, capability)
	if errors.Is(err, fs.ErrNotExist) {
		observation, observationErr := recovery.NewRemovalResidueEntryObservation(
			recovery.RemovalResidueEntryAbsent,
			"", "", nil, "", "",
		)
		return observation, nil, workmeasure.ArtifactWork{}, observationErr
	}
	if err != nil {
		if cancellationErr := removalObservationCancellation(err); cancellationErr != nil {
			return recovery.RemovalResidueEntryObservation{}, nil, workmeasure.ArtifactWork{}, cancellationErr
		}
		observation, observationErr := recovery.NewRemovalResidueEntryObservation(
			recovery.RemovalResidueEntryUnavailable,
			"", "", nil, "", "residue identity could not be observed",
		)
		return observation, nil, workmeasure.ArtifactWork{}, observationErr
	}

	switch identity.Kind() {
	case mutationfs.EntryKindFile:
		admittedWork, readerCapacity, workErr := boundedRemovalFileObservationWork(maximumWork)
		if workErr != nil {
			return recovery.RemovalResidueEntryObservation{}, identity, workmeasure.ArtifactWork{}, workErr
		}
		maximumBytes := max(int64(1), admittedWork.Bytes())
		content, mode, observed, readErr := filesystem.ReadRootedRegularFileUpTo(
			ctx,
			capability,
			maximumBytes,
		)
		if readErr != nil {
			if cancellationErr := removalObservationCancellation(readErr); cancellationErr != nil {
				return recovery.RemovalResidueEntryObservation{}, observed, workmeasure.ArtifactWork{}, cancellationErr
			}
			if chargeErr := budget.AdmitIndeterminateTreeWork(admittedWork, readerCapacity); chargeErr != nil {
				return recovery.RemovalResidueEntryObservation{}, observed, workmeasure.ArtifactWork{}, chargeErr
			}
			observation, observationErr := recovery.NewRemovalResidueEntryObservation(
				recovery.RemovalResidueEntryUnavailable,
				"", "", nil, "", "residue file could not be read",
			)
			return observation, observed, workmeasure.ArtifactWork{}, observationErr
		}
		work, workErr := workmeasure.NewArtifactWork(0, int64(len(content)))
		if workErr != nil {
			return recovery.RemovalResidueEntryObservation{}, observed, workmeasure.ArtifactWork{}, workErr
		}
		if workErr := budget.AdmitTreeWithin(work, maximumWork); workErr != nil {
			return recovery.RemovalResidueEntryObservation{}, observed, workmeasure.ArtifactWork{}, workErr
		}
		if !identity.Equal(observed) {
			observation, observationErr := recovery.NewRemovalResidueEntryObservation(
				recovery.RemovalResidueEntryUnavailable,
				"", "", nil, "", "residue identity changed while reading",
			)
			return observation, observed, work, observationErr
		}
		observation, observationErr := recovery.NewRemovalResidueEntryObservation(
			recovery.RemovalResidueEntryPresent,
			recovery.PathKindFile,
			string(artifact.HashFileContentWithExecutable(content, mode.Perm()&0o111 != 0)),
			recovery.NewPermissionMode(mode),
			"", "",
		)
		return observation, observed, work, observationErr
	case mutationfs.EntryKindDirectory:
		sink := newRootedTreeHashSink(ctx)
		limits, admittedWork, readerCapacity, limitErr := recoveryTreeTraversalLimitsForBudget(
			maximumWork,
			budget,
		)
		if limitErr != nil {
			return recovery.RemovalResidueEntryObservation{}, identity, workmeasure.ArtifactWork{}, limitErr
		}
		observed, snapshotErr := filesystem.SnapshotRootedDirectory(
			ctx,
			capability,
			limits,
			sink,
		)
		if snapshotErr != nil {
			if cancellationErr := removalObservationCancellation(snapshotErr); cancellationErr != nil {
				return recovery.RemovalResidueEntryObservation{}, observed, workmeasure.ArtifactWork{}, cancellationErr
			}
			if chargeErr := budget.AdmitIndeterminateDirectoryWork(admittedWork, readerCapacity); chargeErr != nil {
				return recovery.RemovalResidueEntryObservation{}, observed, workmeasure.ArtifactWork{}, chargeErr
			}
			observation, observationErr := recovery.NewRemovalResidueEntryObservation(
				recovery.RemovalResidueEntryUnavailable,
				"", "", nil, "", "residue directory could not be observed",
			)
			return observation, observed, workmeasure.ArtifactWork{}, observationErr
		}
		work, workErr := sink.removalWork()
		if workErr != nil {
			return recovery.RemovalResidueEntryObservation{}, observed, workmeasure.ArtifactWork{}, workErr
		}
		if workErr := budget.AdmitTreeWithin(work, maximumWork); workErr != nil {
			return recovery.RemovalResidueEntryObservation{}, observed, workmeasure.ArtifactWork{}, workErr
		}
		hash, hashErr := sink.hash()
		if hashErr != nil {
			observation, observationErr := recovery.NewRemovalResidueEntryObservation(
				recovery.RemovalResidueEntryUnavailable,
				"", "", nil, "", "residue directory identity could not be derived",
			)
			return observation, observed, work, observationErr
		}
		observation, observationErr := recovery.NewRemovalResidueEntryObservation(
			recovery.RemovalResidueEntryPresent,
			recovery.PathKindDirectory,
			string(hash), nil, "", "",
		)
		return observation, observed, work, observationErr
	case mutationfs.EntryKindSymlink, mutationfs.EntryKindSpecial:
		observation, observationErr := recovery.NewRemovalResidueEntryObservation(
			recovery.RemovalResidueEntryUnsupported,
			"", "", nil, "", fmt.Sprintf("residue entry kind %q is unsupported", identity.Kind()),
		)
		return observation, identity, workmeasure.ArtifactWork{}, observationErr
	default:
		observation, observationErr := recovery.NewRemovalResidueEntryObservation(
			recovery.RemovalResidueEntryUnavailable,
			"", "", nil, "", "residue entry kind is unavailable",
		)
		return observation, identity, workmeasure.ArtifactWork{}, observationErr
	}
}

func removalObservationCancellation(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return err
	case errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return nil
	}
}

func recoveryTreeTraversalLimitsForBudget(
	maximumWork workmeasure.ArtifactWork,
	budget *physicalwork.Budget,
) (mutationfs.TreeTraversalLimits, workmeasure.ArtifactWork, workmeasure.ArtifactWork, error) {
	if budget == nil {
		return mutationfs.TreeTraversalLimits{}, workmeasure.ArtifactWork{}, workmeasure.ArtifactWork{}, fmt.Errorf(
			"removal directory observation budget is required",
		)
	}
	if budget.RemainingEntries() <= 0 {
		return mutationfs.TreeTraversalLimits{}, workmeasure.ArtifactWork{}, workmeasure.ArtifactWork{}, fmt.Errorf(
			"removal directory observation requires overflow-entry capacity",
		)
	}
	maximumEntries := min(
		maximumRecoveryTreeEntries,
		maximumWork.Entries(),
		budget.RemainingEntries()-1,
	)
	maximumBytes := min(
		int64(maximumRecoveryTreeBytes),
		maximumWork.Bytes(),
		budget.RemainingBytes(),
	)
	admittedWork, err := workmeasure.NewArtifactWork(maximumEntries, maximumBytes)
	if err != nil {
		return mutationfs.TreeTraversalLimits{}, workmeasure.ArtifactWork{}, workmeasure.ArtifactWork{}, err
	}
	readerCapacity, err := workmeasure.NewArtifactWork(
		maximumEntries+1,
		maximumBytes,
	)
	if err != nil {
		return mutationfs.TreeTraversalLimits{}, workmeasure.ArtifactWork{}, workmeasure.ArtifactWork{}, err
	}
	limits, err := mutationfs.NewTreeTraversalLimits(
		admittedWork.Entries(),
		recovery.MaximumArtifactTreeDepth,
		admittedWork.Bytes(),
	)
	return limits, admittedWork, readerCapacity, err
}

func boundedRemovalFileObservationWork(
	maximumWork workmeasure.ArtifactWork,
) (workmeasure.ArtifactWork, workmeasure.ArtifactWork, error) {
	admittedWork, err := workmeasure.NewArtifactWork(
		0,
		min(recovery.MaximumRecoveryBackupFileBytes, maximumWork.Bytes()),
	)
	if err != nil {
		return workmeasure.ArtifactWork{}, workmeasure.ArtifactWork{}, err
	}
	readerCapacity, err := workmeasure.NewArtifactWork(
		0,
		max(int64(1), admittedWork.Bytes()),
	)
	return admittedWork, readerCapacity, err
}
