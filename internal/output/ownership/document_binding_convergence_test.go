package ownership

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
)

func TestDocumentBindingConvergenceKeepsAliasesClaimsAndPartialSettlement(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "settings.json")
	acquired := testDocumentBinding(t, root, "alpha", ".pi/settings.json", physical)
	retired := testDocumentBinding(t, root, "beta", ".pi/settings.json", physical)
	retained := testDocumentBinding(t, root, "alpha", "@pi-agent/settings.json", physical)
	foreign := testDocumentBinding(t, root, "foreign", ".pi/settings.json", physical)
	claim, err := NewActiveClaim(mustAddress(t, physical, ""), foreign.Owner())
	if err != nil {
		t.Fatal(err)
	}
	intents := []DocumentBindingIntent{
		mustExactBindingIntent(t, acquired, NoDocumentBinding(), mustBindingValue(t, acquired)),
		mustExactBindingIntent(t, retired, mustBindingValue(t, retired), NoDocumentBinding()),
		mustExactBindingIntent(t, retained, mustBindingValue(t, retained), mustBindingValue(t, retained)),
	}
	want, err := NewRegistry([]Claim{claim}, acquired, retained, foreign)
	if err != nil {
		t.Fatal(err)
	}

	for _, bindings := range [][]DocumentBinding{
		{retired, retained, foreign},
		{acquired, retired, retained, foreign},
		{acquired, retained, foreign},
	} {
		registry, err := NewRegistry([]Claim{claim}, bindings...)
		if err != nil {
			t.Fatal(err)
		}
		for _, order := range [][]int{{0, 1, 2}, {2, 0, 1}, {1, 2, 0}} {
			input := []DocumentBindingIntent{intents[order[0]], intents[order[1]], intents[order[2]]}
			convergence, err := NewDocumentBindingConvergence(input)
			if err != nil {
				t.Fatal(err)
			}
			input[0] = DocumentBindingIntent{}
			query := convergence.Intents()
			for index := 1; index < len(query); index++ {
				if order := query[index-1].Owner().Key().Compare(query[index].Owner().Key()); order > 0 || order == 0 && query[index-1].Destination().String() >= query[index].Destination().String() {
					t.Fatal("convergence is not canonically ordered by owner/destination")
				}
			}
			query[0] = DocumentBindingIntent{}

			next, changed, err := convergence.Apply(registry)
			if err != nil || !next.Equal(want) || changed == registry.Equal(want) {
				t.Fatalf("binding settlement lost aliases/claims or phase facts: %v", err)
			}
			again, changed, err := convergence.Apply(next)
			if err != nil || changed || !again.Equal(want) {
				t.Fatal("binding settlement was not idempotent")
			}
			unchanged, err := NewRegistry([]Claim{claim}, bindings...)
			if err != nil || !registry.Equal(unchanged) {
				t.Fatal("binding settlement changed its input registry")
			}
		}
	}
}

func TestDocumentBindingConvergenceRefusesStaleProvenanceWithoutPartialSuccessor(t *testing.T) {
	root := t.TempDir()
	first := testDocumentBinding(t, root, "owner", ".pi/a.json", filepath.Join(root, "a.json"))
	last := testDocumentBinding(t, root, "owner", ".pi/z.json", filepath.Join(root, "z.json"))
	aliasOwner, err := stateauthority.New(last.Owner().StatefileAuthority(), filepath.Join(root, "alias.toml"))
	if err != nil {
		t.Fatal(err)
	}
	stale, err := NewDocumentBinding(aliasOwner, last.Destination(), last.PathAuthority())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(nil, stale)
	if err != nil {
		t.Fatal(err)
	}
	convergence, err := NewDocumentBindingConvergence([]DocumentBindingIntent{
		mustExactBindingIntent(t, first, NoDocumentBinding(), mustBindingValue(t, first)),
		mustExactBindingIntent(t, last, mustBindingValue(t, last), NoDocumentBinding()),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result, changed, err := convergence.Apply(registry); err == nil || changed || result.Equal(result) {
		t.Fatal("a later stale exact record exposed a partial registry successor")
	}
	if actual, present := registry.DocumentBinding(last.Owner().Key(), last.Destination()); !present || !actual.Equal(stale) {
		t.Fatal("stale refusal changed original provenance")
	}
	if _, present := registry.DocumentBinding(first.Owner().Key(), first.Destination()); present {
		t.Fatal("stale refusal published an earlier acquisition")
	}
}

func TestDocumentBindingConvergenceRejectsDuplicateAndUninitializedInputs(t *testing.T) {
	root := t.TempDir()
	binding := testDocumentBinding(t, root, "owner", ".pi/settings.json", filepath.Join(root, "settings.json"))
	intent := mustExactBindingIntent(t, binding, NoDocumentBinding(), mustBindingValue(t, binding))
	aliasOwner, err := stateauthority.New(binding.Owner().StatefileAuthority(), filepath.Join(root, "alias.toml"))
	if err != nil {
		t.Fatal(err)
	}
	alias, err := NewDocumentBinding(aliasOwner, binding.Destination(), binding.PathAuthority())
	if err != nil {
		t.Fatal(err)
	}
	aliasIntent := mustExactBindingIntent(t, alias, mustBindingValue(t, alias), mustBindingValue(t, alias))
	for _, inputs := range [][]DocumentBindingIntent{{intent, intent}, {intent, aliasIntent}, {{}}} {
		if result, err := NewDocumentBindingConvergence(inputs); err == nil || result.Validate() == nil {
			t.Fatal("invalid or repeated owner/destination became an admitted settlement")
		}
	}

	var zero DocumentBindingConvergence
	if result, changed, err := zero.Apply(EmptyRegistry()); err == nil || changed || result.Equal(result) {
		t.Fatal("uninitialized convergence became a successful no-op")
	}
	empty, err := NewDocumentBindingConvergence(nil)
	if err != nil {
		t.Fatal(err)
	}
	registry := EmptyRegistry()
	if result, changed, err := empty.Apply(registry); err != nil || changed || !result.Equal(registry) {
		t.Fatal("constructed empty settlement changed an initialized registry")
	}
	if result, changed, err := empty.Apply(Registry{}); err == nil || changed || result.Equal(result) {
		t.Fatal("empty settlement accepted an uninitialized registry")
	}
}

func mustExactBindingIntent(t *testing.T, binding DocumentBinding, before, after DocumentBindingValue) DocumentBindingIntent {
	t.Helper()
	intent, err := NewExactDocumentBindingIntent(binding.Owner(), binding.Destination(), before, after)
	if err != nil {
		t.Fatal(err)
	}
	return intent
}
