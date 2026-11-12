//go:build darwin || linux

package nativeoperation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeMovedStatePassesPostCommitObservationAndIndeterminateSettlement(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		name := "post-commit"
		if uncertain {
			name = "indeterminate-settlement"
		}
		t.Run(name, func(t *testing.T) {
			input, _, _ := nativeOrderApplyInput(t, target.ScopeProject)
			oldPath := filepath.Join(input.Paths.StateDir, "old-state.json")
			if err := os.Rename(input.Paths.StatefilePath, oldPath); err != nil {
				t.Fatal(err)
			}
			old, err := statefile.LoadOptionalPhysical(t.Context(), oldPath)
			if err != nil {
				t.Fatal(err)
			}
			// The fixture has an empty ordinary Before and no foreign owner rows.
			observed, err := mutation.ObservePersistedDirectoryEntryAuthority(oldPath)
			if err != nil {
				t.Fatal(err)
			}
			image, err := statefile.NewStateImage(old.Snapshot(), observed.Reference())
			if err != nil {
				t.Fatal(err)
			}
			content, err := statefile.MarshalImage(image)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(oldPath, content, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(oldPath, input.Paths.StatefilePath); err != nil {
				t.Fatal(err)
			}
			current, err := statefile.LoadOptionalPhysical(t.Context(), input.Paths.StatefilePath)
			if err != nil {
				t.Fatal(err)
			}
			input.StatefileCodec, err = input.StatefileCodec.ForObservedState(current.Codec())
			if err != nil {
				t.Fatal(err)
			}
			catalog, journalCodec, physicalCodec := input.Codecs, input.StateCodec, input.StatefileCodec
			input.RecoveryCodecs = func(context.Context) (aggregate.CodecCatalog, durable.SnapshotCodec, statefile.PhysicalCodec, error) {
				return catalog, journalCodec, physicalCodec, nil
			}
			failure := errors.New("state visibility requires settlement")
			store := &movedStateCommitStore{Store: input.Filesystem, path: input.Paths.StatefilePath}
			if uncertain {
				store.failure = failure
			}
			input.Filesystem = store
			plan, err := PrepareNativeApplyEffectPlan(input)
			if err != nil {
				t.Fatal(err)
			}
			result, err := ApplyNativeWithEffectPlan(t.Context(), input, plan, nativeOrderApplyOptions(t))
			if uncertain && !errors.Is(err, failure) || !uncertain && err != nil {
				t.Fatal("moved State failed its admitted terminal path", err)
			}
			if !result.State.Equal(input.Preparation.AfterState()) || store.stateWrites != 1 {
				t.Fatal("moved State was rolled back or replayed after visibility", err)
			}
			content, err = os.ReadFile(input.Paths.StatefilePath)
			if err != nil {
				t.Fatal(err)
			}
			after, err := statefile.UnmarshalImage(content)
			if err != nil || !after.Reference().Identity().Equal(input.Owner.StatefileAuthority()) {
				t.Fatal("moved State did not publish its fresh self-reference", err)
			}
			requireNoStateCodecJournal(t, input.Paths.RecoveryDir)
		})
	}
}

type movedStateCommitStore struct {
	mutationfs.Store
	path        string
	failure     error
	stateWrites int
}

func (store *movedStateCommitStore) ReplaceRootedFile(ctx context.Context, capability rootedpath.CommitCapability, content []byte, mode os.FileMode, expected mutationfs.EntryIdentity) error {
	path, err := capability.Destination().LexicalPath()
	if err != nil {
		return err
	}
	if err := store.Store.ReplaceRootedFile(ctx, capability, content, mode, expected); err != nil {
		return err
	}
	if path == store.path {
		store.stateWrites++
		if store.failure != nil {
			return movedStateIndeterminateError{store.failure}
		}
	}
	return nil
}

type movedStateIndeterminateError struct{ cause error }

func (failure movedStateIndeterminateError) Error() string { return failure.cause.Error() }

func (failure movedStateIndeterminateError) Unwrap() error { return failure.cause }

func (movedStateIndeterminateError) Kind() mutationfs.FailureKind {
	return mutationfs.FailureIndeterminateCommit
}
