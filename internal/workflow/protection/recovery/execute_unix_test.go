//go:build darwin || linux

package recovery

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/test/ownershiptest"
)

type stoppedBudget struct{ stopped bool }

func (budget *stoppedBudget) AdmitPathComponents(int) error {
	if budget.stopped {
		return errors.New("physical work exhausted")
	}
	return nil
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
