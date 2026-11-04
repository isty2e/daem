package record

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestRecoveryPlanningBasisKeepsAllEntriesAndSelectedObservationsDistinct(t *testing.T) {
	first := defaultRecoveryEntry()
	second := recoveryEntryFor("second", "CLAUDE.md", testBeforeHash, testAfterHash, "backups/CLAUDE.md")
	journal := recoveryJournalFor(first, second)
	fingerprint, err := recoveryJournalAuthorityFingerprint(journal, testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	key, err := entrySelectionKeyFromRecoveryEntry(first)
	if err != nil {
		t.Fatal(err)
	}
	selected := []EntrySelection{{key: key, initialized: true}}
	basis, err := journal.PlanningBasis(testOperationDir, fingerprint, &selected)
	if err != nil || basis.Validate() != nil {
		t.Fatalf("valid planning facts failed: %v", err)
	}
	if len(basis.AllEntries()) != 2 || len(basis.PlanningEntries()) != 1 || basis.PlanningEntries()[0].Path() != first.Path {
		t.Fatal("selection narrowed complete binding checks or lost selected observation order")
	}
	if _, err := recovery.NewSelection(basis.Authority(), []int{1}); err != nil {
		t.Fatal("unobserved entry disappeared from complete authority", err)
	}
	entries, indexes := basis.AllEntries(), basis.SelectedIndexes()
	entries[0], indexes[0] = PathEntry{}, 99
	if basis.AllEntries()[0].Path() != first.Path || basis.SelectedIndexes()[0] != 0 {
		t.Fatal("planning queries exposed mutable arrays")
	}
	empty := []EntrySelection{}
	for _, selection := range []*[]EntrySelection{nil, &empty} {
		value, err := journal.PlanningBasis(testOperationDir, fingerprint, selection)
		if err != nil {
			t.Fatal(err)
		}
		if len(value.AllEntries()) != 2 {
			t.Fatal("empty selection discarded complete entries")
		}
		if selection == nil && (value.SelectedIndexes() != nil || len(value.PlanningEntries()) != 2) {
			t.Fatal("implicit all-entry selection changed")
		}
		if selection != nil && (value.SelectedIndexes() == nil || len(value.PlanningEntries()) != 0) {
			t.Fatal("explicit no-entry selection became implicit all-entry selection")
		}
	}
	if (PlanningBasis{}).Validate() == nil {
		t.Fatal("zero planning basis was usable")
	}
}

func TestRecordedEntryRetainsInvalidPathForConsumerValidation(t *testing.T) {
	entries := recordedPathEntries([]recoveryEntry{{Path: "../escape", Scope: string(target.ScopeGlobal)}})
	if len(entries) != 1 || entries[0].Path() != "../escape" {
		t.Fatal("view normalization discarded the recorded invalid path")
	}
	if _, _, err := entries[0].WriterAddress(); err == nil {
		t.Fatal("recorded invalid path supplied a canonical writer address")
	}
	if (PathEntry{}).Validate() == nil {
		t.Fatal("zero entry passed complete global binding validation")
	}
}

func TestRecordedAggregateEntryOwnsItsProjectionFacts(t *testing.T) {
	contract := recoveryTestAggregateContract(t)
	document := contract.Address().Document()
	subject, err := topology.NewSubjectID(topology.SubjectProjection, contract.Address().PlacementID(), "hook:format")
	if err != nil {
		t.Fatal(err)
	}
	contentPath, err := contract.Address().LegacyContentPath()
	if err != nil {
		t.Fatal(err)
	}
	entry := recoveryEntry{
		Subject: persistedSubjectRef{Kind: string(subject.Kind()), Namespace: subject.Namespace(), Name: subject.Key()},
		Target:  string(document.Target()), Scope: string(document.Scope()), Path: document.AggregateRoot().String(), ContentPath: string(contentPath),
		Aggregate: mustPersistedAggregateContract(t, contract),
	}
	view := recordedPathEntryView(entry)
	entry.Aggregate.AggregateRoot = "../changed"
	if len(entry.Aggregate.ComparedFields) != 0 {
		entry.Aggregate.ComparedFields[0] = "changed"
	}
	actual, err := view.AggregateContract()
	if err != nil || actual == nil || !actual.Equal(contract) {
		t.Fatalf("view retained mutable projection input: %v", err)
	}
	*actual = aggregate.ProjectionContract{}
	again, err := view.AggregateContract()
	if err != nil || again == nil || !again.Equal(contract) {
		t.Fatal("query exposed retained projection state", err)
	}
}
