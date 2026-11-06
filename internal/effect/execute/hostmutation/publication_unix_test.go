//go:build darwin || linux

package hostmutation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/test/outputtest"
)

func TestPreparedPathCopiesShareConsumptionAndClose(t *testing.T) {
	for _, closeFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "published", true: "closed"}[closeFirst], func(t *testing.T) {
			root := t.TempDir()
			store := &countedFileStore{Store: storagecommit.Adapter{}}
			operation := projectMutationOperation(t, root, store)
			destination, err := operation.destinations.ResolveProject(outputtest.Parse(t, "managed"))
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := operation.CapturePath(t.Context(), destination, false, "", realization.PathProjectionFile, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			copied := prepared
			if closeFirst {
				if err := prepared.Close(); err != nil {
					t.Fatal(err)
				}
			} else if err := prepared.WriteFile(t.Context(), []byte("first"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := copied.WriteFile(t.Context(), []byte("second"), 0o600); err == nil {
				t.Fatal("copied precondition published after the shared capability was consumed")
			}
			if closeFirst {
				if store.writes != 0 {
					t.Fatal("closed precondition attempted a host write")
				}
				if _, err := os.Stat(filepath.Join(root, "managed")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("closed precondition changed host absence: %v", err)
				}
			} else {
				content, err := os.ReadFile(filepath.Join(root, "managed"))
				if err != nil || string(content) != "first" || store.writes != 1 {
					t.Fatalf("copied precondition changed publication: content=%q writes=%d error=%v", content, store.writes, err)
				}
			}
		})
	}
}

func TestPreparedDirectoryAbortPreservesPostEffectUncertainty(t *testing.T) {
	root, source := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "payload"), []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	view, err := access.OpenView(source)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := view.Hash(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := artifact.NewExactIdentity("test:directory", "", view.Kind(), hash)
	if err != nil {
		t.Fatal(err)
	}
	commitFailure := &uncommittedTreeFailure{cause: errors.New("refused prepared publication")}
	abortFailure := errors.New("uncertain prepared abort")
	store := &failedPreparedTreeStore{Store: storagecommit.Adapter{}, commitFailure: commitFailure, abortFailure: abortFailure}
	operation := projectMutationOperation(t, root, store)
	destination, err := operation.destinations.ResolveProject(outputtest.Parse(t, "managed"))
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := operation.CapturePath(t.Context(), destination, false, "", realization.PathProjectionDirectory, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	err = prepared.WriteDirectory(t.Context(), identity, view)
	if !errors.Is(err, commitFailure) || !errors.Is(err, abortFailure) || !mutationfs.MayHaveVisibleEffect(err) || !mutationfs.HasIndeterminateEffect(err) {
		t.Fatalf("directory publication lost abort uncertainty or its causes: %v", err)
	}
	if store.aborts != 1 {
		t.Fatalf("prepared tree abort count = %d, want 1", store.aborts)
	}
	if _, err := os.Stat(filepath.Join(root, "managed")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused publication changed host absence: %v", err)
	}
}

type countedFileStore struct {
	mutationfs.Store
	writes int
}

func (store *countedFileStore) CreateRootedFile(ctx context.Context, capability rootedpath.CommitCapability, content []byte, mode os.FileMode) error {
	store.writes++
	return store.Store.CreateRootedFile(ctx, capability, content, mode)
}

type failedPreparedTreeStore struct {
	mutationfs.Store
	commitFailure error
	abortFailure  error
	aborts        int
}

func (store *failedPreparedTreeStore) PrepareRootedTree(ctx context.Context, capability rootedpath.CommitCapability, populate func(mutationfs.RootedTreeWriter) error) (mutationfs.PreparedRootedTree, error) {
	prepared, err := store.Store.PrepareRootedTree(ctx, capability, populate)
	if err != nil {
		return nil, err
	}
	return &failedPreparedTree{PreparedRootedTree: prepared, store: store}, nil
}

type failedPreparedTree struct {
	mutationfs.PreparedRootedTree
	store *failedPreparedTreeStore
}

func (prepared *failedPreparedTree) Commit(context.Context) error {
	return prepared.store.commitFailure
}

func (prepared *failedPreparedTree) Abort(ctx context.Context) error {
	prepared.store.aborts++
	return errors.Join(prepared.PreparedRootedTree.Abort(ctx), prepared.store.abortFailure)
}

type uncommittedTreeFailure struct{ cause error }

func (failure *uncommittedTreeFailure) Error() string { return failure.cause.Error() }
func (failure *uncommittedTreeFailure) Unwrap() error { return failure.cause }
func (failure *uncommittedTreeFailure) Kind() mutationfs.FailureKind {
	return mutationfs.FailureUncommitted
}
