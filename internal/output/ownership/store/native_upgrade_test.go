package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestStoreV2ReadAndNativeWritePreserveExistingClaims(t *testing.T) {
	root := canonicalTestRoot(t)
	registryStore := mustStore(t, filepath.Join(root, "claims.json"))
	legacy := testActiveClaim(t, root, "owner", filepath.Join(root, "config.json"), "/a~1b")
	path := legacy.Address().PathAuthority()
	owner := legacy.Owner().StatefileAuthority()
	previous, err := json.Marshal(registryV2File{Version: 2, Claims: []registryV2ClaimRecord{{
		PathAuthority:      pathAuthorityRecord{Key: path.Key(), Witness: path.Witness()},
		ContentPath:        "/a~1b",
		StatefileAuthority: pathAuthorityRecord{Key: owner.Key(), Witness: owner.Witness()},
		ManifestPath:       legacy.Owner().ManifestPath(), State: string(ownership.ClaimActive),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registryStore.Path(), previous, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := registryStore.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if claim, found := loaded.Exact(legacy.Address()); !found || !claim.Equal(legacy) {
		t.Fatal("loading v2 changed literal ownership")
	}
	retained, err := os.ReadFile(registryStore.Path())
	if err != nil || string(retained) != string(previous) {
		t.Fatalf("read-only load rewrote v2 metadata: %v", err)
	}

	key, err := output.NewKeyPath("a/b")
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	address, err := ownership.NewManagedAddress(path, footprint)
	if err != nil {
		t.Fatal(err)
	}
	native, err := ownership.NewActiveClaim(address, legacy.Owner())
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := ownership.PresentClaim(native)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registryStore.Apply(t.Context(), address, ownership.NoClaim(), replacement); err != nil {
		t.Fatal(err)
	}
	upgraded, err := registryStore.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []ownership.Claim{legacy, native} {
		if actual, found := upgraded.Exact(expected.Address()); !found || !actual.Equal(expected) {
			t.Fatal("native write lost or reinterpreted an existing claim")
		}
	}
	content, err := os.ReadFile(registryStore.Path())
	if err != nil {
		t.Fatal(err)
	}
	var envelope file
	if err := json.Unmarshal(content, &envelope); err != nil || envelope.Version != currentVersion {
		t.Fatalf("state-changing write did not publish the current registry schema: %v", err)
	}
}
