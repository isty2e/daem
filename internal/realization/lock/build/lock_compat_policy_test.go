package build

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/supply/artifact"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestBuildWithoutCompatRepairStillRejectsIncompatibleSkill(t *testing.T) {
	tempDir := t.TempDir()
	skillPath := writeLowercaseSkillContent(t, tempDir, "skills/oracle", "---\ndescription: Use for oracle review.\n---\n")
	config := desired.Spec{
		Skills: []skill.Skill{
			desiredtest.Skill(t, skill.Spec{
				Name: "oracle", Source: sourcetest.Local(t, "skills/oracle", source.LocalSourceModeVendor),
				Targets: []target.Target{target.TargetOpenCode}, Scope: target.ScopeProject, InstallMode: skill.InstallModeCopy,
			}),
		},
	}

	_, err := buildWithTestOptions(context.Background(), lockEnvironment(t, config), stubResolver{
		artifacts: map[string]resolutionFixture{
			"local:skills/oracle?mode=vendor": {
				SourceID:    "local:skills/oracle?mode=vendor",
				ContentPath: skillPath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: contentHashForPath(t, skillPath),
			},
		},
	}, Options{})
	if err == nil {
		t.Fatal("Build returned nil error")
	}
	var guidance *skillrepair.GuidanceError
	if !errors.As(err, &guidance) || guidance.Classification().Repairability() != skillrepair.RepairabilityMechanical || !strings.Contains(strings.Join(guidance.Classification().Actions(), "; "), "rename file: skill.md -> SKILL.md") || !strings.Contains(err.Error(), "missing SKILL.md") {
		t.Fatalf("missing typed mechanical cause/action: %v", err)
	}
}

func TestBuildWithoutCompatRepairReportsSkillGroupMemberGuidance(t *testing.T) {
	tempDir := t.TempDir()
	skillPath := writeLowercaseSkillContent(t, tempDir, "skills/oracle", "---\ndescription: Use for oracle review.\n---\n")
	config := desired.Spec{
		SkillSets: []skill.SkillSet{
			desiredtest.SkillSet(t, skill.SkillSetSpec{
				Source:  sourcetest.Local(t, "skills", source.LocalSourceModeVendor),
				Include: []skill.Selector{desiredtest.Selector(t, "glob:oracle")},
				Targets: []target.Target{target.TargetOpenCode}, Scope: target.ScopeProject, InstallMode: skill.InstallModeCopy,
			}),
		},
	}

	_, err := buildWithTestOptions(context.Background(), lockEnvironment(t, config), &rootListingResolver{
		root: mustRootListing(
			t,
			sourcetest.Local(t, "skills", source.LocalSourceModeVendor),
			"",
			artifact.ArtifactKindDirectory,
			[]string{"oracle"},
		),
		artifacts: map[string]resolutionFixture{
			"local:skills/oracle?mode=vendor": {
				SourceID:    "local:skills/oracle?mode=vendor",
				ContentPath: skillPath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: contentHashForPath(t, skillPath),
			},
		},
	}, Options{})
	if err == nil {
		t.Fatal("Build returned nil error")
	}
	var guidance *skillrepair.GuidanceError
	if !errors.As(err, &guidance) || guidance.Classification().Repairability() != skillrepair.RepairabilityMechanical || len(guidance.Classification().Actions()) == 0 || !strings.Contains(err.Error(), `validate skill "oracle"`) {
		t.Fatalf("group member lost typed mechanical guidance: %v", err)
	}
}

func TestBuildWithoutCompatRepairReportsManualGuidance(t *testing.T) {
	tempDir := t.TempDir()
	skillPath := writeSkillContent(t, tempDir, "skills/oracle", "---\nname: oracle\n---\n")
	config := desired.Spec{
		Skills: []skill.Skill{
			desiredtest.Skill(t, skill.Spec{
				Name: "oracle", Source: sourcetest.Local(t, "skills/oracle", source.LocalSourceModeVendor),
				Targets: []target.Target{target.TargetOpenCode}, Scope: target.ScopeProject, InstallMode: skill.InstallModeCopy,
			}),
		},
	}

	_, err := buildWithTestOptions(context.Background(), lockEnvironment(t, config), stubResolver{
		artifacts: map[string]resolutionFixture{
			"local:skills/oracle?mode=vendor": {
				SourceID:    "local:skills/oracle?mode=vendor",
				ContentPath: skillPath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: contentHashForPath(t, skillPath),
			},
		},
	}, Options{})
	if err == nil {
		t.Fatal("Build returned nil error")
	}
	var guidance *skillrepair.GuidanceError
	if !errors.As(err, &guidance) || guidance.Classification().Repairability() != skillrepair.RepairabilityManual || len(guidance.Classification().Actions()) != 0 || !strings.Contains(err.Error(), "description is required") {
		t.Fatalf("manual failure gained mechanical repair or lost cause: %v", err)
	}
}

func TestBuildCompatRepairRejectsNonMechanicalSkill(t *testing.T) {
	tempDir := t.TempDir()
	skillPath := writeSkillContent(t, tempDir, "skills/oracle", "---\nname: oracle\n---\n")
	config := desired.Spec{
		Skills: []skill.Skill{
			desiredtest.Skill(t, skill.Spec{
				Name: "oracle", Source: sourcetest.Local(t, "skills/oracle", source.LocalSourceModeVendor),
				Targets: []target.Target{target.TargetOpenCode}, Scope: target.ScopeProject, InstallMode: skill.InstallModeCopy,
				CompatRepair: true,
			}),
		},
	}

	_, err := buildWithTestOptions(context.Background(), lockEnvironment(t, config), stubResolver{
		artifacts: map[string]resolutionFixture{
			"local:skills/oracle?mode=vendor": {
				SourceID:    "local:skills/oracle?mode=vendor",
				ContentPath: skillPath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: contentHashForPath(t, skillPath),
			},
		},
	}, Options{})
	if err == nil {
		t.Fatal("Build returned nil error")
	}
	if !strings.Contains(err.Error(), "manual skill compatibility repair required") ||
		!strings.Contains(err.Error(), "description is required") {
		t.Fatalf("error = %q, want manual description repair diagnostic", err)
	}
}

func TestBuildCompatRepairRejectsFileSkillSource(t *testing.T) {
	tempDir := t.TempDir()
	skillPath := writeFile(t, tempDir, "skills/oracle.md", "---\nname: oracle\ndescription: Use for oracle review.\n---\n")
	config := desired.Spec{
		Skills: []skill.Skill{
			desiredtest.Skill(t, skill.Spec{
				Name: "oracle", Source: sourcetest.Local(t, "skills/oracle.md", source.LocalSourceModeVendor),
				Targets: []target.Target{target.TargetOpenCode}, Scope: target.ScopeProject, InstallMode: skill.InstallModeCopy,
				CompatRepair: true,
			}),
		},
	}

	_, err := buildWithTestOptions(context.Background(), lockEnvironment(t, config), stubResolver{
		artifacts: map[string]resolutionFixture{
			"local:skills/oracle.md?mode=vendor": {
				SourceID:    "local:skills/oracle.md?mode=vendor",
				ContentPath: skillPath,
				Kind:        artifact.ArtifactKindFile,
				ContentHash: contentHashForPath(t, skillPath),
			},
		},
	}, Options{})
	if err == nil {
		t.Fatal("Build returned nil error")
	}
	if !strings.Contains(err.Error(), `repair skill "oracle"`) ||
		!strings.Contains(err.Error(), "skill source must resolve to a directory") {
		t.Fatalf("error = %q, want file source repair rejection", err)
	}
}
