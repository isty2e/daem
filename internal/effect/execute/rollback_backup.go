package execute

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/filesystem/artifactstage"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

// rollbackBackup is a process-private artifact staged to reverse effects from
// the current recovery attempt. It is not durable journal authority.
type rollbackBackup struct {
	view     access.View
	identity artifact.ExactIdentity
}

func newRollbackBackup(
	path string,
	reference string,
	kind string,
	contentHash string,
) (rollbackBackup, error) {
	view, err := access.OpenView(path)
	if err != nil {
		return rollbackBackup{}, err
	}
	identity, err := artifact.NewExactIdentity(
		artifact.SourceID("recovery:rollback"),
		artifact.ResolvedRef(reference),
		artifact.ArtifactKind(kind),
		artifact.ContentHash(contentHash),
	)
	if err != nil {
		return rollbackBackup{}, err
	}
	return rollbackBackup{view: view, identity: identity}, nil
}

func (backup rollbackBackup) readFile(ctx context.Context, maximumBytes int64) ([]byte, error) {
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

type boundedRollbackDirectorySource struct {
	backup rollbackBackup
	work   recovery.ArtifactWork
}

func (source boundedRollbackDirectorySource) copyDirectory(
	ctx context.Context,
	writer mutationfs.RootedTreeWriter,
) error {
	if source.backup.identity.Kind() != artifact.ArtifactKindDirectory {
		return fmt.Errorf("rollback backup is not a directory")
	}
	sink, err := artifactstage.New(writer)
	if err != nil {
		return err
	}
	traversalLimit, err := access.NewTraversalLimit(
		uint64(source.work.Entries()+1),
		max(int64(1), source.work.Bytes()),
	)
	if err != nil {
		return err
	}
	structureLimit, err := access.NewTreeStructureLimit(
		source.work.Entries(),
		recovery.MaximumArtifactTreeDepth,
	)
	if err != nil {
		return err
	}
	return source.backup.view.CopyVerifiedWithLimits(
		ctx,
		source.backup.identity,
		sink,
		traversalLimit,
		structureLimit,
	)
}
