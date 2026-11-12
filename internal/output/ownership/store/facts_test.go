package store

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestDecodeRegistryFactsDoesNotObserveRecordedNamespaces(t *testing.T) {
	root := canonicalTestRoot(t)
	blocked := filepath.Join(root, "not-a-directory")
	claim := testActiveClaim(t, blocked, "owner", filepath.Join(blocked, "settings.json"), "/model")
	binding := storedDocumentBinding(t, claim, "@pi-agent/settings.json")
	registry, err := ownership.NewRegistryWithProtections([]ownership.Claim{claim}, []ownership.DocumentBinding{binding},
		[]ownership.PendingProtection{storedPendingProtection(t, binding)})
	if err != nil {
		t.Fatal(err)
	}
	content, err := Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocked, []byte("blocks path traversal"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := DecodeRegistryFacts(content)
	if err != nil || !loaded.Equal(registry) {
		t.Fatal("strict facts depended on current path access", err)
	}
	reencoded, err := Marshal(loaded)
	if err != nil || !bytes.Equal(content, reencoded) {
		t.Fatal("facts decoding changed claims, bindings or pending protection", err)
	}

	path := filepath.Join(root, "claims.json")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(t.Context()); err == nil {
		t.Fatal("facts decoding removed the normal Store's live observation guard")
	}
}

func TestDecodeRegistryFactsPreservesClosedVersionAndStrictJSONAdmission(t *testing.T) {
	if registry, err := DecodeRegistryFacts([]byte(`{"version":1,"claims":[]}`)); err != nil || !registry.Equal(ownership.EmptyRegistry()) {
		t.Fatal("exact empty retired facts were lost", err)
	}
	image, err := NewRegistryImage(ownership.EmptyRegistry(), nil)
	if err != nil {
		t.Fatal(err)
	}
	located, err := MarshalImage(image)
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range [][]byte{
		located,
		[]byte(`{"version":4,"claims":[],"document_bindings":[],"claims":[]}`),
		[]byte(`{"version":4,"claims":[],"document_bindings":[],"unexpected":true}`),
		[]byte(`{"version":4,"claims":[],"document_bindings":[]} {}`),
		[]byte(`{"version":4,"claims":[]}`),
	} {
		registry, err := DecodeRegistryFacts(content)
		if err == nil || len(registry.Claims()) != 0 || len(registry.DocumentBindings()) != 0 || len(registry.PendingProtections()) != 0 {
			t.Fatal("invalid or located bytes became usable legacy facts", err)
		}
	}
}

func TestRequiredReferenceIdentitiesIncludesEveryRegistryFamily(t *testing.T) {
	root := canonicalTestRoot(t)
	claim := testActiveClaim(t, root, "claim-owner", filepath.Join(root, "claimed.json"), "/model")
	bindingOnly := testActiveClaim(t, root, "binding-owner", filepath.Join(root, "bound.json"), "/theme")
	protectedOnly := testActiveClaim(t, root, "protected-owner", filepath.Join(root, "protected.json"), "/style")
	registry, err := ownership.NewRegistryWithProtections([]ownership.Claim{claim},
		[]ownership.DocumentBinding{storedDocumentBinding(t, bindingOnly, "@pi-agent/settings.json")},
		[]ownership.PendingProtection{storedPendingProtection(t, storedDocumentBinding(t, protectedOnly, ".pi/settings.json"))})
	if err != nil {
		t.Fatal(err)
	}
	want := []pathauthority.Exact{
		claim.Address().PathAuthority(), claim.Owner().StatefileAuthority(),
		bindingOnly.Address().PathAuthority(), bindingOnly.Owner().StatefileAuthority(),
		protectedOnly.Address().PathAuthority(), protectedOnly.Owner().StatefileAuthority(),
	}
	slices.SortFunc(want, pathauthority.Exact.Compare)
	actual := RequiredReferenceIdentities(registry)
	if !slices.Equal(actual, want) {
		t.Fatal("reference census lost a family, ordering or uniqueness", actual, want)
	}
	references := make([]pathauthority.Reference, 0, len(actual))
	for index, identity := range actual {
		references = append(references, mustImageReference(t, identity, filepath.Join(root, fmt.Sprintf("selected-%d", index))))
	}
	if _, err := NewRegistryImage(registry, references); err != nil {
		t.Fatal("public census disagreed with the image's membership", err)
	}
	actual[0] = pathauthority.Exact{}
	if !slices.Equal(RequiredReferenceIdentities(registry), want) || len(RequiredReferenceIdentities(ownership.EmptyRegistry())) != 0 {
		t.Fatal("reference census exposed mutable facts or invented empty-registry identities")
	}
}
