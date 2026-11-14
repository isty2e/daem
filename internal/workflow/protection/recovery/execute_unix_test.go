//go:build darwin || linux

package recovery

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/test/ownershiptest"
)

type stoppedBudget struct{ stopped bool }

func (budget *stoppedBudget) AdmitPathComponents(int) error {
	if budget.stopped {
		return errors.New("physical work exhausted")
	}
	return nil
}

func TestPermissionRecoverySharesGlobalMetadataLease(t *testing.T) {
	fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml")
	prepared, found, err := Prepare(t.Context(), fixture.Paths, physicalwork.NewCaptureBudget(nil))
	if err != nil || !found {
		t.Fatal("permission recovery fixture is unavailable", err)
	}
	marker, err := recoverygate.GlobalMetadataAuthorityPath(fixture.Paths)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{Path: marker, Access: mutation.AccessExclusive, Effect: mutation.PathEffectDirectoryEntry})
	if err != nil {
		t.Fatal(err)
	}
	store, err := mutation.NewStore(fixture.Paths.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.Acquire(t.Context(), writer)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	_, err = Execute(ctx, prepared)
	var canceled mutation.CancellationError
	if !errors.As(err, &canceled) {
		t.Fatal("permission recovery did not wait for the publisher lease", err)
	}
	rows, err := fixture.Store.LoadPendingProtections(t.Context())
	if err != nil || len(rows) != 1 {
		t.Fatal("blocked permission recovery removed its fence", err)
	}
	info, err := os.Stat(fixture.Documents[0])
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatal("blocked permission recovery changed permissions", err)
	}
}

func TestPermissionExecutionCannotReplenishItsPlanningBudget(t *testing.T) {
	fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml")
	budget := &stoppedBudget{}
	prepared, found, err := Prepare(t.Context(), fixture.Paths, budget)
	if err != nil || !found {
		t.Fatalf("prepare: %v", err)
	}
	budget.stopped = true
	if _, err := Execute(t.Context(), prepared); err == nil {
		t.Fatal("executor allocated fresh traversal credit")
	}
	info, _ := os.Stat(fixture.Documents[0])
	if info.Mode().Perm() != 0o644 {
		t.Fatal("exhausted operation changed permissions")
	}
	rows, err := fixture.Store.LoadPendingProtections(t.Context())
	if err != nil || len(rows) != 1 {
		t.Fatal("exhausted operation removed its fence", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := Prepare(ctx, fixture.Paths, physicalwork.NewCaptureBudget(nil)); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled discovery admitted work", err)
	}
}
