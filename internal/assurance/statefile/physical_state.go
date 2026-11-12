package statefile

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

// PhysicalState couples canonical facts and their observed physical encoding.
// The selected codec contains reference facts, not filesystem effect authority.
type PhysicalState struct {
	snapshot durable.Snapshot
	codec    PhysicalCodec
}

type physicalStateMissingError struct{ cause error }

func (failure *physicalStateMissingError) Error() string { return failure.cause.Error() }

func (failure *physicalStateMissingError) Unwrap() error { return failure.cause }

func (state PhysicalState) Snapshot() durable.Snapshot { return state.snapshot }

func (state PhysicalState) Codec() PhysicalCodec { return state.codec }

// LoadPhysical reads State without projecting away its physical reference.
func LoadPhysical(ctx context.Context, path string) (PhysicalState, error) {
	if ctx == nil {
		return PhysicalState{}, fmt.Errorf("statefile context is required")
	}
	codec, err := currentSnapshotCodec(ctx)
	if err != nil {
		return PhysicalState{}, err
	}
	return loadPhysical(ctx, path, PhysicalCodec{codec: codec}, nil)
}

func LoadOptionalPhysical(ctx context.Context, path string) (PhysicalState, error) {
	state, err := LoadPhysical(ctx, path)
	var missing *physicalStateMissingError
	if errors.As(err, &missing) {
		if err := ctx.Err(); err != nil {
			return PhysicalState{}, err
		}
		codec, err := currentSnapshotCodec(ctx)
		if err != nil {
			return PhysicalState{}, err
		}
		return PhysicalState{snapshot: durable.EmptySnapshot(), codec: PhysicalCodec{codec: codec, selection: &physicalStateSelection{}}}, nil
	}
	return state, err
}

func LoadOptionalPhysicalWithOptions(ctx context.Context, path string, options ReadOptions) (PhysicalState, error) {
	if ctx == nil || options.Codec.IsZero() || options.PhysicalPathBudget == nil {
		return PhysicalState{}, fmt.Errorf("statefile context, selected codec and path budget are required")
	}
	state, err := loadPhysical(ctx, path, options.Codec, options.PhysicalPathBudget)
	var missing *physicalStateMissingError
	if errors.As(err, &missing) {
		if err := ctx.Err(); err != nil {
			return PhysicalState{}, err
		}
		codec := options.Codec
		codec.selection = &physicalStateSelection{}
		return PhysicalState{snapshot: durable.EmptySnapshot(), codec: codec}, nil
	}
	return state, err
}

func loadPhysical(ctx context.Context, path string, codec PhysicalCodec, budget rootedpath.PhysicalTraversalBudget) (PhysicalState, error) {
	if err := ctx.Err(); err != nil {
		return PhysicalState{}, err
	}
	canonical, err := canonicalPhysicalPath(path, budget)
	if err != nil {
		return PhysicalState{}, fmt.Errorf("canonicalize selected statefile: %w", err)
	}
	loaded, err := storagecommit.ReadRegularFileSnapshotUpTo(ctx, canonical, maximumStatefileBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return PhysicalState{}, &physicalStateMissingError{cause: err}
		}
		return PhysicalState{}, err
	}
	if loaded.Mode().Perm() != statefileMode {
		return PhysicalState{}, fmt.Errorf("statefile %q permissions are %04o, want %04o", path, loaded.Mode().Perm(), statefileMode)
	}
	content := loaded.Content()
	version, err := statefileDocumentVersion(content)
	if err != nil {
		return PhysicalState{}, fmt.Errorf("decode statefile %q: %w", path, err)
	}
	var snapshot durable.Snapshot
	var recorded pathauthority.Reference
	if version == locatedSnapshotVersion {
		if _, owned := codec.codec.(imageFactCodec); owned {
			facts, err := codec.imageFactsCodec(ctx)
			if err != nil {
				return PhysicalState{}, err
			}
			image, err := unmarshalImage(content, facts)
			if err != nil {
				return PhysicalState{}, fmt.Errorf("decode State image %q: %w", path, err)
			}
			snapshot, recorded = image.Snapshot(), image.Reference()
		} else {
			snapshot, err = codec.DecodeState(content)
			if err == nil {
				recorded, err = stateImageReference(content)
			}
		}
	} else if version == retiredStatefileVersion {
		snapshot, err = decodeRetiredStatefile(content)
	} else {
		snapshot, err = codec.DecodeState(content)
	}
	if err != nil {
		return PhysicalState{}, fmt.Errorf("decode statefile %q: %w", path, err)
	}
	observed, err := observePhysicalReference(canonical, budget)
	if err != nil {
		return PhysicalState{}, fmt.Errorf("observe statefile %q: %w", path, err)
	}
	if err := validateSnapshotStatefileIdentity(snapshot, observed.Identity()); err != nil {
		return PhysicalState{}, fmt.Errorf("decode statefile %q: %w", path, err)
	}
	selection := &physicalStateSelection{before: recorded}
	if !recorded.IsZero() {
		selection.after, err = SelectStatefileReference(recorded, observed)
		if err != nil {
			return PhysicalState{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return PhysicalState{}, err
	}
	codec.selection = selection
	return PhysicalState{snapshot: snapshot, codec: codec}, nil
}

func canonicalPhysicalPath(path string, budget rootedpath.PhysicalTraversalBudget) (string, error) {
	if budget == nil {
		return mutation.CanonicalDirectoryEntryPath(path)
	}
	return mutation.CanonicalDirectoryEntryPathBounded(path, mutationfs.MaximumPhysicalPathDepth, budget)
}

func observePhysicalReference(path string, budget rootedpath.PhysicalTraversalBudget) (pathauthority.Reference, error) {
	var observed mutation.PersistedDirectoryEntryAuthority
	var err error
	if budget == nil {
		observed, err = mutation.ObservePersistedDirectoryEntryAuthority(path)
	} else {
		observed, err = mutation.ObservePersistedDirectoryEntryAuthorityBounded(path, mutationfs.MaximumPhysicalPathDepth, budget)
	}
	if err != nil {
		return pathauthority.Reference{}, err
	}
	return observed.Reference(), nil
}
