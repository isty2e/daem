package statefile

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

// ReadOptions binds filesystem loading to an operation-selected codec and path budget.
type ReadOptions struct {
	Codec              PhysicalCodec
	PhysicalPathBudget rootedpath.PhysicalTraversalBudget
}

// LoadOptionalWithOptions reads complete state with the selected codec. Missing
// files and exact empty retirement records retain their ordinary empty meaning.
func LoadOptionalWithOptions(ctx context.Context, path string, options ReadOptions) (durable.Snapshot, error) {
	if ctx == nil || options.Codec.IsZero() || options.PhysicalPathBudget == nil {
		return durable.Snapshot{}, fmt.Errorf("statefile context, selected codec and path budget are required")
	}
	if err := ctx.Err(); err != nil {
		return durable.Snapshot{}, err
	}
	canonical, err := mutation.CanonicalDirectoryEntryPathBounded(path, mutationfs.MaximumPhysicalPathDepth, options.PhysicalPathBudget)
	if err != nil {
		return durable.Snapshot{}, fmt.Errorf("canonicalize selected statefile: %w", err)
	}
	loaded, err := storagecommit.ReadRegularFileSnapshotUpTo(ctx, canonical, maximumStatefileBytes)
	if errors.Is(err, fs.ErrNotExist) {
		if err := ctx.Err(); err != nil {
			return durable.Snapshot{}, err
		}
		return durable.EmptySnapshot(), nil
	}
	if err != nil {
		return durable.Snapshot{}, err
	}
	if loaded.Mode().Perm() != statefileMode {
		return durable.Snapshot{}, fmt.Errorf("selected statefile permissions must be %04o", statefileMode)
	}
	content := loaded.Content()
	version, err := statefileDocumentVersion(content)
	if err != nil {
		return durable.Snapshot{}, err
	}
	var snapshot durable.Snapshot
	if version == retiredStatefileVersion {
		snapshot, err = decodeRetiredStatefile(content)
	} else {
		snapshot, err = options.Codec.DecodeState(content)
	}
	if err != nil {
		return durable.Snapshot{}, fmt.Errorf("decode selected statefile: %w", err)
	}
	authority, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(canonical, mutationfs.MaximumPhysicalPathDepth, options.PhysicalPathBudget)
	if err != nil {
		return durable.Snapshot{}, err
	}
	if err := validateLoadedStateAuthority(snapshot, authority); err != nil {
		return durable.Snapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return durable.Snapshot{}, err
	}
	return snapshot, nil
}
