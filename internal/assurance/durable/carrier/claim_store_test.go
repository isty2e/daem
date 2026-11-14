package carrier_test

import (
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/target"
)

func TestClaimStoreFollowsCarrierScope(t *testing.T) {
	for _, scenario := range []struct {
		scope target.Scope
		store durablecarrier.ClaimStore
	}{
		{scope: target.ScopeProject, store: durablecarrier.ClaimStoreProjectStatefile},
		{scope: target.ScopeGlobal, store: durablecarrier.ClaimStoreGlobalRegistry},
	} {
		t.Run(string(scenario.scope), func(t *testing.T) {
			store, known := durablecarrier.ClaimStoreForScope(scenario.scope)
			if !known || store != scenario.store {
				t.Fatalf("scope selection = (%q, %t), want %q", store, known, scenario.store)
			}

			fixture := carrierFixtureFor(t, "alpha", "alpha@official", scenario.scope)
			if actual := fixture.identity.ClaimStore(); actual != scenario.store {
				t.Fatalf("identity store = %q, want %q", actual, scenario.store)
			}
		})
	}
}

func TestClaimStoreDoesNotDefaultUnknownScopeOrZeroIdentity(t *testing.T) {
	for _, scope := range []target.Scope{"", "user"} {
		if store, known := durablecarrier.ClaimStoreForScope(scope); known || store != "" {
			t.Fatalf("scope %q selection = (%q, %t), want no store", scope, store, known)
		}
	}
	if store := (durablecarrier.ManagedCarrierIdentity{}).ClaimStore(); store != "" {
		t.Fatalf("zero identity store = %q, want no store", store)
	}
}
