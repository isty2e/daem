package listworkflow

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestCanonicalOutputInventoryEntriesDeduplicatesOnlyEquivalentRows(t *testing.T) {
	entry := outputInventoryTestEntry(t, "review")

	entries, err := canonicalOutputInventoryEntries(
		"unmanaged",
		[]OutputInventoryEntry{entry, entry},
	)
	if err != nil {
		t.Fatalf("canonicalOutputInventoryEntries returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %#v, want one equivalent row", entries)
	}

	conflicting := entry
	conflicting.detail = "different evidence"
	if _, err := canonicalOutputInventoryEntries(
		"unmanaged",
		[]OutputInventoryEntry{entry, conflicting},
	); err == nil {
		t.Fatal("conflicting duplicate inventory rows were accepted")
	}
}

func TestOutputInventoryReturnsDefensiveCopies(t *testing.T) {
	entry := outputInventoryTestEntry(t, "review")
	inventory := OutputInventory{managed: []OutputInventoryEntry{entry}}

	first := inventory.Managed()
	first[0].targets[0] = target.TargetClaudeCode
	first[0].detail = "mutated"

	second := inventory.Managed()
	if len(second) != 1 || second[0].Targets()[0] != target.TargetCodex ||
		second[0].Detail() != entry.detail {
		t.Fatalf("inventory changed through returned copy: %#v", second)
	}
	targets := second[0].Targets()
	targets[0] = target.TargetClaudeCode
	if second[0].Targets()[0] != target.TargetCodex {
		t.Fatal("entry targets changed through returned copy")
	}
}

func outputInventoryTestEntry(t *testing.T, name string) OutputInventoryEntry {
	t.Helper()
	entityID, err := entity.New(entity.KindSkill, name)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topology.NewSubjectID(topology.SubjectProjection, "skill", name)
	if err != nil {
		t.Fatal(err)
	}
	return OutputInventoryEntry{
		entityID: entityID,
		subject:  subject,
		targets:  []target.Target{target.TargetCodex},
		scope:    target.ScopeProject,
		path:     ".agents/skills/" + name,
		hash:     "sha256:live",
		reason:   reconcile.ReasonUnmanagedOutputExists,
		detail:   "destination exists but is not recorded as managed",
	}
}
