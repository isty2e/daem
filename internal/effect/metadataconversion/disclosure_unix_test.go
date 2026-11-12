//go:build darwin || linux

package metadataconversion

import (
	"slices"
	"sort"
	"testing"
)

func TestDisclosureDescribesTheWholeCohortWithoutChangingIt(t *testing.T) {
	fixture := compilerFixture(t, 3)
	plan := prepareFixture(t, fixture.paths)
	disclosure := plan.Disclosure()
	if disclosure.ManifestPath != fixture.paths.ManifestPath || disclosure.DataDir != fixture.paths.DataDir {
		t.Fatal("disclosure lost the original selection")
	}
	if !slices.Equal(disclosure.Statefiles, fixture.states) {
		t.Fatalf("State cohort = %v, want %v", disclosure.Statefiles, fixture.states)
	}
	var targets []string
	for path := range fixture.before {
		targets = append(targets, path)
	}
	sort.Strings(targets)
	if !slices.Equal(disclosure.Targets, targets) {
		t.Fatalf("metadata targets = %v, want %v", disclosure.Targets, targets)
	}
	fixture.assertUnchanged(t)

	disclosure.Statefiles[0], disclosure.Targets[0] = "changed view", "changed view"
	fresh := plan.Disclosure()
	if !slices.Equal(fresh.Statefiles, fixture.states) || !slices.Equal(fresh.Targets, targets) {
		t.Fatal("disclosure changed the retained conversion selection")
	}
}
