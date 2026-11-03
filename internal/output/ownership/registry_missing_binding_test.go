package ownership

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/test/outputtest"
)

func TestMissingBindingQueryRetainsIdentityWithoutExactPromotion(t *testing.T) {
	root := t.TempDir()
	exact := pathtest.DarwinCaseSensitive(filepath.Join(root, "Café.json"))
	missing, err := pathauthority.NewProvisional(exact.Key(), exact.Witness(), root, pathtest.DarwinCaseSensitive(root).Witness())
	if err != nil {
		t.Fatal(err)
	}
	own := mustAuthority(t, filepath.Join(root, "own-state"), filepath.Join(root, "own.toml"))
	foreign := mustAuthority(t, filepath.Join(root, "foreign-state"), filepath.Join(root, "foreign.toml"))
	var bindings []DocumentBinding
	for _, input := range []struct {
		name      string
		namespace pathauthority.Exact
		foreign   bool
	}{
		{"one.json", exact, false},
		{"alias.json", exact, true},
		{"other.json", pathtest.DarwinCaseSensitive(filepath.Join(root, "other.json")), true},
		{"wrong-witness.json", pathtest.Exact(exact.Key()), true},
	} {
		owner := own
		if input.foreign {
			owner = foreign
		}
		binding, err := NewDocumentBinding(owner, outputtest.Parse(t, input.name), input.namespace)
		if err != nil {
			t.Fatal(err)
		}
		bindings = append(bindings, binding)
	}
	registry, err := NewRegistry(nil, bindings...)
	if err != nil {
		t.Fatal(err)
	}
	got, err := registry.DocumentBindingsForMissing(missing)
	if err != nil || len(got) != 2 {
		t.Fatal("provisional namespace query lost an owner/alias or accepted unrelated history", err)
	}
	for _, binding := range got {
		if !binding.PathAuthority().Equal(exact) || !missing.MatchesMissingExact(binding.PathAuthority()) {
			t.Fatal("stored exact history was changed by provisional lookup")
		}
	}
	original := got[0]
	got[0] = DocumentBinding{}
	again, err := registry.DocumentBindingsForMissing(missing)
	if err != nil || !again[0].Equal(original) {
		t.Fatal("missing binding query exposed mutable Registry")
	}
	if _, err := (Registry{}).DocumentBindingsForMissing(missing); err == nil {
		t.Fatal("unknown Registry became observed-empty missing metadata")
	}
	if _, err := registry.DocumentBindingsForMissing(pathauthority.Provisional{}); err == nil {
		t.Fatal("invalid missing observation yielded usable metadata")
	}
	if got, err := EmptyRegistry().DocumentBindingsForMissing(missing); err != nil || len(got) != 0 {
		t.Fatal("observed empty Registry was refused", err)
	}
}
