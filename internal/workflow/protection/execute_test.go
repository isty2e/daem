//go:build darwin || linux

package protection

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	journalrecovery "github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/test/ownershiptest"
)

type failedClearStore struct {
	store       ownershipmutation.ProtectionRegistryStore
	visible     bool
	readFailure bool
	cause       error
}

func (store failedClearStore) LoadPendingProtections(ctx context.Context) ([]ownership.PendingProtection, error) {
	if store.readFailure {
		return nil, store.cause
	}
	return store.store.LoadPendingProtections(ctx)
}

func (store failedClearStore) TransitionProtection(ctx context.Context, owner stateauthority.Authority, transition ownership.ProtectionTransition) (ownership.Registry, error) {
	if store.visible {
		if _, err := store.store.TransitionProtection(ctx, owner, transition); err != nil {
			return ownership.Registry{}, err
		}
	}
	return ownership.Registry{}, store.cause
}

func TestClearFailureRequiresFreshRetainedAuthorityAndNeverInventsSettlement(t *testing.T) {
	for _, test := range []struct {
		name                 string
		visible, readFailure bool
		want                 Settlement
	}{
		{name: "retained", want: SettlementPending},
		{name: "visible-error", visible: true, want: SettlementUnknown},
		{name: "unreadable", readFailure: true, want: SettlementUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml")
			cause := errors.New("publication was not acknowledged")
			state, err := settleProtection(t.Context(), failedClearStore{store: fixture.Store, visible: test.visible, readFailure: test.readFailure, cause: cause}, fixture.Owner, fixture.Protection)
			if state != test.want || !errors.Is(err, cause) {
				t.Fatalf("settlement=%s err=%v", state, err)
			}
			result := Result{records: []Record{{settlement: state, cause: err}}}
			if result.Complete() {
				t.Fatal("unacknowledged publication became successful recovery")
			}
			if state == SettlementUnknown && result.Phase() != ResultUnknown {
				t.Fatal("projection lost indeterminate authority")
			}
		})
	}
}

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
	if _, _, err := Prepare(ctx, fixture.Paths, journalrecovery.NewPhysicalCaptureBudget(nil)); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled discovery admitted work", err)
	}
}
