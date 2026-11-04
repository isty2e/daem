package nativefixture

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
)

func RequireLifecycleMetadata(t *testing.T, manifest string, managed bool) {
	t.Helper()
	RequireLifecycleMetadataForScope(t, manifest, managed, target.ScopeProject)
}

func RequireLifecycleMetadataForScope(t *testing.T, manifest string, managed bool, scope target.Scope) {
	t.Helper()
	paths, err := daempaths.Resolve(manifest)
	if err != nil {
		t.Fatal(err)
	}
	codecs, err := nativeconfig.Codecs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	codec, err := statefile.NewNativeCodec(codecs.Contributions())
	if err != nil {
		t.Fatal(err)
	}

	budget := physicalwork.NewCaptureBudget(nil)
	state, err := statefile.LoadOptionalWithOptions(t.Context(), paths.StatefilePath, statefile.ReadOptions{Codec: codec, PhysicalPathBudget: budget})
	if err != nil || state.ContainsHostConfig() != managed {
		t.Fatal("persisted native ownership does not match the applied lifecycle", err)
	}
	reader, err := ownershipstore.NewRecoveryReader(paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := reader.LoadForClaimRemovals(t.Context(), nil, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}

	wantBindings, wantClaims := 0, 0
	if managed {
		wantBindings = 1
		if scope == target.ScopeGlobal {
			wantClaims = len(state.ManagedAggregates())
		}
	}
	if len(registry.DocumentBindings()) != wantBindings || len(registry.Claims()) != wantClaims {
		t.Fatalf("native %s lifecycle metadata: bindings=%d want=%d claims=%d want=%d", scope, len(registry.DocumentBindings()), wantBindings, len(registry.Claims()), wantClaims)
	}
	for _, claim := range registry.Claims() {
		if claim.State() != ownership.ClaimActive || claim.OperationID() != "" {
			t.Fatal("settled global claim retains reservation debt")
		}
	}
}
