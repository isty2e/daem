package journal

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestDocumentStateGroupsWholeRetargetRequiresDistinctRetireCreateGroups(t *testing.T) {
	fixture := documentClaimsForTest(t)
	beforeRow := nativeWholeFileJournalBaseline(t, target.TargetPi, target.ScopeGlobal)
	destination, err := output.Parse("@pi-agent/extension-settings/moved.json")
	if err != nil {
		t.Fatal(err)
	}
	afterRow, err := durable.NewManagedPathState(beforeRow.Subject(), beforeRow.ConsumerTargets(), beforeRow.Scope(), destination,
		testContentHash(`{"model":2}`), beforeRow.ContentKind(), beforeRow.PermissionPolicy(), beforeRow.FileMode())
	if err != nil {
		t.Fatal(err)
	}
	before, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{beforeRow}})
	if err != nil {
		t.Fatal(err)
	}
	after, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{afterRow}})
	if err != nil {
		t.Fatal(err)
	}
	oldFixture, newFixture := fixture, fixture
	oldFixture.alias, newFixture.alias = stateGroupWholeAlias(t, beforeRow), stateGroupWholeAlias(t, afterRow)
	oldValue, newValue := membershipBindingValue(t, oldFixture), membershipBindingValue(t, newFixture)
	retire := membershipIntent(t, oldFixture, oldValue, ownership.NoDocumentBinding())
	acquire := membershipIntent(t, newFixture, ownership.NoDocumentBinding(), newValue)

	merged := documentStateGroupInput{
		aliases:        []aggregate.DocumentAddress{oldFixture.alias, newFixture.alias},
		comparison:     stateGroupWholeComparison(t, `{"model":1}`, `{"model":2}`),
		bindingIntents: []ownership.DocumentBindingIntent{retire, acquire},
	}
	for range 2 {
		if groups, err := reconstructDocumentStateGroups(t.Context(), fixture.owner, before, after, []documentStateGroupInput{merged}); err == nil || groups != nil {
			t.Fatal("one whole comparison silently moved a subject between document destinations")
		}
		merged.aliases[0], merged.aliases[1] = merged.aliases[1], merged.aliases[0]
		merged.bindingIntents[0], merged.bindingIntents[1] = merged.bindingIntents[1], merged.bindingIntents[0]
	}

	mode := recovery.NewPermissionMode(0o600)
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	retirement, err := NewWholeDocumentComparison(t.Context(), DocumentComparisonInput{
		Before: aggregate.ExistingDocument([]byte(`{"model":1}`)), Expected: aggregate.AbsentDocument(),
		BeforeMode: mode, Limits: limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	creation, err := NewWholeDocumentComparison(t.Context(), DocumentComparisonInput{
		Before: aggregate.AbsentDocument(), Expected: aggregate.ExistingDocument([]byte(`{"model":2}`)),
		ExpectedMode: mode, Limits: limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	inputs := []documentStateGroupInput{
		{aliases: []aggregate.DocumentAddress{oldFixture.alias}, comparison: retirement, bindingIntents: []ownership.DocumentBindingIntent{retire}},
		{aliases: []aggregate.DocumentAddress{newFixture.alias}, comparison: creation, bindingIntents: []ownership.DocumentBindingIntent{acquire}},
	}
	for range 2 {
		groups, err := reconstructDocumentStateGroups(t.Context(), fixture.owner, before, after, inputs)
		if err != nil || len(groups) != 2 {
			t.Fatalf("explicit whole-file retirement/creation was refused: %v", err)
		}
		inputs[0], inputs[1] = inputs[1], inputs[0]
	}
}

func TestDocumentStateGroupsKeyRetargetCannotBorrowAnotherSubjectsFrontier(t *testing.T) {
	fixture := documentClaimsForTest(t)
	oldRow := selectionNativeRow(t, piSelectionContract(t, ".pi/old.json", "node", "leaf"), "1")
	newRow := selectionNativeRow(t, piSelectionContract(t, ".pi/new.json", "node", "leaf"), "2")
	parent := selectionNativeRow(t, piSelectionContract(t, ".pi/new.json", "node"), "true")
	id, err := entity.New(entity.KindHostConfig, "prior-new-document")
	if err != nil {
		t.Fatal(err)
	}
	parentSubject, err := topologyprojection.HostConfigKeySubject(id, target.TargetPi, target.ScopeProject, parent.Contribution().Contract().Address().KeyPath())
	if err != nil {
		t.Fatal(err)
	}
	parent, err = durable.NewManagedAggregateState(parentSubject, parent.Contribution())
	if err != nil {
		t.Fatal(err)
	}
	before, after := selectionState(t, oldRow, parent), selectionState(t, newRow)
	oldFixture, newFixture := fixture, fixture
	oldFixture.alias = oldRow.Contribution().Contract().Address().Document()
	newFixture.alias = newRow.Contribution().Contract().Address().Document()
	oldValue, newValue := membershipBindingValue(t, oldFixture), membershipBindingValue(t, newFixture)
	retire := membershipIntent(t, oldFixture, oldValue, ownership.NoDocumentBinding())
	retain := membershipIntent(t, newFixture, newValue, newValue)
	newInput := stateGroupInput(t, newFixture, []durable.ManagedAggregateState{parent}, `{"node":true}`, `{"node":{"leaf":2}}`)
	newInput.bindingIntents = []ownership.DocumentBindingIntent{retain}
	merged := newInput
	merged.aliases = []aggregate.DocumentAddress{oldFixture.alias, newFixture.alias}
	merged.bindingIntents = []ownership.DocumentBindingIntent{retire, retain}
	if groups, err := reconstructDocumentStateGroups(t.Context(), fixture.owner, before, after, []documentStateGroupInput{merged}); err == nil || groups != nil {
		t.Fatal("retargeted unselected child borrowed another subject's selected parent in the same group")
	}

	oldInput := stateGroupInput(t, oldFixture, []durable.ManagedAggregateState{oldRow}, `{"node":{"leaf":1}}`, `{}`)
	oldInput.bindingIntents = []ownership.DocumentBindingIntent{retire}
	newInput.selectedSubjects = []topology.SubjectID{parent.Subject(), newRow.Subject()}
	inputs := []documentStateGroupInput{oldInput, newInput}
	for range 2 {
		if groups, err := reconstructDocumentStateGroups(t.Context(), fixture.owner, before, after, inputs); err != nil || len(groups) != 2 {
			t.Fatalf("explicit selected retirement/creation lost its actual parent frontier: %v", err)
		}
		inputs[0], inputs[1] = inputs[1], inputs[0]
	}
}
