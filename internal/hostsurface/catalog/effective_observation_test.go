package catalog

import (
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestEffectiveObservationCapabilityIsExplicitCatalogRegistration(t *testing.T) {
	seed := productSeed()
	without, err := Compile(seed)
	if err != nil {
		t.Fatal(err)
	}
	key := mustMCPKey(t, target.TargetOpenCode, target.ScopeProject)
	view, present := without.Lookup(key)
	if !present || view.SupportsObservation(ObservationEffectiveConfiguration) || slices.Contains(view.ObservationPurposes(), ObservationEffectiveConfiguration) {
		t.Fatal("placement identity alone manufactured an observation capability")
	}

	seed.EffectiveObservers = append(seed.EffectiveObservers, aggregate.MCPPlacementOpenCodeProject)
	with, err := Compile(seed)
	if err != nil {
		t.Fatal(err)
	}
	view, present = with.Lookup(key)
	if !present || !view.SupportsObservation(ObservationEffectiveConfiguration) || !slices.Contains(view.ObservationPurposes(), ObservationEffectiveConfiguration) {
		t.Fatal("registered observation capability was not compiled")
	}
	global, present := with.Lookup(mustMCPKey(t, target.TargetOpenCode, target.ScopeGlobal))
	if !present || global.SupportsObservation(ObservationEffectiveConfiguration) {
		t.Fatal("project registration leaked into another placement")
	}
	if view.SupportsObservation(ObservationPurpose("future_purpose")) {
		t.Fatal("unregistered future observation purpose was admitted")
	}
}

func TestCompileRejectsProviderPlacementWithoutEffectiveObserver(t *testing.T) {
	for _, missing := range []aggregate.MCPPlacementID{
		aggregate.MCPPlacementPiProject,
		aggregate.MCPPlacementPiGlobal,
	} {
		t.Run(string(missing), func(t *testing.T) {
			seed := productSeed()
			seed.ProviderFor = nil
			seed.EffectiveObservers = slices.DeleteFunc(seed.EffectiveObservers, func(id aggregate.MCPPlacementID) bool {
				return id == missing
			})

			compiled, err := Compile(seed)
			if err == nil {
				t.Fatal("provider-required placement compiled without an effective observer")
			}
			if len(compiled.Surfaces()) != 0 {
				t.Fatal("failed compilation published a partial catalog")
			}
		})
	}
}

func TestProviderAuthoringAdmissionDoesNotRequireEffectiveObservation(t *testing.T) {
	seed := productSeed()
	seed.ProviderFor[target.TargetClaudeCode] = struct{}{}
	compiled, err := Compile(seed)
	if err != nil {
		t.Fatal(err)
	}

	view, present := compiled.Lookup(mustMCPKey(t, target.TargetClaudeCode, target.ScopeProject))
	if !present || !view.ProviderAuthoringAdmitted() {
		t.Fatal("provider authoring admission was not compiled")
	}
	if view.SupportsObservation(ObservationEffectiveConfiguration) {
		t.Fatal("provider authoring admission manufactured an observation capability")
	}
}

func TestEffectiveObservationRegistrationRejectsDuplicatesAndUnknownPlacements(t *testing.T) {
	for _, ids := range [][]aggregate.MCPPlacementID{
		{aggregate.MCPPlacementPiProject, aggregate.MCPPlacementPiProject},
		{aggregate.MCPPlacementID("missing-placement")},
	} {
		seed := productSeed()
		seed.EffectiveObservers = ids
		if _, err := Compile(seed); err == nil {
			t.Fatalf("invalid observer registrations were admitted: %v", ids)
		}
	}
}
