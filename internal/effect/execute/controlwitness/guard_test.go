package controlwitness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

func TestExecutionGuardRetainsObservedPresenceAndIdentity(t *testing.T) {
	for _, present := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "present"}[present], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			if present {
				if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			parent, err := physicalwork.NewBudget(0)
			if err != nil {
				t.Fatal(err)
			}
			root, bound, err := rootedpath.CaptureDestinationBounded(path, physicalwork.MaximumPhysicalPathDepth, parent)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = root.Close() })
			guard := New(storagecommit.Adapter{})
			if err := guard.BindStatefile(root, bound); err != nil {
				t.Fatal(err)
			}
			before, err := guard.Observe(t.Context(), parent)
			if err != nil {
				t.Fatal(err)
			}
			after, err := guard.Observe(t.Context(), parent)
			if err != nil {
				t.Fatal(err)
			}
			if err := guard.Establish(before, after); err != nil {
				t.Fatal(err)
			}
			if err := guard.Reserve(2, parent); err != nil {
				t.Fatal(err)
			}
			if err := guard.BeginExecution(parent); err != nil {
				t.Fatal(err)
			}
			if err := guard.Validate(t.Context()); err != nil {
				t.Fatalf("unchanged control entry refused: %v", err)
			}
			replacement := path + ".replacement"
			if err := os.WriteFile(replacement, []byte("after"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(replacement, path); err != nil {
				t.Fatal(err)
			}
			var stale mutation.StaleSnapshotError
			if err := guard.Validate(t.Context()); !errors.As(err, &stale) {
				t.Fatalf("changed control entry error = %v, want stale snapshot", err)
			}
		})
	}
}

func TestUnclassifiedExecutionRefusesBeforeTakingReservedCapacity(t *testing.T) {
	guard := New(nil)
	parent, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.BeginExecution(parent); err == nil {
		t.Fatal("unclassified execution was admitted")
	}
	if _, err := parent.BeginReservedSemanticExecution(); err != nil {
		t.Fatalf("failed guard activation spent the parent's allocation: %v", err)
	}
}

func TestEqualObservationsCannotClassifyAnotherGuard(t *testing.T) {
	parent, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "state.json")
	root, bound, err := rootedpath.CaptureDestinationBounded(path, physicalwork.MaximumPhysicalPathDepth, parent)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	first, second := New(storagecommit.Adapter{}), New(storagecommit.Adapter{})
	for _, guard := range []*Guard{first, second} {
		if err := guard.BindStatefile(root, bound); err != nil {
			t.Fatal(err)
		}
	}
	observed, err := first.Observe(t.Context(), parent)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Establish(observed, observed); err != nil {
		t.Fatalf("same-source control failed: %v", err)
	}
	var stale mutation.StaleSnapshotError
	if err := second.Establish(observed, observed); !errors.As(err, &stale) {
		t.Fatalf("foreign observations classified another guard: %v", err)
	}
}

func TestNativeDelegationHasOneScopedDecisionOwner(t *testing.T) {
	guard := New(nil)
	expected := errors.New("native qualification refused")
	calls := 0
	check := func(context.Context) error { calls++; return expected }
	release, err := guard.InstallNativeCheck(check)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.Validate(t.Context()); !errors.Is(err, expected) {
		t.Fatalf("delegated validation error = %v, want native refusal", err)
	}
	if _, err := guard.InstallNativeCheck(check); err == nil {
		t.Fatal("second semantic owner was admitted")
	}
	release()
	if err := guard.Validate(t.Context()); err != nil || calls != 1 {
		t.Fatalf("released guard error = %v, native calls = %d, want nil/1", err, calls)
	}
	releaseNext, err := guard.InstallNativeCheck(check)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseNext()
	release()
	if err := guard.Validate(t.Context()); !errors.Is(err, expected) || calls != 2 {
		t.Fatalf("old release changed the next scope: error = %v, native calls = %d", err, calls)
	}
}
