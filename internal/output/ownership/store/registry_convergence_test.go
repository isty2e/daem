package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/output/ownership"
)

func TestStoreConvergesClaimsAndBindingsWithoutLosingForeignRecords(t *testing.T) {
	root := canonicalTestRoot(t)
	store := mustStore(t, filepath.Join(root, "claims.json"))
	retiring := testActiveClaim(t, root, "owner", filepath.Join(root, "old.json"), "")
	added := testActiveClaim(t, root, "owner", filepath.Join(root, "new.json"), "")
	foreign := testActiveClaim(t, root, "foreign", filepath.Join(root, "foreign.json"), "")
	retiringBinding := storedDocumentBinding(t, retiring, "@pi-agent/settings.json")
	alias := storedDocumentBinding(t, retiring, ".pi/settings.json")
	newBinding := storedDocumentBinding(t, added, "@codex-home/config.toml")
	foreignBinding := storedDocumentBinding(t, foreign, "@pi-agent/settings.json")
	initial := seedConvergenceRegistry(t, store, []ownership.Claim{retiring, foreign}, retiringBinding, alias, foreignBinding)

	claims, err := ownership.NewClaimConvergence([]ownership.ClaimChange{
		mustStoreClaimChange(t, retiring.Address(), storeClaimValue(t, retiring), ownership.NoClaim()),
		mustStoreClaimChange(t, added.Address(), ownership.NoClaim(), storeClaimValue(t, added)),
	})
	if err != nil {
		t.Fatal(err)
	}
	bindings := mustStoreBindingConvergence(
		t,
		storeBindingIntent(t, retiringBinding, true, false),
		storeBindingIntent(t, alias, false, true),
		storeBindingIntent(t, newBinding, false, true),
	)
	want, err := ownership.NewRegistry([]ownership.Claim{added, foreign}, alias, newBinding, foreignBinding)
	if err != nil {
		t.Fatal(err)
	}

	got, err := store.ConvergeRegistry(t.Context(), claims, bindings)
	if err != nil || !got.Equal(want) {
		t.Fatalf("joint registry successor differs: %v", err)
	}
	loaded, err := store.Load(t.Context())
	if err != nil || !loaded.Equal(want) {
		t.Fatalf("persisted joint registry differs: %v", err)
	}
	if initial.Equal(got) {
		t.Fatal("joint convergence did not change either family")
	}

	before, err := os.Stat(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if got, err := store.ConvergeRegistry(t.Context(), claims, bindings); err != nil || !got.Equal(want) {
		t.Fatalf("partially finalized retry is not idempotent: %v", err)
	}
	after, err := os.Stat(store.Path())
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("idempotent joint convergence replaced the registry: %v", err)
	}
}

func TestStoreRefusesEitherStaleFamilyWithoutPublishingTheOther(t *testing.T) {
	root := canonicalTestRoot(t)
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "config.json"), "")
	otherOwner := testActiveClaim(t, root, "other", filepath.Join(root, "config.json"), "")
	otherPath := testActiveClaim(t, root, "owner", filepath.Join(root, "different.json"), "")
	binding := storedDocumentBinding(t, claim, "@pi-agent/settings.json")
	staleBinding := storedDocumentBinding(t, otherPath, "@pi-agent/settings.json")
	claims, err := ownership.NewClaimConvergence([]ownership.ClaimChange{
		mustStoreClaimChange(t, claim.Address(), storeClaimValue(t, claim), ownership.NoClaim()),
	})
	if err != nil {
		t.Fatal(err)
	}
	bindings := mustStoreBindingConvergence(t, storeBindingIntent(t, binding, true, false))

	for _, test := range []struct {
		name    string
		claim   ownership.Claim
		binding ownership.DocumentBinding
	}{
		{name: "stale claim", claim: otherOwner, binding: binding},
		{name: "stale binding", claim: claim, binding: staleBinding},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := mustStore(t, filepath.Join(t.TempDir(), "claims.json"))
			initial := seedConvergenceRegistry(t, store, []ownership.Claim{test.claim}, test.binding)
			before, err := os.ReadFile(store.Path())
			if err != nil {
				t.Fatal(err)
			}
			identity, err := os.Stat(store.Path())
			if err != nil {
				t.Fatal(err)
			}

			result, err := store.ConvergeRegistry(t.Context(), claims, bindings)
			if err == nil {
				t.Fatal("stale family was accepted")
			}
			assertUnusableRegistry(t, result)
			after, err := os.ReadFile(store.Path())
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("refusal changed durable registry bytes: %v", err)
			}
			afterIdentity, err := os.Stat(store.Path())
			if err != nil || !os.SameFile(identity, afterIdentity) {
				t.Fatalf("refusal replaced the durable registry: %v", err)
			}
			loaded, err := store.Load(t.Context())
			if err != nil || !loaded.Equal(initial) {
				t.Fatalf("refusal published a partial successor: %v", err)
			}
		})
	}
}

func TestPendingProtectionAllowsOnlyExactAtTargetConvergenceWithoutStoreReplacement(t *testing.T) {
	root := canonicalTestRoot(t)
	store := mustStore(t, filepath.Join(root, "claims.json"))
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "settings.json"), "/model")
	binding := storedDocumentBinding(t, claim, "@pi-agent/settings.json")
	pending := storedPendingProtection(t, binding)
	initial, err := ownership.NewRegistryWithProtections([]ownership.Claim{claim}, []ownership.DocumentBinding{binding}, []ownership.PendingProtection{pending})
	if err != nil {
		t.Fatal(err)
	}
	content, err := Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), content, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	row := storeClaimValue(t, claim)
	independent, err := ownership.NewClaimConvergence([]ownership.ClaimChange{mustStoreClaimChange(t, claim.Address(), row, row)})
	if err != nil {
		t.Fatal(err)
	}
	document, err := ownership.NewDocumentClaimChange([]ownership.Claim{claim}, []ownership.Claim{claim})
	if err != nil {
		t.Fatal(err)
	}
	group, _ := ownership.NewClaimConvergence(nil)
	group, err = group.WithDocumentChanges([]ownership.DocumentClaimChange{document})
	if err != nil {
		t.Fatal(err)
	}
	bindings := mustStoreBindingConvergence(t, storeBindingIntent(t, binding, true, true))
	for _, convergence := range []ownership.ClaimConvergence{independent, group} {
		got, err := store.ConvergeRegistry(t.Context(), convergence, bindings)
		if err != nil || !got.Equal(initial) {
			t.Fatal("read-only at-target convergence lost complete facts", err)
		}
		after, err := os.Stat(store.Path())
		if err != nil || !os.SameFile(before, after) || !bytes.Equal(content, mustReadRegistryBytes(t, store.Path())) {
			t.Fatal("at-target convergence replaced the protected Registry", err)
		}
	}
	remove, _ := ownership.NewClaimConvergence([]ownership.ClaimChange{mustStoreClaimChange(t, claim.Address(), row, ownership.NoClaim())})
	var blocked *ownership.ProtectionPendingError
	if _, err := store.ConvergeRegistry(t.Context(), remove, bindings); !errors.As(err, &blocked) {
		t.Fatalf("actual mutation acquired an at-target exemption: %v", err)
	}
	if !bytes.Equal(content, mustReadRegistryBytes(t, store.Path())) {
		t.Fatal("refused mutation changed the Registry")
	}
}

func mustReadRegistryBytes(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func TestStoreCreatesBindingOnlyRegistryAndRetiresRemovedClaimTogether(t *testing.T) {
	root := canonicalTestRoot(t)
	store := mustStore(t, filepath.Join(root, "data", "claims.json"))
	host := filepath.Join(root, "config-\u00e9.json")
	if err := os.WriteFile(host, []byte("{}"), 0o640); err != nil {
		t.Fatal(err)
	}
	claim := testActiveClaim(t, root, "owner", host, "")
	binding := storedDocumentBinding(t, claim, "@pi-agent/settings.json")
	emptyClaims, err := ownership.NewClaimConvergence(nil)
	if err != nil {
		t.Fatal(err)
	}
	acquire := mustStoreBindingConvergence(t, storeBindingIntent(t, binding, false, true))
	result, err := store.ConvergeRegistry(t.Context(), emptyClaims, acquire)
	want, wantErr := ownership.NewRegistry(nil, binding)
	if err != nil || wantErr != nil || !result.Equal(want) {
		t.Fatalf("binding-only registry creation differs: %v, %v", err, wantErr)
	}
	info, err := os.Stat(store.Path())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("new registry permissions are not private: %v", err)
	}
	if _, err := store.Apply(t.Context(), claim.Address(), ownership.NoClaim(), storeClaimValue(t, claim)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(host); err != nil {
		t.Fatal(err)
	}
	retireClaims, err := ownership.NewClaimConvergence([]ownership.ClaimChange{
		mustStoreClaimChange(t, claim.Address(), storeClaimValue(t, claim), ownership.NoClaim()),
	})
	if err != nil {
		t.Fatal(err)
	}
	retireBindings := mustStoreBindingConvergence(t, storeBindingIntent(t, binding, true, false))
	result, err = store.ConvergeRegistry(t.Context(), retireClaims, retireBindings)
	if err != nil || !result.Equal(ownership.EmptyRegistry()) {
		t.Fatalf("joint retirement did not admit the exact removed claim: %v", err)
	}
	loaded, err := store.Load(t.Context())
	if err != nil || !loaded.Equal(result) {
		t.Fatalf("joint retirement was not persisted: %v", err)
	}
}

func TestStoreJointNoOpRetainsLegacyBytesAndFileIdentity(t *testing.T) {
	store := mustStore(t, filepath.Join(canonicalTestRoot(t), "claims.json"))
	legacy, err := json.Marshal(registryV3File{Version: 3, Claims: []claimRecord{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	claims, err := ownership.NewClaimConvergence(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := store.ConvergeRegistry(t.Context(), claims, mustStoreBindingConvergence(t)); err != nil || !got.Equal(ownership.EmptyRegistry()) {
		t.Fatalf("legacy joint no-op failed: %v", err)
	}
	content, err := os.ReadFile(store.Path())
	if err != nil || !bytes.Equal(content, legacy) {
		t.Fatalf("joint no-op upgraded legacy bytes: %v", err)
	}
	after, err := os.Stat(store.Path())
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("joint no-op replaced legacy metadata: %v", err)
	}
}

func TestStoreJointConvergenceRefusesInvalidOrCanceledInputWithoutCreatingMetadata(t *testing.T) {
	claims, err := ownership.NewClaimConvergence(nil)
	if err != nil {
		t.Fatal(err)
	}
	bindings := mustStoreBindingConvergence(t)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	for _, test := range []struct {
		name     string
		ctx      context.Context
		claims   ownership.ClaimConvergence
		bindings ownership.DocumentBindingConvergence
	}{
		{name: "zero claims", ctx: t.Context(), bindings: bindings},
		{name: "zero bindings", ctx: t.Context(), claims: claims},
		{name: "canceled", ctx: canceled, claims: claims, bindings: bindings},
		{name: "nil context", claims: claims, bindings: bindings},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := mustStore(t, filepath.Join(t.TempDir(), "claims.json"))
			result, err := store.ConvergeRegistry(test.ctx, test.claims, test.bindings)
			if err == nil || (test.name == "canceled" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("invalid/canceled convergence was not refused: %v", err)
			}
			assertUnusableRegistry(t, result)
			if _, err := os.Lstat(store.Path()); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("refused convergence created metadata: %v", err)
			}
		})
	}
}

func seedConvergenceRegistry(t *testing.T, store Store, claims []ownership.Claim, bindings ...ownership.DocumentBinding) ownership.Registry {
	t.Helper()
	registry, err := ownership.NewRegistry(claims, bindings...)
	if err != nil {
		t.Fatal(err)
	}
	content, err := Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), content, 0o600); err != nil {
		t.Fatal(err)
	}
	return registry
}

func storeClaimValue(t *testing.T, claim ownership.Claim) ownership.ClaimValue {
	t.Helper()
	value, err := ownership.PresentClaim(claim)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func storeBindingIntent(t *testing.T, binding ownership.DocumentBinding, hadBefore bool, hasAfter bool) ownership.DocumentBindingIntent {
	t.Helper()
	value, err := ownership.PresentDocumentBinding(binding)
	if err != nil {
		t.Fatal(err)
	}
	before, after := ownership.NoDocumentBinding(), ownership.NoDocumentBinding()
	if hadBefore {
		before = value
	}
	if hasAfter {
		after = value
	}
	intent, err := ownership.NewExactDocumentBindingIntent(binding.Owner(), binding.Destination(), before, after)
	if err != nil {
		t.Fatal(err)
	}
	return intent
}

func mustStoreBindingConvergence(t *testing.T, intents ...ownership.DocumentBindingIntent) ownership.DocumentBindingConvergence {
	t.Helper()
	convergence, err := ownership.NewDocumentBindingConvergence(intents)
	if err != nil {
		t.Fatal(err)
	}
	return convergence
}

func assertUnusableRegistry(t *testing.T, registry ownership.Registry) {
	t.Helper()
	if _, _, err := mustStoreBindingConvergence(t).Apply(registry); err == nil {
		t.Fatal("refusal returned a usable partial registry")
	}
}
