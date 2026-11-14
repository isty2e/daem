package configuration

import (
	"errors"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestNativeExclusionFacetsKeepEachPathAtItsOwnRoot(t *testing.T) {
	configurationRoot := rootFact{"fixture-configuration", "fixture-configuration-v1"}
	dataRoot := rootFact{"fixture-data", "fixture-data-v1"}
	facets := []nativePathFacet{
		{root: configurationRoot, paths: []nativePathReservation{{"plugin", "loaded code", false}}},
		{root: dataRoot, paths: []nativePathReservation{{"auth.json", "authentication", true}}},
	}
	requirements, err := protectionForFacets(facets)
	if err != nil {
		t.Fatal(err)
	}
	references, err := requirements.References()
	if err != nil || len(references) != 2 {
		t.Fatalf("root-bound requirements = %v, %v", references, err)
	}
	for _, root := range []rootFact{configurationRoot, dataRoot} {
		reference, err := root.reference()
		if err != nil || !slices.Contains(references, reference) {
			t.Fatalf("facet root missing from requirements: %v, %v", reference, err)
		}
	}
	for _, test := range []struct {
		root     rootFact
		path     string
		reserved bool
	}{
		{configurationRoot, "auth.json", false},
		{dataRoot, "auth.json", true},
		{configurationRoot, "plugin/tool.js", true},
		{dataRoot, "plugin/tool.js", false},
	} {
		err := requireNativeRootPath(facets, test.root, target.ScopeGlobal, test.path)
		if test.reserved && !errors.Is(err, ErrReserved) || !test.reserved && err != nil {
			t.Errorf("root %s path %q reserved=%t: %v", test.root.identity, test.path, test.reserved, err)
		}
	}
}

func TestExclusionRequirementsAreDerivedFromCompleteBoundFacets(t *testing.T) {
	root := rootFact{"fixture-configuration", "fixture-configuration-v1"}
	requirements, err := protectionForFacets([]nativePathFacet{{root: root}})
	if err != nil {
		t.Fatal(err)
	}
	if references, err := requirements.References(); err != nil || len(references) != 0 {
		t.Fatalf("checked empty facet needs no protection selector: %v, %v", references, err)
	}

	requirements, err = protectionForFacets([]nativePathFacet{
		{root: root, paths: []nativePathReservation{{"auth.json", "authentication", true}}},
		{paths: []nativePathReservation{{"secret", "authentication", true}}},
	})
	if err == nil {
		t.Fatal("a reserved path without a root supplied requirements")
	}
	if _, err := requirements.References(); err == nil {
		t.Fatal("a later invalid facet leaked partial known requirements")
	}
}

func TestMissingNativeRootFacetsAreNotCheckedEmptyFacets(t *testing.T) {
	root := rootFact{"fixture-configuration", "fixture-configuration-v1"}
	data := rootFact{"fixture-data", "fixture-data-v1"}
	for _, facets := range []nativePathFacets{
		nil,
		{},
		{{}},
		{{root: data, paths: []nativePathReservation{{"auth.json", "authentication", true}}}},
		{{root: rootFact{root.identity, "fixture-configuration-v2"}}},
	} {
		if err := requireNativeRootPath(facets, root, target.ScopeGlobal, "unrelated.json"); err == nil {
			t.Fatal("missing native root facet bypassed logical admission")
		}
		requirements, err := facets.requirementsForRoot(root)
		if err == nil {
			t.Fatal("missing native root facet supplied checked requirements")
		}
		if _, err := requirements.References(); err == nil {
			t.Fatal("missing native root facet became known-no-protection")
		}
	}
	for _, facets := range []nativePathFacets{nil, {}, {{}}} {
		if _, err := protectionForFacets(facets); err == nil {
			t.Fatal("missing or zero facets became known-no-protection")
		}
	}
	checked := nativePathFacets{{root: root}}
	if err := requireNativeRootPath(checked, root, target.ScopeGlobal, "unrelated.json"); err != nil {
		t.Fatal(err)
	}
	requirements, err := checked.requirementsForRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if references, err := requirements.References(); err != nil || len(references) != 0 {
		t.Fatalf("explicit checked-empty root = %v, %v", references, err)
	}
}

func TestNativeFacetRequirementsIncludePatternOnlyRoots(t *testing.T) {
	root := rootFact{"fixture-configuration", "fixture-configuration-v1"}
	for _, facet := range []nativePathFacet{
		{root: root, moduleDirectory: "extensions"},
		{root: root, runtimeFiles: true},
	} {
		requirements, err := protectionForFacets(nativePathFacets{facet})
		if err != nil {
			t.Fatal(err)
		}
		references, err := requirements.References()
		if err != nil || len(references) != 1 {
			t.Fatalf("pattern-only facet lost its required root: %v, %v", references, err)
		}
	}
}

func TestNativeFacetRootMatchingPreservesLocatorContract(t *testing.T) {
	first := rootFact{"fixture-root", "fixture-root-v1"}
	second := rootFact{"fixture-root", "fixture-root-v2"}
	facets := nativePathFacets{
		{root: first, paths: []nativePathReservation{{"auth.json", "authentication", true}}},
		{root: second, paths: []nativePathReservation{{"cache", "runtime cache", true}}},
	}
	for _, test := range []struct {
		root rootFact
		path string
	}{{first, "cache/value.json"}, {second, "auth.json"}} {
		if err := requireNativeRootPath(facets, test.root, target.ScopeGlobal, test.path); err != nil {
			t.Fatalf("another locator contract supplied this root's rules: %v", err)
		}
	}
}
