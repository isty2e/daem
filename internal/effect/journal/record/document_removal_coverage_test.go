package record

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeRemovalCoverageChecksLogicalAndPhysicalCardinality(t *testing.T) {
	fixture := nativeCreateFixture(t)
	group := fixture.record.DocumentGroups[0]
	baseline := nativeJournalRecord{DocumentGroups: []recoveryDocumentGroup{group}}
	if err := baseline.validateRemovalCoverage(); err != nil {
		t.Fatal("single physical group lost its logical removal coverage", err)
	}
	removal, err := group.Removal.canonical()
	if err != nil {
		t.Fatal(err)
	}
	physical, err := removal.PhysicalIntent()
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := persistedRecoveryRemovalIntents([]recovery.RemovalIntent{physical})
	if err != nil {
		t.Fatal(err)
	}
	duplicateRelation := nativeJournalRecord{RemovalIntents: ordinary, DocumentGroups: []recoveryDocumentGroup{group}}
	if err := duplicateRelation.validateRemovalCoverage(); err == nil {
		t.Fatal("ordinary/native duplicate logical relation admitted")
	}
	destination, err := output.Parse("other.json")
	if err != nil {
		t.Fatal(err)
	}
	demand, err := recovery.NewRemovalDemand(target.ScopeProject, destination, physical.States())
	if err != nil {
		t.Fatal(err)
	}
	set, err := recovery.NewRemovalDemandSet([]recovery.RemovalDemand{demand})
	if err != nil {
		t.Fatal(err)
	}
	other, err := recovery.NewDocumentRemoval(set, removal.Namespace())
	if err != nil {
		t.Fatal(err)
	}
	otherRecord, err := persistedDocumentRemoval(other)
	if err != nil {
		t.Fatal(err)
	}
	duplicatePhysical := nativeJournalRecord{DocumentGroups: []recoveryDocumentGroup{group, {Removal: &otherRecord}}}
	if err := duplicatePhysical.validateRemovalCoverage(); err == nil {
		t.Fatal("two physical groups reused one cleanup namespace")
	}
	secondRelation, err := recovery.NewRemovalDemandSet([]recovery.RemovalDemand{physical.Demand(), demand})
	if err != nil {
		t.Fatal(err)
	}
	joined, err := recovery.NewDocumentRemoval(secondRelation, removal.Namespace())
	if err != nil {
		t.Fatal(err)
	}
	joinedRecord, err := persistedDocumentRemoval(joined)
	if err != nil {
		t.Fatal(err)
	}
	onePhysical := nativeJournalRecord{DocumentGroups: []recoveryDocumentGroup{{Removal: &joinedRecord}}}
	if err := onePhysical.validateRemovalCoverage(); err != nil {
		t.Fatalf("one group repeated physical cleanup for its logical relations: %v", err)
	}
}
