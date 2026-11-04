//go:build darwin || linux

package protection

import (
	"context"
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/test/ownershiptest"
)

func TestRefusedFirstProofRetainsAssessedExpansionWithoutSettling(t *testing.T) {
	fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml")
	prior := Unobserved(fixture.Protection)
	prior.targets[0].state, prior.targets[0].expanded = TargetCheck, true
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	record := NewContainment(nil, nil).Settle(ctx, ownershipstore.Store{}, fixture.Protection, prior, nil, nil, nil)
	if record.Settlement() != SettlementPending || record.Targets()[0].State() != TargetBlocked || !record.Targets()[0].Expanded() || !errors.Is(record.Cause(), context.Canceled) {
		t.Fatal("refused proof lost assessed facts or became completed protection")
	}
	if prior.Targets()[0].State() != TargetCheck {
		t.Fatal("settlement modified its earlier assessment")
	}
	rows, err := fixture.Store.LoadPendingProtections(t.Context())
	if err != nil || len(rows) != 1 || !rows[0].Equal(fixture.Protection) {
		t.Fatal("refused proof changed its durable fence", err)
	}
}

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
