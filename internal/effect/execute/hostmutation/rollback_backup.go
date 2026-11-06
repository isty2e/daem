package hostmutation

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/filesystem/artifactstage"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

// RollbackBackup is a process-private artifact staged to reverse effects from
// the current recovery attempt. It is not durable journal authority.
type RollbackBackup struct {
	view     access.View
	identity artifact.ExactIdentity
}

func OpenRollbackBackup(
	path string,
	reference string,
	kind string,
	contentHash string,
) (RollbackBackup, error) {
	view, err := access.OpenView(path)
	if err != nil {
		return RollbackBackup{}, err
	}
	identity, err := artifact.NewExactIdentity(
		artifact.SourceID("recovery:rollback"),
		artifact.ResolvedRef(reference),
		artifact.ArtifactKind(kind),
		artifact.ContentHash(contentHash),
	)
	if err != nil {
		return RollbackBackup{}, err
	}
	return RollbackBackup{view: view, identity: identity}, nil
}

func (backup RollbackBackup) ReadFile(ctx context.Context, maximumBytes int64) ([]byte, error) {
	content, err := backup.view.ReadRootFileVerified(
		ctx,
		backup.identity,
		maximumBytes,
	)
	if err != nil {
		return nil, err
	}
	return content.Bytes(), nil
}

func (backup RollbackBackup) CopyDirectory(
	ctx context.Context,
	writer mutationfs.RootedTreeWriter,
	work workmeasure.ArtifactWork,
) error {
	if backup.identity.Kind() != artifact.ArtifactKindDirectory {
		return fmt.Errorf("rollback backup is not a directory")
	}
	sink, err := artifactstage.New(writer)
	if err != nil {
		return err
	}
	traversalLimit, err := access.NewTraversalLimit(
		uint64(work.Entries()+1),
		max(int64(1), work.Bytes()),
	)
	if err != nil {
		return err
	}
	structureLimit, err := access.NewTreeStructureLimit(
		work.Entries(),
		recovery.MaximumArtifactTreeDepth,
	)
	if err != nil {
		return err
	}
	return backup.view.CopyVerifiedWithLimits(
		ctx,
		backup.identity,
		sink,
		traversalLimit,
		structureLimit,
	)
}
