package readiness

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestNativeWholeAdoptionUsesBindingOwnerDestinationAndNamespace(t *testing.T) {
	for _, test := range []struct {
		name    string
		blocked bool
	}{
		{"foreign same destination", true},
		{"same owner other alias", true},
		{"foreign unrelated namespace", false},
		{"same owner changed provenance", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newNativePlanningFixture(t, target.ScopeProject, "extension-settings/test.json")
			destination, physical := outputtest.Parse(t, "shared/peer.json"), fixture.hostPath
			if test.name == "foreign same destination" || test.name == "same owner changed provenance" {
				destination = fixture.observation.Aliases[0].AggregateRoot()
			}
			if test.name == "foreign unrelated namespace" {
				physical = filepath.Join(fixture.observation.Roots.Manifest.SelectedPath, "unrelated.json")
				requireNativePreparationFile(t, physical, fixture.original)
			}
			binding := nativeForeignProjectBinding(t, fixture, destination, physical)
			if test.name == "same owner other alias" || test.name == "same owner changed provenance" {
				owner := fixture.observation.Owner
				if test.name == "same owner changed provenance" {
					var err error
					owner, err = stateauthority.New(owner.StatefileAuthority(), filepath.Join(fixture.observation.Roots.Manifest.SelectedPath, "moved-manifest.toml"))
					if err != nil {
						t.Fatal(err)
					}
				}
				var err error
				binding, err = ownership.NewDocumentBinding(owner, destination, binding.PathAuthority())
				if err != nil {
					t.Fatal(err)
				}
			}
			registry, err := fixture.observation.Registry.Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			bindings := registry.DocumentBindings()
			if test.name == "same owner changed provenance" {
				bindings = nil
			}
			registry, err = ownership.NewRegistry(registry.Claims(), append(bindings, binding)...)
			if err != nil {
				t.Fatal(err)
			}
			content, err := ownershipstore.Marshal(registry)
			if err != nil {
				t.Fatal(err)
			}
			requireNativePreparationFile(t, fixture.registryPath, content)
			fixture.observe(t)
			reads := fixture.reader.reads
			plan, err := PrepareNativeWholeAdoption(t.Context(), fixture.adoption(t, true))
			if err != nil || plan.Validate() != nil || plan.Decision().IsBlocked() != test.blocked || fixture.reader.reads != reads {
				t.Fatal("whole adoption conflated binding identity, provenance or physical namespace", err)
			}
			if test.blocked && len(plan.RetiredSubjects()) != 0 || !test.blocked && (plan.Decision().Kind() != reconcile.ManagedPathRecord || len(plan.RetiredSubjects()) != 1) {
				t.Fatal("binding classification forced mutation or consumed the wrong prior rows")
			}
			facts, err := fixture.input.Documents.Evidence().Ownership(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			fragment, err := facts.Document(t.Context(), fixture.observation.Aliases[0])
			if err != nil || len(fragment.Aliases()) != 1 {
				t.Fatal("stored binding destination became an unobserved namespace alias", err)
			}
			if test.name == "same owner changed provenance" && (len(fragment.CurrentBindings()) != 1 || !fragment.CurrentBindings()[0].Equal(binding)) {
				t.Fatal("binding projection replaced exact diagnostic provenance")
			}
		})
	}
}
