package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/executionidentity"
	"github.com/isty2e/daem/internal/output/ownership"
)

func storedPendingProtection(t *testing.T, bindings ...ownership.DocumentBinding) ownership.PendingProtection {
	t.Helper()
	document, err := ownership.NewProtectedDocument(bindings, 0, 0, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := executionidentity.NewInvocation(strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	correlation, err := ownership.NewProtectionCorrelation("operation-1", strings.Repeat("b", 64), strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	protection, err := ownership.NewArmedProtection(bindings[0].Owner(), invocation, correlation, []ownership.ProtectedDocument{document})
	if err != nil {
		t.Fatal(err)
	}
	return protection
}

func storedProtectionExecution(t *testing.T, protection ownership.PendingProtection) executionidentity.Binding {
	t.Helper()
	context, err := executionidentity.NewContext(executionidentity.Linux, strings.Repeat("d", 64), "12345678-1234-1234-1234-123456789abc")
	if err != nil {
		t.Fatal(err)
	}
	execution, err := executionidentity.NewBinding(protection.Invocation(), context, 123)
	if err != nil {
		t.Fatal(err)
	}
	return execution
}

func assertProtectionNoOp(t *testing.T, store Store, owner stateauthority.Authority, transition ownership.ProtectionTransition, want ownership.Registry) {
	t.Helper()
	before, err := os.Stat(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	beforeBytes, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.TransitionProtection(t.Context(), owner, transition)
	if err != nil || !got.Equal(want) {
		t.Fatalf("idempotent protection transition changed facts: %v", err)
	}
	after, err := os.Stat(store.Path())
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("idempotent protection transition replaced the registry: %v", err)
	}
	afterBytes, err := os.ReadFile(store.Path())
	if err != nil || !bytes.Equal(beforeBytes, afterBytes) {
		t.Fatalf("idempotent protection transition changed bytes: %v", err)
	}
}

func TestProtectionWireRoundTripAndEmptyFamilyByteParity(t *testing.T) {
	root := canonicalTestRoot(t)
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "settings.json"), "/model")
	binding := storedDocumentBinding(t, claim, "@pi-agent/settings.json")
	ordinary, err := ownership.NewRegistry([]ownership.Claim{claim}, binding)
	if err != nil {
		t.Fatal(err)
	}
	content, err := Marshal(ordinary)
	if err != nil {
		t.Fatal(err)
	}
	var current file
	if err := json.Unmarshal(content, &current); err != nil {
		t.Fatal(err)
	}
	legacyShape := struct {
		Version          int                     `json:"version"`
		Claims           []claimRecord           `json:"claims"`
		DocumentBindings []documentBindingRecord `json:"document_bindings"`
	}{current.Version, current.Claims, current.DocumentBindings}
	legacyBytes, err := json.MarshalIndent(legacyShape, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, append(legacyBytes, '\n')) {
		t.Fatal("empty protection family changed existing Registry4 bytes")
	}
	armed := storedPendingProtection(t, binding)
	bound, err := armed.Bind(storedProtectionExecution(t, armed))
	if err != nil {
		t.Fatal(err)
	}
	zeroMode, err := ownership.NewProtectedDocument([]ownership.DocumentBinding{binding}, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	explicitZero, err := ownership.NewArmedProtection(armed.Owner(), armed.Invocation(), armed.Correlation(), []ownership.ProtectedDocument{zeroMode})
	if err != nil {
		t.Fatal(err)
	}
	for _, protection := range []ownership.PendingProtection{armed, bound, explicitZero} {
		for _, bindings := range [][]ownership.DocumentBinding{nil, {binding}} {
			registry, err := ownership.NewRegistryWithProtections(nil, bindings, []ownership.PendingProtection{protection})
			if err != nil {
				t.Fatal(err)
			}
			content, err := Marshal(registry)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := decode(content)
			if err != nil || !decoded.Equal(registry) || len(decoded.PendingProtections()) != 1 {
				t.Fatalf("protection-only or bound record lost authority: %v", err)
			}
			if decoded.PendingProtections()[0].Documents()[0].UserID() != 0 || decoded.PendingProtections()[0].Documents()[0].GroupID() != 0 {
				t.Fatal("valid numeric zero owner/group was lost")
			}
		}
	}
}

func TestProtectionDecoderRejectsMalformedPhasesScopeAndUnknownFields(t *testing.T) {
	root := canonicalTestRoot(t)
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "config.json"), "")
	binding := storedDocumentBinding(t, claim, "@pi-agent/settings.json")
	protection := storedPendingProtection(t, binding)
	registry, err := ownership.NewRegistryWithProtections(nil, []ownership.DocumentBinding{binding}, []ownership.PendingProtection{protection})
	if err != nil {
		t.Fatal(err)
	}
	valid, err := Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(map[string]any, map[string]any, map[string]any)
	}{
		{"null family", func(top, row, document map[string]any) { top["pending_protections"] = nil }},
		{"null record", func(top, row, document map[string]any) { top["pending_protections"] = []any{nil} }},
		{"unknown record field", func(top, row, document map[string]any) { row["allow_write"] = true }},
		{"unknown target field", func(top, row, document map[string]any) { document["restore_bytes"] = "secret" }},
		{"missing uid", func(top, row, document map[string]any) { delete(document, "user_id") }},
		{"null gid", func(top, row, document map[string]any) { document["group_id"] = nil }},
		{"absent mode", func(top, row, document map[string]any) { delete(document, "mode_cap") }},
		{"special mode", func(top, row, document map[string]any) { document["mode_cap"] = 0o4640 }},
		{"empty cohort", func(top, row, document map[string]any) { document["bindings"] = []any{} }},
		{"duplicate target", func(top, row, document map[string]any) { row["documents"] = []any{document, document} }},
		{"duplicate invocation", func(top, row, document map[string]any) { top["pending_protections"] = []any{row, row} }},
		{"missing fingerprint", func(top, row, document map[string]any) { delete(row, "selection_fingerprint") }},
		{"bound without identity", func(top, row, document map[string]any) { row["phase"] = "bound" }},
		{"armed with identity", func(top, row, document map[string]any) { row["execution"] = map[string]any{} }},
		{"null execution", func(top, row, document map[string]any) { row["execution"] = nil }},
		{"unsupported phase", func(top, row, document map[string]any) { row["phase"] = "completed" }},
		{"future version", func(top, row, document map[string]any) { top["version"] = currentVersion + 1 }},
		{"empty invocation", func(top, row, document map[string]any) { row["invocation_id"] = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			var top map[string]any
			if err := json.Unmarshal(valid, &top); err != nil {
				t.Fatal(err)
			}
			row := top["pending_protections"].([]any)[0].(map[string]any)
			document := row["documents"].([]any)[0].(map[string]any)
			test.mutate(top, row, document)
			content, err := json.Marshal(top)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := decode(content)
			if err == nil {
				t.Fatal("malformed protection produced a usable registry")
			}
			assertUnusableRegistry(t, decoded)
		})
	}
}

func TestProtectionExecutionVisibilityRoundTripPreservesLegacyUncertainty(t *testing.T) {
	root := canonicalTestRoot(t)
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "settings.json"), "/model")
	binding := storedDocumentBinding(t, claim, "@pi-agent/settings.json")
	armed := storedPendingProtection(t, binding)
	legacy := storedProtectionExecution(t, armed)
	context, err := executionidentity.NewContextWithVisibility(legacy.Context().Platform(), legacy.Context().Realm(), legacy.Context().Epoch(), strings.Repeat("e", 64))
	if err != nil {
		t.Fatal(err)
	}
	qualified, err := executionidentity.NewBinding(armed.Invocation(), context, legacy.Group())
	if err != nil {
		t.Fatal(err)
	}
	for _, execution := range []executionidentity.Binding{legacy, qualified} {
		bound, err := armed.Bind(execution)
		if err != nil {
			t.Fatal(err)
		}
		registry, err := ownership.NewRegistryWithProtections([]ownership.Claim{claim}, []ownership.DocumentBinding{binding}, []ownership.PendingProtection{bound})
		if err != nil {
			t.Fatal(err)
		}
		content, err := Marshal(registry)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decode(content)
		if err != nil || !decoded.Equal(registry) {
			t.Fatal("execution visibility was lost or invented by persistence", err)
		}
		stored, present := decoded.PendingProtections()[0].Execution()
		if !present || stored.Context().HasVisibility() != execution.Context().HasVisibility() || stored.Context() != execution.Context() {
			t.Fatal("pending execution context changed its qualification facts")
		}
		reencoded, err := Marshal(decoded)
		if err != nil || !bytes.Equal(content, reencoded) {
			t.Fatal("unpublished context round-trip changed its persisted representation", err)
		}
		if !execution.Context().HasVisibility() && stored.Context().Relation(context) != executionidentity.Incomparable {
			t.Fatal("legacy execution acquired a current namespace")
		}
		if execution.Context().HasVisibility() {
			for _, invalid := range []any{nil, false, 12, strings.Repeat("0", 64), strings.Repeat("E", 64), "invalid"} {
				var top map[string]any
				if err := json.Unmarshal(content, &top); err != nil {
					t.Fatal(err)
				}
				row := top["pending_protections"].([]any)[0].(map[string]any)
				row["execution"].(map[string]any)["visibility"] = invalid
				malformed, err := json.Marshal(top)
				if err != nil {
					t.Fatal(err)
				}
				unusable, err := decode(malformed)
				if err == nil {
					t.Fatal("malformed visibility produced a usable registry")
				}
				assertUnusableRegistry(t, unusable)
			}
		}
	}
}

func TestStoreProtectionTransitionsPreserveDisjointWritersAndRefuseScopeRelease(t *testing.T) {
	root := canonicalTestRoot(t)
	store := mustStore(t, filepath.Join(root, "claims.json"))
	protected := testActiveClaim(t, root, "owner", filepath.Join(root, "config.json"), "/model")
	binding := storedDocumentBinding(t, protected, "@pi-agent/settings.json")
	seedConvergenceRegistry(t, store, []ownership.Claim{protected}, binding)
	protection := storedPendingProtection(t, binding)
	arm, _ := ownership.NewArmProtection(protection)
	armed, err := store.TransitionProtection(t.Context(), protected.Owner(), arm)
	if err != nil {
		t.Fatal(err)
	}
	assertProtectionNoOp(t, store, protected.Owner(), arm, armed)
	foreign := testActiveClaim(t, root, "foreign", filepath.Join(root, "disjoint.json"), "")
	foreignBinding := storedDocumentBinding(t, foreign, "@codex-home/config.toml")
	change := mustStoreClaimChange(t, foreign.Address(), ownership.NoClaim(), storeClaimValue(t, foreign))
	claims, err := ownership.NewClaimConvergence([]ownership.ClaimChange{change})
	if err != nil {
		t.Fatal(err)
	}
	bindings := mustStoreBindingConvergence(t, storeBindingIntent(t, foreignBinding, false, true))
	if _, err := store.ConvergeRegistry(t.Context(), claims, bindings); err != nil {
		t.Fatalf("foreign disjoint work was blocked: %v", err)
	}
	rows, err := store.LoadPendingProtections(t.Context())
	if err != nil || len(rows) != 1 || !rows[0].Equal(protection) {
		t.Fatalf("disjoint write lost protection: %v", err)
	}
	before, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RemoveClaim(t.Context(), protected); err == nil {
		t.Fatal("claim release bypassed pending scope")
	}
	retireClaims, err := ownership.NewClaimConvergence([]ownership.ClaimChange{
		mustStoreClaimChange(t, protected.Address(), storeClaimValue(t, protected), ownership.NoClaim()),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConvergeRegistry(t.Context(), retireClaims, mustStoreBindingConvergence(t, storeBindingIntent(t, binding, true, false))); err == nil {
		t.Fatal("joint claim/binding retirement orphaned protection")
	}
	after, err := os.ReadFile(store.Path())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("refused release changed registry: %v", err)
	}
	bind, _ := ownership.NewBindProtection(protection, storedProtectionExecution(t, protection))
	bound, err := store.TransitionProtection(t.Context(), protected.Owner(), bind)
	if err != nil {
		t.Fatal(err)
	}
	assertProtectionNoOp(t, store, protected.Owner(), bind, bound)
	actual, _ := bound.Protection(protection.Invocation())
	clear, _ := ownership.NewClearProtection(actual)
	cleared, err := store.TransitionProtection(t.Context(), protected.Owner(), clear)
	if err != nil || len(cleared.PendingProtections()) != 0 {
		t.Fatalf("qualified clear failed: %v", err)
	}
	if _, found := cleared.Exact(foreign.Address()); !found {
		t.Fatal("protection settlement dropped foreign work")
	}
	assertProtectionNoOp(t, store, protected.Owner(), clear, cleared)
}

func TestProtectionDiscoveryAndSettlementDoNotDependOnUnrelatedLiveClaims(t *testing.T) {
	root := canonicalTestRoot(t)
	store := mustStore(t, filepath.Join(root, "claims.json"))
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "config.json"), "/model")
	binding := storedDocumentBinding(t, claim, "@pi-agent/settings.json")
	protection := storedPendingProtection(t, binding)
	foreign := testActiveClaim(t, root, "foreign", filepath.Join(root, "unrelated.json"), "")
	path := foreign.Address().PathAuthority()
	stalePath, err := pathauthority.NewExact(path.Key(), alternateSemanticsWitness(path.Key(), path.Witness()))
	if err != nil {
		t.Fatal(err)
	}
	staleAddress, err := ownership.NewManagedAddress(stalePath, foreign.Address().Footprint())
	if err != nil {
		t.Fatal(err)
	}
	staleClaim, err := ownership.NewActiveClaim(staleAddress, foreign.Owner())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := ownership.NewRegistryWithProtections([]ownership.Claim{staleClaim}, nil, []ownership.PendingProtection{protection})
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
	if _, err := store.Load(t.Context()); err == nil {
		t.Fatal("ordinary registry read accepted stale referenced authority")
	}
	rows, err := store.LoadPendingProtections(t.Context())
	if err != nil || len(rows) != 1 || !rows[0].Equal(protection) {
		t.Fatalf("unrelated authority drift hid safety work: %v", err)
	}
	budget := &documentSettlementCountingBudget{}
	rows, err = ReadPendingProtectionsBounded(t.Context(), store.Path(), physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil || len(rows) != 1 || !rows[0].Equal(protection) || budget.calls == 0 {
		t.Fatal("bounded artifact-only read qualified unrelated paths or bypassed traversal admission", err)
	}
	clear, _ := ownership.NewClearProtection(protection)
	settled, err := store.TransitionProtection(t.Context(), protection.Owner(), clear)
	if err != nil || len(settled.PendingProtections()) != 0 {
		t.Fatalf("metadata settlement depended on unrelated paths: %v", err)
	}
	if actual, present := settled.Exact(staleClaim.Address()); !present || !actual.Equal(staleClaim) {
		t.Fatal("protection settlement repaired or removed unrelated authority")
	}
}

func TestProtectionStoreRefusesInvalidCanceledForeignAndControlTargets(t *testing.T) {
	root := canonicalTestRoot(t)
	store := mustStore(t, filepath.Join(root, "claims.json"))
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "config.json"), "/model")
	binding := storedDocumentBinding(t, claim, "@pi-agent/settings.json")
	initial := seedConvergenceRegistry(t, store, nil, binding)
	protection := storedPendingProtection(t, binding)
	arm, _ := ownership.NewArmProtection(protection)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	for _, ctx := range []context.Context{nil, canceled} {
		if _, err := store.TransitionProtection(ctx, claim.Owner(), arm); err == nil {
			t.Fatal("nil/canceled protection transition was admitted")
		}
		if _, err := store.LoadPendingProtections(ctx); err == nil {
			t.Fatal("nil/canceled protection read was admitted")
		}
	}
	foreign := testActiveClaim(t, root, "foreign", filepath.Join(root, "other.json"), "")
	if _, err := store.TransitionProtection(t.Context(), foreign.Owner(), arm); err == nil {
		t.Fatal("foreign owner armed a protection")
	}
	loaded, err := store.Load(t.Context())
	if err != nil || !loaded.Equal(initial) {
		t.Fatalf("rejected transition changed metadata: %v", err)
	}
	control := testActiveClaim(t, root, "owner", store.Path(), "")
	controlBinding := storedDocumentBinding(t, control, "@pi-agent/settings.json")
	seedConvergenceRegistry(t, store, nil, controlBinding)
	unsafe, _ := ownership.NewArmProtection(storedPendingProtection(t, controlBinding))
	if _, err := store.TransitionProtection(t.Context(), control.Owner(), unsafe); err == nil {
		t.Fatalf("registry became its own chmod target: store=%q, physical=%q", store.Path(), controlBinding.PathAuthority().Key())
	}
	if err := os.Chmod(store.Path(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadPendingProtections(t.Context()); err == nil {
		t.Fatal("public registry supplied safety authority")
	}
	missing := mustStore(t, filepath.Join(root, "missing.json"))
	if rows, err := missing.LoadPendingProtections(t.Context()); err != nil || len(rows) != 0 {
		t.Fatalf("missing registry fabricated safety work: %v", err)
	}
	if _, err := os.Stat(missing.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("safety discovery created a registry")
	}
}
