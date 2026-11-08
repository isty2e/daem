package configuration_test

import (
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func TestPrivateStoreRegionsPreserveWorkspaceAndConfigurationBoundaries(t *testing.T) {
	policies, err := configuration.NativeExclusionPolicies()
	if err != nil {
		t.Fatal(err)
	}
	facets := make(map[string][]configuration.ExclusionFacet)
	for _, policy := range policies {
		if policy.Target() != target.TargetOpenCode {
			continue
		}
		selected, err := policy.Facets()
		if err != nil {
			t.Fatal(err)
		}
		for _, facet := range selected {
			identity := facet.Reference().Identity()
			facets[identity] = append(facets[identity], facet)
		}
	}
	for _, row := range []struct {
		root, path string
		reserved   bool
	}{
		{"opencode-data", "new-private-store/item", true},
		{"opencode-data", "worktree/team/project/settings.json", false},
		{"opencode-data", "worktree-old/settings.json", true},
		{"opencode-state", "model.json", true},
		{"opencode-cache", "bin/helper", true},
		{"opencode-configuration", "opencode.json", false},
		{"opencode-configuration", "plugins/reviewer.json", false},
		{"opencode-configuration", "plugins/reviewer.ts", true},
		{"opencode-configuration", "plugins/package/index.ts", false},
		{"opencode-package-installation", "node_modules/installed/index.js", true},
		{"opencode-additional-sources", "plugin/helper.js", true},
		{"opencode-legacy-configuration", "package-lock.json", true},
	} {
		reserved := false
		for _, facet := range facets[row.root] {
			_, matched := facet.ReservationAt(row.path)
			reserved = reserved || matched
		}
		if reserved != row.reserved {
			t.Fatalf("%s/%s reserved=%v, want %v", row.root, row.path, reserved, row.reserved)
		}
	}
	data := facets["opencode-data"][0]
	if data.CouldReserveBelow("worktree") || !data.CouldReserveBelow("worktree-old") {
		t.Fatal("future-name proof ignored the workspace partition")
	}
	pluginPossible, nestedPossible := false, false
	for _, facet := range facets["opencode-configuration"] {
		pluginPossible = pluginPossible || facet.CouldReserveBelow("plugins")
		nestedPossible = nestedPossible || facet.CouldReserveBelow("plugins/preferences")
	}
	if !pluginPossible || nestedPossible {
		t.Fatal("direct plugin scripts were confused with arbitrary nested configuration")
	}
	endpoint, reserved := facets["opencode-database-override"][0].ReservationAt(".")
	if !reserved {
		t.Fatal("explicit database lost its endpoint reservation")
	}
	suffixes := endpoint.CompanionSuffixes()
	slices.Sort(suffixes)
	if !slices.Equal(suffixes, []string{"-journal", "-shm", "-wal"}) {
		t.Fatal("database files lost their related journals", suffixes)
	}
	defaultDatabase, reserved := data.ReservationAt("opencode-local.db")
	if !reserved || len(defaultDatabase.CompanionSuffixes()) != 3 {
		t.Fatal("directory-listed databases lost their related files")
	}
	for _, fixed := range data.FixedReservations() {
		if fixed.Path() == "." {
			t.Fatal("a whole-root endpoint would bypass the workspace exemption")
		}
	}
}
