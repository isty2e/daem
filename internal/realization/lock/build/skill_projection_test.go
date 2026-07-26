package build

import (
	"context"
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestBuildCoalescesSharedSkillPlacementIntoOneLockedProjection(t *testing.T) {
	tempDir := t.TempDir()
	skillPath := writeSkill(t, tempDir, "skills/oracle")
	value := desiredtest.Skill(t, skill.Spec{
		Name: "oracle", Source: sourcetest.Local(t, "skills/oracle", source.LocalSourceModeVendor),
		Targets: []target.Target{target.TargetCodex, target.TargetAntigravityCLI},
		Scope:   target.ScopeProject, InstallMode: skill.InstallModeCopy,
	})

	environment := lockEnvironment(t, desired.Spec{Skills: []skill.Skill{value}})
	locked, err := buildWithTestOptions(context.Background(), environment, stubResolver{
		artifacts: map[string]resolutionFixture{
			"local:skills/oracle?mode=vendor": {
				SourceID: "local:skills/oracle?mode=vendor", ContentPath: skillPath,
				Kind: artifact.ArtifactKindDirectory, ContentHash: "sha256:oracle",
			},
		},
	}, Options{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if got := lockedExactSupplySubjectsOfKind(locked, entity.KindSkill); len(got) != 1 {
		t.Fatalf("exact Supply subjects = %#v, want one", got)
	}
	projections := lockedPathProjectionSubjectsOfKind(locked, entity.KindSkill)
	if len(projections) != 1 {
		t.Fatalf("path projection subjects = %#v, want one", projections)
	}
	expected, err := refine.ExpectedManagedPaths(environment, locked.Locked)
	if err != nil {
		t.Fatalf("ExpectedManagedPaths returned error: %v", err)
	}
	if !reflect.DeepEqual(expected, projections) {
		t.Fatalf("expected managed paths = %#v, locked projections %#v", expected, projections)
	}
	realization, _ := projections[0].Realization()
	projection, _ := realization.ManagedPathProjection()
	wantTargets := []target.Target{target.TargetAntigravityCLI, target.TargetCodex}
	if projections[0].SubjectID().Namespace() != "skill.project.agents" ||
		projection.Destination() != ".agents/skills/oracle" ||
		!reflect.DeepEqual(projection.ConsumerTargets(), wantTargets) {
		t.Fatalf("projection = subject %q body %#v consumers %#v", projections[0].SubjectID(), projection, projection.ConsumerTargets())
	}
	if _, supplied := projections[0].ExactSupply(); supplied {
		t.Fatal("path projection duplicated exact Supply authority")
	}
}
