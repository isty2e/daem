package ownership

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/test/outputtest"
)

func TestRegistryDocumentBindingsAtRetainsAllNonexclusiveParticipants(t *testing.T) {
	root := t.TempDir()
	namespace := pathtest.Exact(filepath.Join(root, "document.json"))
	own := mustAuthority(t, filepath.Join(root, "own-state"), filepath.Join(root, "own.toml"))
	foreign := mustAuthority(t, filepath.Join(root, "foreign-state"), filepath.Join(root, "foreign.toml"))
	var bindings []DocumentBinding
	for _, input := range []struct {
		foreign bool
		path    string
		other   bool
	}{
		{false, "one.json", false}, {false, "alias.json", false}, {true, "one.json", false}, {true, "elsewhere.json", true},
	} {
		owner, exact := own, namespace
		if input.foreign {
			owner = foreign
		}
		if input.other {
			exact = pathtest.Exact(filepath.Join(root, "elsewhere.json"))
		}
		binding, err := NewDocumentBinding(owner, outputtest.Parse(t, input.path), exact)
		if err != nil {
			t.Fatal(err)
		}
		bindings = append(bindings, binding)
	}
	registry, err := NewRegistry(nil, bindings...)
	if err != nil || len(registry.Claims()) != 0 {
		t.Fatal("binding query fixture gained exclusive claims", err)
	}
	got, err := registry.DocumentBindingsAt(namespace)
	if err != nil || len(got) != 3 {
		t.Fatal("namespace query lost an owner or alias participant", err)
	}
	for index, binding := range got {
		if !binding.PathAuthority().Equal(namespace) || index != 0 && !got[index-1].less(binding) {
			t.Fatal("namespace query changed canonical identity/order")
		}
	}
	original := got[0]
	got[0] = DocumentBinding{}
	again, err := registry.DocumentBindingsAt(namespace)
	if err != nil || !again[0].Equal(original) {
		t.Fatal("namespace query exposed mutable Registry storage", err)
	}
	if _, err := (Registry{}).DocumentBindingsAt(namespace); err == nil {
		t.Fatal("unknown Registry became observed-empty binding evidence")
	}
	if _, err := registry.DocumentBindingsAt(pathauthority.Exact{}); err == nil {
		t.Fatal("invalid namespace became observed-empty binding evidence")
	}
	if got, err := EmptyRegistry().DocumentBindingsAt(namespace); err != nil || len(got) != 0 {
		t.Fatal("observed empty Registry was refused", err)
	}
	if got, err := registry.DocumentBindingsAt(pathtest.DarwinCaseSensitive(namespace.Key())); err != nil || len(got) != 0 {
		t.Fatal("query conflated different exact namespace witnesses", err)
	}
}
