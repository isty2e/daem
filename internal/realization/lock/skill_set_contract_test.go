package lock

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
)

func TestSkillSetChildrenAllowsEmptyCurrentAndLockedSets(t *testing.T) {
	section, err := NewLockedSection(nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	children, err := section.SkillSetChildren(nil, nil)
	if err != nil {
		t.Fatalf("SkillSetChildren returned error: %v", err)
	}
	if len(children) != 0 {
		t.Fatalf("SkillSetChildren returned %d children, want 0", len(children))
	}
}

func TestSkillSetChildrenRejectsCorrelationAfterDeclarationRemoval(t *testing.T) {
	set := testSkillSet(t, sourcetest.Local(t, "skills", source.LocalSourceModeVendor), []string{"glob:*"})
	section := testLockedSection(t, correlatedSkillSubject(t, set, "review"))

	_, err := section.SkillSetChildren(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "stale skill_group declaration") {
		t.Fatalf("SkillSetChildren error = %v, want stale declaration rejection", err)
	}
}

func TestSkillSetChildrenReconstructsExactLockedMemberWithoutListing(t *testing.T) {
	set := testSkillSet(t, sourcetest.Local(t, "skills", source.LocalSourceModeVendor), []string{"glob:*"})
	contract := correlatedSkillSubject(t, set, "review")
	section := testLockedSection(t, contract)

	children, err := section.SkillSetChildren(nil, []skill.SkillSet{set})
	if err != nil {
		t.Fatalf("SkillSetChildren returned error: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("SkillSetChildren returned %d children, want 1", len(children))
	}
	if children[0].ID() != contract.EntityID() {
		t.Fatalf("child ID = %q, want %q", children[0].ID(), contract.EntityID())
	}
	gotSourceID, err := source.SourceIDFor(children[0].Source())
	if err != nil {
		t.Fatal(err)
	}
	exact, ok := contract.ExactSupply()
	if !ok {
		t.Fatal("correlated contract is missing exact Supply")
	}
	if gotSourceID != exact.SourceID() {
		t.Fatalf("child source ID = %q, want locked %q", gotSourceID, exact.SourceID())
	}
}

func TestSkillSetChildrenRejectsDirectDeclarationCollision(t *testing.T) {
	set := testSkillSet(t, sourcetest.Local(t, "skills", source.LocalSourceModeVendor), []string{"glob:*"})
	contract := correlatedSkillSubject(t, set, "review")
	section := testLockedSection(t, contract)
	exact, _ := contract.ExactSupply()
	childSource, err := lockedSkillSetChildSource(set.Source(), contract.EntityID().Name(), exact)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := set.Child(contract.EntityID().Name(), childSource)
	if err != nil {
		t.Fatal(err)
	}

	_, err = section.SkillSetChildren([]skill.Skill{direct}, []skill.SkillSet{set})
	if err == nil || !strings.Contains(err.Error(), "current manifest also declares it directly") {
		t.Fatalf("SkillSetChildren error = %v, want direct declaration collision", err)
	}
}

func TestSkillSetChildrenRejectsCurrentSetWithoutLockedMembers(t *testing.T) {
	set := testSkillSet(t, sourcetest.Local(t, "skills", source.LocalSourceModeVendor), []string{"glob:*"})
	section, err := NewLockedSection(nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = section.SkillSetChildren(nil, []skill.SkillSet{set})
	if err == nil || !strings.Contains(err.Error(), "no locked members match") {
		t.Fatalf("SkillSetChildren error = %v, want missing locked membership rejection", err)
	}
}

func TestSkillSetChildrenRejectsChangedDeclarationIdentity(t *testing.T) {
	root := sourcetest.Local(t, "skills", source.LocalSourceModeVendor)
	lockedSet := testSkillSet(t, root, []string{"glob:*"})
	currentSet := testSkillSet(t, root, []string{"glob:review*"})
	section := testLockedSection(t, correlatedSkillSubject(t, lockedSet, "review"))

	_, err := section.SkillSetChildren(nil, []skill.SkillSet{currentSet})
	if err == nil || !strings.Contains(err.Error(), "stale skill_group declaration") {
		t.Fatalf("SkillSetChildren error = %v, want changed declaration rejection", err)
	}
}

func TestSkillSetChildrenRejectsDuplicateCurrentDeclarationIdentity(t *testing.T) {
	set := testSkillSet(t, sourcetest.Local(t, "skills", source.LocalSourceModeVendor), []string{"glob:*"})
	section := testLockedSection(t, correlatedSkillSubject(t, set, "review"))

	_, err := section.SkillSetChildren(nil, []skill.SkillSet{set, set})
	if err == nil || !strings.Contains(err.Error(), "duplicates declaration identity") {
		t.Fatalf("SkillSetChildren error = %v, want duplicate declaration identity rejection", err)
	}
}

func correlatedSkillSubject(t *testing.T, set skill.SkillSet, name string) LockedSubjectContract {
	t.Helper()
	entityID := mustContractEntityID(t, entity.KindSkill, name)
	childSource, err := set.Source().Child(name)
	if err != nil {
		t.Fatal(err)
	}
	sourceID, err := source.SourceIDFor(childSource)
	if err != nil {
		t.Fatal(err)
	}
	exact, err := artifact.NewExactIdentity(
		sourceID,
		artifact.ResolvedRef(""),
		artifact.ArtifactKindDirectory,
		testExactHash(name),
	)
	if err != nil {
		t.Fatal(err)
	}
	derivation, err := NewDirectResolutionDerivation(exact)
	if err != nil {
		t.Fatal(err)
	}
	declarationIdentity, err := set.DeclarationIdentity()
	if err != nil {
		t.Fatal(err)
	}
	correlation, err := NewSkillSetMemberCorrelation(declarationIdentity)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := NewExactSupplySubjectContract(ExactSupplySubjectInput{
		EntityID:                  entityID,
		SubjectID:                 mustResourceSubjectID(t, entityID),
		ExactSupply:               exact,
		Derivation:                derivation,
		SkillSetMemberCorrelation: &correlation,
	})
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func testLockedSection(t *testing.T, subjects ...LockedSubjectContract) LockedSection {
	t.Helper()
	section, err := NewLockedSection(subjects, nil)
	if err != nil {
		t.Fatal(err)
	}
	return section
}
