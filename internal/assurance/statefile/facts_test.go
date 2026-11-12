package statefile

import (
	"context"
	"errors"
	"testing"
)

func TestDecodePersistedFactsKeepsRetirementSeparateFromSnapshotCodec(t *testing.T) {
	content := []byte(`{"version":7,"managed_paths":[],"managed_aggregate_contributions":[],"pending_carrier_installs":[],"pending_carrier_removals":[],"managed_carrier_claims":[],"delegate_attempts":[],"host_route_attempts":[]}`)
	facts, err := DecodePersistedFacts(t.Context(), content)
	if err != nil || len(facts.ManagedPaths()) != 0 || facts.NativeDocumentCommit() != 0 {
		t.Fatal("exact empty retirement lost its persisted-load meaning", err)
	}
	if _, err := Decode(content); err == nil {
		t.Fatal("persisted facts changed the frozen Snapshot codec")
	}
	if _, err := DecodePersistedFacts(t.Context(), []byte(`{"version":7,"managed_paths":[{}]}`)); err == nil {
		t.Fatal("populated retired authority became empty facts")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := DecodePersistedFacts(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost precedence", err)
	}
}
