package statefile

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

var ErrStatefilePresent = errors.New("the selected state file is present")

// MissingState retains a no-follow absence observation, not a write capability.
type MissingState struct {
	path        string
	destination rootedpath.Destination
	ownerPath   mutation.DirectoryEntryAuthorityObservation
}

func (missing MissingState) Path() string { return missing.path }

func (missing MissingState) OwnerPath() mutation.DirectoryEntryAuthorityObservation {
	return missing.ownerPath
}

func (missing MissingState) Equal(other MissingState) bool {
	if missing.path == "" || other.path == "" {
		return missing.path == other.path
	}
	return missing.path == other.path && missing.destination.Equal(other.destination) &&
		missing.ownerPath.Equal(other.ownerPath)
}

func ObserveMissingState(ctx context.Context, selected string, budget rootedpath.PhysicalTraversalBudget) (missing MissingState, returnErr error) {
	if ctx == nil || budget == nil {
		return MissingState{}, fmt.Errorf("statefile absence requires context and physical work budget")
	}
	if err := ctx.Err(); err != nil {
		return MissingState{}, err
	}
	if selected == "" {
		return MissingState{}, fmt.Errorf("statefile path is required")
	}
	path, err := filepath.Abs(selected)
	if err != nil {
		return MissingState{}, err
	}
	depth := physicalwork.MaximumPhysicalPathDepth
	root, destination, err := rootedpath.CaptureCanonicalDestinationBounded(path, depth, budget)
	if err != nil {
		return MissingState{}, err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close(), ctx.Err()) }()
	capability, err := root.AcquireBounded(destination, depth, budget)
	if err != nil {
		return MissingState{}, err
	}
	defer func() { returnErr = errors.Join(returnErr, capability.Close()) }()
	_, err = storagecommit.CaptureRootedEntryIdentity(ctx, capability)
	if err == nil {
		return MissingState{}, ErrStatefilePresent
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return MissingState{}, err
	}
	ownerPath, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, depth, budget)
	if err != nil {
		return MissingState{}, err
	}

	// Comparison can consult the selected spelling again; reject a new root or
	// an entry that appeared instead of binding that spelling to the old miss.
	current, currentDestination, err := rootedpath.CaptureCanonicalDestinationBounded(path, depth, budget)
	if err != nil {
		return MissingState{}, err
	}
	defer func() { returnErr = errors.Join(returnErr, current.Close()) }()
	if !destination.Equal(currentDestination) {
		return MissingState{}, mutation.StaleSnapshotError{}
	}
	_, err = storagecommit.CaptureRootedEntryIdentity(ctx, capability)
	if !errors.Is(err, fs.ErrNotExist) {
		return MissingState{}, errors.Join(mutation.StaleSnapshotError{}, err)
	}
	return MissingState{path: path, destination: destination, ownerPath: ownerPath}, nil
}
