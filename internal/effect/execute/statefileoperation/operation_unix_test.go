//go:build darwin || linux

package statefileoperation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

func TestSelectedStatefileReadRetainsAllocationAndEntryCustody(t *testing.T) {
	store := &observedStatefileStore{Store: storagecommit.Adapter{}}
	selected, _, budget := selectedStatefileFixture(t, store)
	codec := statefile.DefaultPhysicalCodec()
	content, err := codec.EncodeState(durable.EmptySnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if outcome := selected.Commit(t.Context(), content, 0o600); !outcome.Committed() {
		t.Fatal(outcome.Failure())
	}

	foreign, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	remaining := budget.RemainingBytes()
	if _, err := selected.Read(t.Context(), codec, foreign); err == nil {
		t.Fatal("state reread accepted another operation's allocation")
	}
	if store.reads != 0 || budget.RemainingBytes() != remaining {
		t.Fatal("foreign-allocation refusal read state or charged the selected allocation")
	}
	state, err := selected.Read(t.Context(), codec, budget)
	if err != nil || !state.Equal(durable.EmptySnapshot()) {
		t.Fatalf("selected state reread = %v, %v", state, err)
	}
	if store.reads != 1 || remaining-budget.RemainingBytes() != int64(len(content)) {
		t.Fatal("state reread did not charge its actual bytes to the original allocation")
	}
	if err := selected.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := selected.Read(t.Context(), codec, budget); err == nil {
		t.Fatal("closed statefile retained read authority")
	}
	if outcome := selected.Commit(t.Context(), content, 0o600); outcome.Committed() || outcome.RequiresRecovery() {
		t.Fatal("closed statefile did not refuse before publication")
	}
}

func TestSelectedStatefileReadMissingAndUncertainBytes(t *testing.T) {
	store := &observedStatefileStore{Store: storagecommit.Adapter{}}
	selected, _, budget := selectedStatefileFixture(t, store)
	remaining := budget.RemainingBytes()
	state, err := selected.Read(t.Context(), statefile.DefaultPhysicalCodec(), budget)
	if err != nil || !state.Equal(durable.EmptySnapshot()) || budget.RemainingBytes() != remaining {
		t.Fatalf("missing statefile did not retain empty state and zero byte work: %v", err)
	}

	store.readFailure = errors.New("uncertain statefile read")
	if _, err := selected.Read(t.Context(), statefile.DefaultPhysicalCodec(), budget); !errors.Is(err, store.readFailure) {
		t.Fatalf("state reread error = %v, want injected read failure", err)
	}
	if remaining-budget.RemainingBytes() != min(remaining, recovery.MaximumRecoveryBackupFileBytes) {
		t.Fatal("uncertain state reread did not charge its full bounded read")
	}
}

func TestSelectedStatefileCommitRetainsVisibleUncertainty(t *testing.T) {
	refusal := &statefileCommitFailure{kind: mutationfs.FailureUncommitted}
	for _, test := range []struct {
		name      string
		failure   error
		publish   bool
		committed bool
		recovery  bool
	}{
		{name: "committed", publish: true, committed: true},
		{name: "uncommitted", failure: refusal},
		{name: "unknown", failure: errors.New("unclassified commit failure"), recovery: true},
		{name: "visible nested refusal", failure: mutationfs.MarkEffectIndeterminate(refusal), publish: true, recovery: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &reportedStatefileCommit{Store: storagecommit.Adapter{}, failure: test.failure, publish: test.publish}
			selected, path, _ := selectedStatefileFixture(t, store)
			outcome := selected.Commit(t.Context(), []byte("state\n"), 0o600)
			if outcome.Committed() != test.committed || outcome.RequiresRecovery() != test.recovery {
				t.Fatalf("statefile outcome = %#v, want committed=%v recovery=%v", outcome, test.committed, test.recovery)
			}
			if test.failure != nil && !errors.Is(outcome.Failure(), test.failure) {
				t.Fatalf("statefile outcome lost its cause: %v", outcome.Failure())
			}
			if test.publish {
				assertHostFileContent(t, path, "state\n")
			} else {
				assertHostMissing(t, path)
			}
		})
	}
}

func selectedStatefileFixture(t *testing.T, store mutationfs.Store) (*Selected, string, *physicalwork.Budget) {
	t.Helper()
	base := t.TempDir()
	path := filepath.Join(base, "state.json")
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	root, err := rootedpath.CaptureRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	entry, err := rootedpath.BindSelectedEntryAuthorityBounded(root, base, path, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	selected := NewSelected(store, entry, budget)
	t.Cleanup(func() {
		if err := selected.Close(); err != nil {
			t.Error(err)
		}
	})
	return selected, path, budget
}

type observedStatefileStore struct {
	mutationfs.Store
	reads       int
	readFailure error
}

func (store *observedStatefileStore) ReadRootedRegularFileUpTo(ctx context.Context, capability rootedpath.CommitCapability, maximum int64) ([]byte, os.FileMode, mutationfs.EntryIdentity, error) {
	store.reads++
	if store.readFailure != nil {
		return nil, 0, nil, store.readFailure
	}
	return store.Store.ReadRootedRegularFileUpTo(ctx, capability, maximum)
}

type reportedStatefileCommit struct {
	mutationfs.Store
	failure error
	publish bool
}

func (store *reportedStatefileCommit) CreateRootedFile(ctx context.Context, capability rootedpath.CommitCapability, content []byte, mode os.FileMode) error {
	if store.publish {
		return errors.Join(store.Store.CreateRootedFile(ctx, capability, content, mode), store.failure)
	}
	return errors.Join(store.failure, capability.Close())
}

type statefileCommitFailure struct{ kind mutationfs.FailureKind }

func (failure *statefileCommitFailure) Error() string                { return "classified statefile commit failure" }
func (failure *statefileCommitFailure) Kind() mutationfs.FailureKind { return failure.kind }
