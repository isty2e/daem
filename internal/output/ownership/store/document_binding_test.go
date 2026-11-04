package store

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

func storedDocumentBinding(t *testing.T, claim ownership.Claim, destination string) ownership.DocumentBinding {
	t.Helper()
	logical, err := output.Parse(destination)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := ownership.NewDocumentBinding(claim.Owner(), logical, claim.Address().PathAuthority())
	if err != nil {
		t.Fatal(err)
	}
	return binding
}

func TestRegistryV4RoundTripsBindingsAndClaimWritesPreserveThem(t *testing.T) {
	root := canonicalTestRoot(t)
	base := testActiveClaim(t, root, "owner", filepath.Join(root, "config.json"), "")
	global := storedDocumentBinding(t, base, "@pi-agent/settings.json")
	project := storedDocumentBinding(t, base, ".pi/settings.json")
	registry, err := ownership.NewRegistry(nil, project, global)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decode(encoded)
	if err != nil || !decoded.Equal(registry) {
		t.Fatalf("binding round trip changed identity: %v", err)
	}
	reordered, err := ownership.NewRegistry(nil, global, project)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Marshal(reordered)
	if err != nil || !bytes.Equal(encoded, second) {
		t.Fatalf("registry serialization depends on insertion order: %v", err)
	}

	store := mustStore(t, filepath.Join(root, "claims.json"))
	if err := os.WriteFile(store.Path(), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(t.Context())
	if err != nil || !loaded.Equal(registry) {
		t.Fatalf("store changed recorded identity evidence: %v", err)
	}
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "other.json"), "")
	present, _ := ownership.PresentClaim(claim)
	if _, err := store.Apply(t.Context(), claim.Address(), ownership.NoClaim(), present); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []ownership.DocumentBinding{global, project} {
		if actual, found := loaded.DocumentBinding(expected.Owner().Key(), expected.Destination()); !found || !actual.Equal(expected) {
			t.Fatal("ordinary claim write discarded a document binding")
		}
	}
	if actual, found := loaded.Exact(claim.Address()); !found || !actual.Equal(claim) {
		t.Fatal("binding preservation prevented the authorized claim transition")
	}
	change, err := ownership.NewClaimChange(claim.Address(), present, ownership.NoClaim())
	if err != nil {
		t.Fatal(err)
	}
	convergence, err := ownership.NewClaimConvergence([]ownership.ClaimChange{change})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Converge(t.Context(), convergence); err != nil {
		t.Fatalf("claim convergence failed: %v", err)
	}
	loaded, err = store.Load(t.Context())
	if err != nil || !loaded.Equal(registry) {
		t.Fatalf("persisted claim convergence changed document identity evidence: %v", err)
	}
}

func TestRegistryV3ReadAndNoOpDoNotRewriteBeforeV4Mutation(t *testing.T) {
	root := canonicalTestRoot(t)
	store := mustStore(t, filepath.Join(root, "claims.json"))
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "config.json"), "/a~1b")
	registry, err := ownership.NewRegistry([]ownership.Claim{claim})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	var current file
	if err := json.Unmarshal(encoded, &current); err != nil {
		t.Fatal(err)
	}
	legacy, err := json.Marshal(registryV3File{Version: 3, Claims: current.Claims})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(t.Context())
	if err != nil || !loaded.Equal(registry) || len(loaded.DocumentBindings()) != 0 {
		t.Fatalf("v3 admission changed claims or invented bindings: %v", err)
	}
	present, _ := ownership.PresentClaim(claim)
	if _, err := store.Apply(t.Context(), claim.Address(), present, present); err != nil {
		t.Fatal(err)
	}
	retained, err := os.ReadFile(store.Path())
	if err != nil || !bytes.Equal(retained, legacy) {
		t.Fatalf("read/no-op rewrote legacy metadata: %v", err)
	}
	other := testActiveClaim(t, root, "owner", filepath.Join(root, "other.json"), "")
	otherValue, _ := ownership.PresentClaim(other)
	if _, err := store.Apply(t.Context(), other.Address(), ownership.NoClaim(), otherValue); err != nil {
		t.Fatal(err)
	}
	upgraded, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []ownership.Claim{claim, other} {
		if actual, found := upgraded.Exact(expected.Address()); !found || !actual.Equal(expected) {
			t.Fatal("v4 publication reinterpreted an old claim")
		}
	}
	content, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, &current); err != nil || current.Version != currentVersion || current.DocumentBindings == nil {
		t.Fatalf("state-changing write did not publish complete v4 metadata: %v", err)
	}
}

func TestRegistryV4RefusesMissingMalformedAndDuplicateBindingFacts(t *testing.T) {
	claim := nativeClaim(t, "value")
	binding := storedDocumentBinding(t, claim, "@codex-home/config.toml")
	registry, err := ownership.NewRegistry([]ownership.Claim{claim}, binding)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	var valid file
	if err := json.Unmarshal(encoded, &valid); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*file){
		func(record *file) { record.DocumentBindings = nil },
		func(record *file) { record.DocumentBindings[0].Destination = "../config.toml" },
		func(record *file) { record.DocumentBindings[0].PathAuthority.Witness = "unknown-v99:" },
		func(record *file) { record.DocumentBindings[0].PathAuthority.Key = "" },
		func(record *file) { record.DocumentBindings[0].StatefileAuthority.Key = "" },
		func(record *file) { record.DocumentBindings[0].ManifestPath = "relative.toml" },
		func(record *file) {
			record.DocumentBindings = append(record.DocumentBindings, record.DocumentBindings[0])
		},
	} {
		candidate := valid
		candidate.DocumentBindings = append([]documentBindingRecord(nil), valid.DocumentBindings...)
		mutate(&candidate)
		content, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if result, err := decode(content); err == nil || len(result.DocumentBindings()) != 0 || len(result.Claims()) != 0 {
			t.Fatal("invalid binding data admitted or returned partial authority facts")
		}
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatal(err)
	}
	delete(envelope, "document_bindings")
	missing, _ := json.Marshal(envelope)
	if _, err := decode(missing); err == nil {
		t.Fatal("missing v4 binding family was accepted")
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &valid); err != nil {
		t.Fatal(err)
	}
	envelope["document_bindings"], _ = json.Marshal(valid.DocumentBindings)
	if err := json.Unmarshal(envelope["document_bindings"], &rows); err != nil {
		t.Fatal(err)
	}
	rows[0]["permission_to_write"] = json.RawMessage("true")
	envelope["document_bindings"], _ = json.Marshal(rows)
	unknown, _ := json.Marshal(envelope)
	if _, err := decode(unknown); err == nil {
		t.Fatal("binding record acquired an unrecognized authority field")
	}
	legacy, _ := json.Marshal(struct {
		Version          int                     `json:"version"`
		Claims           []claimRecord           `json:"claims"`
		DocumentBindings []documentBindingRecord `json:"document_bindings"`
	}{Version: 3, Claims: []claimRecord{}, DocumentBindings: valid.DocumentBindings})
	if _, err := decode(legacy); err == nil {
		t.Fatal("v3 silently accepted a new binding family")
	}
}
