package catalog

import (
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestEffectiveObservationCapabilityIsExplicitCatalogRegistration(t *testing.T) {
	seed := productSeed()
	seed.EffectiveObservers = nil
	without, err := Compile(seed)
	if err != nil {
		t.Fatal(err)
	}
	key := mustMCPKey(t, target.TargetPi, target.ScopeProject)
	view, present := without.Lookup(key)
	if !present || view.SupportsObservation(ObservationEffectiveConfiguration) || slices.Contains(view.ObservationPurposes(), ObservationEffectiveConfiguration) {
		t.Fatal("Pi identity alone manufactured an observation capability")
	}

	seed.EffectiveObservers = []aggregate.MCPPlacementID{aggregate.MCPPlacementPiProject}
	with, err := Compile(seed)
	if err != nil {
		t.Fatal(err)
	}
	view, present = with.Lookup(key)
	if !present || !view.SupportsObservation(ObservationEffectiveConfiguration) || !slices.Contains(view.ObservationPurposes(), ObservationEffectiveConfiguration) {
		t.Fatal("registered observation capability was not compiled")
	}
	global, present := with.Lookup(mustMCPKey(t, target.TargetPi, target.ScopeGlobal))
	if !present || global.SupportsObservation(ObservationEffectiveConfiguration) {
		t.Fatal("project registration leaked into another placement")
	}
	if view.SupportsObservation(ObservationPurpose("future_purpose")) {
		t.Fatal("unregistered future observation purpose was admitted")
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
