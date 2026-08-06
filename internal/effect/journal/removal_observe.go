package journal

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
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
) (recovery.RemovalResidueEntryObservation, mutationfs.EntryIdentity, error) {
	if ctx == nil {
		return recovery.RemovalResidueEntryObservation{}, nil, fmt.Errorf("removal residue observation context is required")
	}
	if filesystem == nil || capability == nil {
		return recovery.RemovalResidueEntryObservation{}, nil, fmt.Errorf("removal residue observation authority is required")
	}
	if err := ctx.Err(); err != nil {
		return recovery.RemovalResidueEntryObservation{}, nil, err
	}
	identity, err := filesystem.CaptureRootedEntryIdentity(ctx, capability)
	if errors.Is(err, fs.ErrNotExist) {
		observation, observationErr := recovery.NewRemovalResidueEntryObservation(
			recovery.RemovalResidueEntryAbsent,
			"", "", nil, "", "",
		)
		return observation, nil, observationErr
	}
	if err != nil {
		observation, observationErr := recovery.NewRemovalResidueEntryObservation(
			recovery.RemovalResidueEntryUnavailable,
			"", "", nil, "", "residue identity could not be observed",
		)
		return observation, nil, observationErr
	}

	switch identity.Kind() {
	case mutationfs.EntryKindFile:
		content, mode, observed, readErr := filesystem.ReadRootedRegularFileUpTo(
			ctx,
			capability,
			MaximumRecoveryBackupFileBytes,
		)
		if readErr != nil {
			observation, observationErr := recovery.NewRemovalResidueEntryObservation(
				recovery.RemovalResidueEntryUnavailable,
				"", "", nil, "", "residue file could not be read",
			)
			return observation, observed, observationErr
		}
		if !identity.Equal(observed) {
			observation, observationErr := recovery.NewRemovalResidueEntryObservation(
				recovery.RemovalResidueEntryUnavailable,
				"", "", nil, "", "residue identity changed while reading",
			)
			return observation, observed, observationErr
		}
		observation, observationErr := recovery.NewRemovalResidueEntryObservation(
			recovery.RemovalResidueEntryPresent,
			recovery.PathKindFile,
			string(artifact.HashFileContentWithExecutable(content, mode.Perm()&0o111 != 0)),
			recovery.NewPermissionMode(mode),
			"", "",
		)
		return observation, observed, observationErr
	case mutationfs.EntryKindDirectory:
		sink := newRootedTreeHashSink(ctx)
		observed, snapshotErr := filesystem.SnapshotRootedDirectory(
			ctx,
			capability,
			recoveryTreeTraversalLimits(),
			sink,
		)
		if snapshotErr != nil {
			observation, observationErr := recovery.NewRemovalResidueEntryObservation(
				recovery.RemovalResidueEntryUnavailable,
				"", "", nil, "", "residue directory could not be observed",
			)
			return observation, observed, observationErr
		}
		hash, hashErr := sink.hash()
		if hashErr != nil {
			observation, observationErr := recovery.NewRemovalResidueEntryObservation(
				recovery.RemovalResidueEntryUnavailable,
				"", "", nil, "", "residue directory identity could not be derived",
			)
			return observation, observed, observationErr
		}
		observation, observationErr := recovery.NewRemovalResidueEntryObservation(
			recovery.RemovalResidueEntryPresent,
			recovery.PathKindDirectory,
			string(hash), nil, "", "",
		)
		return observation, observed, observationErr
	case mutationfs.EntryKindSymlink, mutationfs.EntryKindSpecial:
		observation, observationErr := recovery.NewRemovalResidueEntryObservation(
			recovery.RemovalResidueEntryUnsupported,
			"", "", nil, "", fmt.Sprintf("residue entry kind %q is unsupported", identity.Kind()),
		)
		return observation, identity, observationErr
	default:
		observation, observationErr := recovery.NewRemovalResidueEntryObservation(
			recovery.RemovalResidueEntryUnavailable,
			"", "", nil, "", "residue entry kind is unavailable",
		)
		return observation, identity, observationErr
	}
}
