package projection

import (
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
)

func TestHostConfigKeySelectionPreservesCanonicalScope(t *testing.T) {
	id, err := entity.New(entity.KindHostConfig, "preferences")
	if err != nil {
		t.Fatal(err)
	}
	path := func(names ...string) output.KeyPath {
		t.Helper()
		key, err := output.NewKeyPath(names...)
		if err != nil {
			t.Fatal(err)
		}
		return key
	}
	parent, literal, sibling := path("a"), path("a/b"), path("ab")
	input := []output.KeyPath{literal, parent, parent}
	selection, err := NewHostConfigKeySelection(id, input)
	if err != nil {
		t.Fatal(err)
	}
	if selection.Declaration() != id || !slices.Equal(selection.Selectors(), []output.KeyPath{parent, literal}) {
		t.Fatal("declaration and canonical unique selectors must be retained")
	}

	input[0] = sibling
	exposed := selection.Selectors()
	exposed[0] = sibling
	if !selection.Includes(path("a", "child")) || !selection.Includes(literal) || selection.Includes(sibling) {
		t.Fatal("selection must retain its own path scope and distinguish literal names from descendants")
	}
	for _, selectors := range [][]output.KeyPath{nil, {{}}} {
		all, err := NewHostConfigKeySelection(id, selectors)
		if err != nil || !all.Includes(sibling) {
			t.Fatalf("an omitted or root selector must cover the declaration: %v", err)
		}
	}
}

func TestHostConfigKeySelectionRefusesOtherDeclarations(t *testing.T) {
	wrong, err := entity.New(entity.KindSkill, "preferences")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []entity.ID{{}, wrong} {
		if _, err := NewHostConfigKeySelection(id, nil); err == nil {
			t.Fatal("selection must have one host-config declaration")
		}
	}
	if err := (HostConfigKeySelection{}).Validate(); err == nil {
		t.Fatal("a zero selection is not valid")
	}
}
