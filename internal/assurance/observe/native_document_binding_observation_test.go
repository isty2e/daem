package observe

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestNativeDocumentBindingFragmentPreservesNonexclusiveIdentity(t *testing.T) {
	root := t.TempDir()
	namespace := pathtest.Exact(filepath.Join(root, "document.json"))
	owner, err := stateauthority.New(pathtest.Exact(filepath.Join(root, "state.json")), filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := stateauthority.New(pathtest.Exact(filepath.Join(root, "foreign-state.json")), filepath.Join(root, "foreign.toml"))
	if err != nil {
		t.Fatal(err)
	}
	alias, err := aggregate.NewDocumentAddress(target.TargetPi, target.ScopeProject, outputtest.Parse(t, "document.json"))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := ownership.NewDocumentBinding(foreign, outputtest.Parse(t, "unobserved-alias.json"), namespace)
	if err != nil {
		t.Fatal(err)
	}
	input := NativeDocumentClaimObservationInput{Owner: owner, Comparison: namespace, Aliases: []aggregate.DocumentAddress{alias}, CurrentBindings: []ownership.DocumentBinding{binding}}
	observation, err := NewNativeDocumentClaimObservation(input)
	if err != nil || len(observation.CurrentBindings()) != 1 || !observation.CurrentBindings()[0].Equal(binding) || len(observation.CurrentClaims()) != 0 || len(observation.Aliases()) != 1 || observation.Aliases()[0] != alias {
		t.Fatal("binding fragment lost provenance, acquired a claim or invented an observed alias", err)
	}
	input.CurrentBindings[0] = ownership.DocumentBinding{}
	observation.CurrentBindings()[0] = ownership.DocumentBinding{}
	if !observation.CurrentBindings()[0].Equal(binding) {
		t.Fatal("binding participant was mutable through input or getter")
	}
	for _, fault := range []string{"invalid", "duplicate identity", "different namespace"} {
		t.Run(fault, func(t *testing.T) {
			input.CurrentBindings = []ownership.DocumentBinding{binding}
			switch fault {
			case "invalid":
				input.CurrentBindings[0] = ownership.DocumentBinding{}
			case "duplicate identity":
				input.CurrentBindings = append(input.CurrentBindings, binding)
			case "different namespace":
				other, err := ownership.NewDocumentBinding(foreign, binding.Destination(), pathtest.Exact(filepath.Join(root, "other.json")))
				if err != nil {
					t.Fatal(err)
				}
				input.CurrentBindings[0] = other
			}
			if result, err := NewNativeDocumentClaimObservation(input); err == nil || result.Validate() == nil {
				t.Fatal("invalid binding fragment produced a usable observation")
			}
		})
	}
}
