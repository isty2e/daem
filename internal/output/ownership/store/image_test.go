package store

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestRegistryImageRequiresCompleteUnambiguousReferences(t *testing.T) {
	root := canonicalTestRoot(t)
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "document.json"), "/model")
	binding := storedDocumentBinding(t, claim, "@pi-agent/settings.json")
	protection := storedPendingProtection(t, binding)
	registry, err := ownership.NewRegistryWithProtections([]ownership.Claim{claim}, []ownership.DocumentBinding{binding}, []ownership.PendingProtection{protection})
	if err != nil {
		t.Fatal(err)
	}
	document := mustImageReference(t, claim.Address().PathAuthority(), filepath.Join(root, "Selected.json"))
	owner := mustImageReference(t, claim.Owner().StatefileAuthority(), filepath.Join(root, "SelectedState.json"))
	for _, test := range []struct {
		name       string
		references []pathauthority.Reference
		valid      bool
	}{
		{name: "complete", references: []pathauthority.Reference{document, owner}, valid: true},
		{name: "reordered and repeated", references: []pathauthority.Reference{owner, document, owner}, valid: true},
		{name: "missing owner", references: []pathauthority.Reference{document}},
		{name: "missing document", references: []pathauthority.Reference{owner}},
		{name: "zero", references: []pathauthority.Reference{document, owner, {}}},
		{name: "conflicting locator", references: []pathauthority.Reference{document, owner, mustImageReference(t, document.Identity(), filepath.Join(root, "Other.json"))}},
	} {
		t.Run(test.name, func(t *testing.T) {
			image, err := NewRegistryImage(registry, test.references)
			if (err == nil) != test.valid {
				t.Fatal("unexpected registry image admission", err)
			}
			if !test.valid {
				return
			}
			test.references[0] = pathauthority.Reference{}
			if image.Validate() != nil || !image.Registry().Equal(registry) || len(image.References()) != 2 {
				t.Fatal("image changed registry facts or retained duplicates")
			}
			actual, present := image.Reference(document.Identity())
			if !present || !actual.Equal(document) {
				t.Fatal("image lost independently selected document locator")
			}
			returned := image.References()
			if !slices.IsSortedFunc(returned, func(left, right pathauthority.Reference) int { return left.Identity().Compare(right.Identity()) }) {
				t.Fatal("reference output is not canonical")
			}
			returned[0] = pathauthority.Reference{}
			if image.References()[0].IsZero() {
				t.Fatal("reference output mutated the image")
			}
		})
	}
}

func TestRegistryImageIncludesProtectionOnlyCohorts(t *testing.T) {
	root := canonicalTestRoot(t)
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "protected.json"), "/model")
	other := testActiveClaim(t, root, "other", claim.Address().Path(), "/theme")
	bindings := []ownership.DocumentBinding{
		storedDocumentBinding(t, claim, "@pi-agent/settings.json"),
		storedDocumentBinding(t, other, ".pi/settings.json"),
	}
	registry, err := ownership.NewRegistryWithProtections(nil, nil, []ownership.PendingProtection{storedPendingProtection(t, bindings...)})
	if err != nil {
		t.Fatal(err)
	}
	references := []pathauthority.Reference{
		mustImageReference(t, claim.Address().PathAuthority(), claim.Address().Path()),
		mustImageReference(t, claim.Owner().StatefileAuthority(), claim.Owner().StatefileKey()),
		mustImageReference(t, other.Owner().StatefileAuthority(), other.Owner().StatefileKey()),
	}
	bindingsOnly, err := ownership.NewRegistry(nil, bindings[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRegistryImage(bindingsOnly, references[1:2]); err == nil {
		t.Fatal("binding-only registry lost its physical reference requirement")
	}
	if _, err := NewRegistryImage(bindingsOnly, references[:2]); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRegistryImage(registry, references[:2]); err == nil {
		t.Fatal("protection cohort's other owner lost its reference requirement")
	}
	if _, err := NewRegistryImage(registry, references); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRegistryImage(ownership.EmptyRegistry(), references); err == nil {
		t.Fatal("unrelated references entered an empty registry image")
	}
	if image, err := NewRegistryImage(ownership.EmptyRegistry(), nil); err != nil || len(image.References()) != 0 {
		t.Fatal("empty registry requires invented path evidence", err)
	}
	if err := (RegistryImage{}).Validate(); err == nil {
		t.Fatal("uninitialized registry image was admitted")
	}
}

func mustImageReference(t *testing.T, identity pathauthority.Exact, locator string) pathauthority.Reference {
	t.Helper()
	reference, err := pathauthority.NewReference(identity, locator)
	if err != nil {
		t.Fatal(err)
	}
	return reference
}
