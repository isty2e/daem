package recoveryoperation

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestRecoveryAuthorityKeepsBothPhysicalDocumentsForOneAlias(t *testing.T) {
	destination, err := output.Parse(aggregate.ClaudeGlobalMCPConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	alias, err := aggregate.NewDocumentAddress(target.TargetClaudeCode, target.ScopeGlobal, destination)
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	source := mutation.PhysicalAuthorityRequest{Path: filepath.Join(base, "source.json"), Target: string(alias.Target()), Scope: string(alias.Scope())}
	target := source
	target.Path = filepath.Join(base, "target.json")
	coalesced := source
	coalesced.Path = filepath.Join(base, "coalesced-current.json")
	groups := [][]mutation.PhysicalAuthorityRequest{{source}, {target}, {source, coalesced}}
	plan := NativeRecoveryPlan{documentRequests: groups}
	for index, expected := range groups {
		actual, err := plan.DocumentAuthorityRequests(index)
		if err != nil || !slices.Equal(actual, expected) {
			t.Fatal("one document's authority was replaced by its logical peer", index, actual, err)
		}
		actual[0].Path = "changed"
		again, _ := plan.DocumentAuthorityRequests(index)
		if !slices.Equal(again, expected) {
			t.Fatal("request caller changed the retained plan")
		}
	}
	if _, err := plan.DocumentAuthorityRequests(3); err == nil {
		t.Fatal("unselected document inherited another group's authority")
	}
}
