package refine

import (
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestSkillProjectionLoweringPreservesDistinctPlacementsAndMode(t *testing.T) {
	value := desiredtest.Skill(t, skill.Spec{
		Name: "oracle", Source: sourcetest.Local(t, "skills/oracle", source.LocalSourceModeVendor),
		Targets: []target.Target{target.TargetCodex, target.TargetClaudeCode},
		Scope:   target.ScopeProject, InstallMode: skill.InstallModeHardlink,
	})
	contracts, err := SkillPathProjections(value)
	if err != nil {
		t.Fatalf("SkillPathProjections returned error: %v", err)
	}
	if len(contracts) != 2 {
		t.Fatalf("contracts = %#v, want two distinct placements", contracts)
	}
	want := []struct {
		namespace   string
		destination string
		consumer    target.Target
	}{
		{namespace: "skill.project.agents", destination: ".agents/skills/oracle", consumer: target.TargetCodex},
		{namespace: "skill.project.claude", destination: ".claude/skills/oracle", consumer: target.TargetClaudeCode},
	}
	for index, contract := range contracts {
		spec, _ := contract.Realization()
		projection, _ := spec.ManagedPathProjection()
		if contract.SubjectID().Namespace() != want[index].namespace ||
			projection.Destination().String() != want[index].destination ||
			projection.PlacementMode() != realization.PathProjectionHardlink ||
			!reflect.DeepEqual(projection.ConsumerTargets(), []target.Target{want[index].consumer}) {
			t.Fatalf("contract[%d] = subject %q projection %#v", index, contract.SubjectID(), projection)
		}
	}
}

func TestSkillProjectionIdentitySurvivesConsumerAndInstallNameChanges(t *testing.T) {
	base := skill.Spec{
		Name: "oracle", Source: sourcetest.Local(t, "skills/oracle", source.LocalSourceModeVendor),
		Targets: []target.Target{target.TargetCodex, target.TargetAntigravityCLI},
		Scope:   target.ScopeProject, InstallMode: skill.InstallModeCopy,
	}
	shared := desiredtest.Skill(t, base)

	selectedOnlySpec := base
	selectedOnlySpec.Targets = []target.Target{target.TargetCodex}
	selectedOnly := desiredtest.Skill(t, selectedOnlySpec)

	renamedSpec := base
	renamedSpec.InstallName = "review"
	renamed := desiredtest.Skill(t, renamedSpec)

	sharedContracts, err := SkillPathProjections(shared)
	if err != nil {
		t.Fatal(err)
	}
	selectedContracts, err := SkillPathProjections(selectedOnly)
	if err != nil {
		t.Fatal(err)
	}
	renamedContracts, err := SkillPathProjections(renamed)
	if err != nil {
		t.Fatal(err)
	}
	if len(sharedContracts) != 1 || len(selectedContracts) != 1 || len(renamedContracts) != 1 {
		t.Fatalf("projection cardinalities = %d, %d, %d", len(sharedContracts), len(selectedContracts), len(renamedContracts))
	}
	wantSubject := sharedContracts[0].SubjectID()
	if selectedContracts[0].SubjectID() != wantSubject || renamedContracts[0].SubjectID() != wantSubject {
		t.Fatalf(
			"projection identity changed: shared=%q selected=%q renamed=%q",
			wantSubject,
			selectedContracts[0].SubjectID(),
			renamedContracts[0].SubjectID(),
		)
	}

	selectedRealization, _ := selectedContracts[0].Realization()
	selectedProjection, _ := selectedRealization.ManagedPathProjection()
	if !reflect.DeepEqual(selectedProjection.ConsumerTargets(), []target.Target{target.TargetCodex}) {
		t.Fatalf("selected consumers = %#v", selectedProjection.ConsumerTargets())
	}
	renamedRealization, _ := renamedContracts[0].Realization()
	renamedProjection, _ := renamedRealization.ManagedPathProjection()
	if renamedProjection.Destination().String() != ".agents/skills/review" {
		t.Fatalf("renamed destination = %q", renamedProjection.Destination())
	}
}

func TestSkillProjectionLoweringSelectsExplicitAdmittedRoot(t *testing.T) {
	placement, err := skill.NewTargetPlacement(target.ScopeProject, ".agents/skills")
	if err != nil {
		t.Fatal(err)
	}
	value := desiredtest.Skill(t, skill.Spec{
		Name: "oracle", Source: sourcetest.Local(t, "skills/oracle", source.LocalSourceModeVendor),
		Targets:    []target.Target{target.TargetOpenCode},
		Placements: map[target.Target]skill.TargetPlacement{target.TargetOpenCode: placement},
		Scope:      target.ScopeProject, InstallMode: skill.InstallModeCopy,
	})

	contracts, err := SkillPathProjections(value)
	if err != nil {
		t.Fatal(err)
	}
	if len(contracts) != 1 || contracts[0].SubjectID().Namespace() != "skill.project.agents" {
		t.Fatalf("contracts = %#v, want agents placement", contracts)
	}
	spec, _ := contracts[0].Realization()
	projection, _ := spec.ManagedPathProjection()
	if projection.Destination().String() != ".agents/skills/oracle" ||
		!reflect.DeepEqual(projection.ConsumerTargets(), []target.Target{target.TargetOpenCode}) {
		t.Fatalf("projection = %#v", projection)
	}
}

func TestSkillProjectionLoweringTreatsExplicitDefaultLikeOmission(t *testing.T) {
	base := skill.Spec{
		Name: "oracle", Source: sourcetest.Local(t, "skills/oracle", source.LocalSourceModeVendor),
		Targets: []target.Target{target.TargetOpenCode},
		Scope:   target.ScopeProject, InstallMode: skill.InstallModeCopy,
	}
	omitted := desiredtest.Skill(t, base)

	explicitDefault, err := skill.NewTargetPlacement(target.ScopeProject, ".opencode/skills")
	if err != nil {
		t.Fatal(err)
	}
	base.Placements = map[target.Target]skill.TargetPlacement{
		target.TargetOpenCode: explicitDefault,
	}
	explicit := desiredtest.Skill(t, base)

	omittedContracts, err := SkillPathProjections(omitted)
	if err != nil {
		t.Fatal(err)
	}
	explicitContracts, err := SkillPathProjections(explicit)
	if err != nil {
		t.Fatal(err)
	}
	if len(omittedContracts) != 1 || len(explicitContracts) != 1 ||
		!omittedContracts[0].Equal(explicitContracts[0]) {
		t.Fatalf("omitted=%#v explicit=%#v", omittedContracts, explicitContracts)
	}
}

func TestSkillProjectionLoweringCoalescesOnlySharedSelectedRoots(t *testing.T) {
	sharedRoot, err := skill.NewTargetPlacement(target.ScopeProject, ".agents/skills")
	if err != nil {
		t.Fatal(err)
	}
	shared := desiredtest.Skill(t, skill.Spec{
		Name: "oracle", Source: sourcetest.Local(t, "skills/oracle", source.LocalSourceModeVendor),
		Targets: []target.Target{target.TargetCodex, target.TargetOpenCode},
		Placements: map[target.Target]skill.TargetPlacement{
			target.TargetOpenCode: sharedRoot,
		},
		Scope: target.ScopeProject, InstallMode: skill.InstallModeCopy,
	})
	sharedContracts, err := SkillPathProjections(shared)
	if err != nil {
		t.Fatal(err)
	}
	if len(sharedContracts) != 1 {
		t.Fatalf("shared contracts = %#v", sharedContracts)
	}

	splitRoot, err := skill.NewTargetPlacement(target.ScopeProject, ".claude/skills")
	if err != nil {
		t.Fatal(err)
	}
	splitSpec := skill.Spec{
		Name: "oracle", Source: sourcetest.Local(t, "skills/oracle", source.LocalSourceModeVendor),
		Targets: []target.Target{target.TargetCodex, target.TargetOpenCode},
		Placements: map[target.Target]skill.TargetPlacement{
			target.TargetOpenCode: splitRoot,
		},
		Scope: target.ScopeProject, InstallMode: skill.InstallModeCopy,
	}
	splitContracts, err := SkillPathProjections(desiredtest.Skill(t, splitSpec))
	if err != nil {
		t.Fatal(err)
	}
	if len(splitContracts) != 2 {
		t.Fatalf("split contracts = %#v", splitContracts)
	}
}

func TestSkillProjectionLoweringRejectsUnadmittedExplicitRoot(t *testing.T) {
	unadmitted, err := skill.NewTargetPlacement(target.ScopeProject, ".pi/skills")
	if err != nil {
		t.Fatal(err)
	}
	value := desiredtest.Skill(t, skill.Spec{
		Name: "oracle", Source: sourcetest.Local(t, "skills/oracle", source.LocalSourceModeVendor),
		Targets:    []target.Target{target.TargetOpenCode},
		Placements: map[target.Target]skill.TargetPlacement{target.TargetOpenCode: unadmitted},
		Scope:      target.ScopeProject, InstallMode: skill.InstallModeCopy,
	})

	_, err = SkillPathProjections(value)
	if err == nil ||
		!strings.Contains(err.Error(), `placement ".pi/skills" is not admitted`) ||
		!strings.Contains(err.Error(), `.agents/skills, .claude/skills, .opencode/skills`) {
		t.Fatalf("SkillPathProjections error = %v", err)
	}
}
