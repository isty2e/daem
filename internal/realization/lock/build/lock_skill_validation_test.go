package build

import (
	"context"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestBuildRejectsInvalidSkillArtifact(t *testing.T) {
	tempDir := t.TempDir()
	artifactPath := writeFile(t, tempDir, "hooks/not-a-skill.py", "print('not a skill')\n")
	config := desired.Spec{
		Skills: []skill.Skill{
			projectCopySkill(t, "bad-skill", sourcetest.Local(t, "hooks/not-a-skill.py", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}, false),
		},
	}

	_, err := buildWithTestOptions(context.Background(), lockEnvironment(t, config), stubResolver{
		artifacts: map[string]resolutionFixture{
			"local:hooks/not-a-skill.py?mode=vendor": {
				SourceID:    "local:hooks/not-a-skill.py?mode=vendor",
				ContentPath: artifactPath,
				Kind:        artifact.ArtifactKindFile,
				ContentHash: "sha256:script",
			},
		},
	}, Options{})
	if err == nil {
		t.Fatal("Build returned nil error")
	}

	if !strings.Contains(err.Error(), `validate skill "bad-skill"`) {
		t.Fatalf("error = %q, want skill validation context", err)
	}
}

func TestBuildRejectsOpenCodeSkillFrontmatterNameMismatch(t *testing.T) {
	tempDir := t.TempDir()
	skillPath := writeSkillContent(t, tempDir, "skills/oracle", "---\nname: other\ndescription: Use for oracle review.\n---\n")
	config := desired.Spec{
		Skills: []skill.Skill{
			projectCopySkill(t, "oracle", sourcetest.Local(t, "skills/oracle", source.LocalSourceModeVendor), []target.Target{target.TargetOpenCode}, false),
		},
	}

	_, err := buildWithTestOptions(context.Background(), lockEnvironment(t, config), stubResolver{
		artifacts: map[string]resolutionFixture{
			"local:skills/oracle?mode=vendor": {
				SourceID:    "local:skills/oracle?mode=vendor",
				ContentPath: skillPath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: "sha256:oracle",
			},
		},
	}, Options{})
	if err == nil {
		t.Fatal("Build returned nil error")
	}
	if !strings.Contains(err.Error(), `target "opencode"`) || !strings.Contains(err.Error(), `name "other" must match skill name "oracle"`) {
		t.Fatalf("error = %q, want opencode name mismatch diagnostic", err)
	}
}

func TestBuildAllowsPiSkillFrontmatterNameMismatch(t *testing.T) {
	tempDir := t.TempDir()
	skillPath := writeSkillContent(t, tempDir, "skills/oracle", "---\nname: portable-skill\ndescription: Use for oracle review.\n---\n")
	config := desired.Spec{
		Skills: []skill.Skill{
			projectCopySkill(t, "oracle", sourcetest.Local(t, "skills/oracle", source.LocalSourceModeVendor), []target.Target{target.TargetPi}, false),
		},
	}

	lockfile, err := buildWithTestOptions(context.Background(), lockEnvironment(t, config), stubResolver{
		artifacts: map[string]resolutionFixture{
			"local:skills/oracle?mode=vendor": {
				SourceID:    "local:skills/oracle?mode=vendor",
				ContentPath: skillPath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: "sha256:oracle",
			},
		},
	}, Options{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	lockedSkills := lockedExactSupplySubjectsOfKind(lockfile, entity.KindSkill)
	if len(lockedSkills) != 1 || lockedSkills[0].EntityID().Name() != "oracle" {
		t.Fatalf("Locked skills = %#v, want oracle", lockedSkills)
	}
}

func TestBuildAllowsAntigravityCLISkillFrontmatterNameMismatch(t *testing.T) {
	tempDir := t.TempDir()
	skillPath := writeSkillContent(t, tempDir, "skills/antigravity_guide", "---\nname: antigravity-guide\ndescription: Use for Antigravity guidance.\n---\n")
	config := desired.Spec{
		Skills: []skill.Skill{
			projectCopySkill(t, "antigravity_guide", sourcetest.Local(t, "skills/antigravity_guide", source.LocalSourceModeVendor), []target.Target{target.TargetAntigravityCLI}, false),
		},
	}

	lockfile, err := buildWithTestOptions(context.Background(), lockEnvironment(t, config), stubResolver{
		artifacts: map[string]resolutionFixture{
			"local:skills/antigravity_guide?mode=vendor": {
				SourceID:    "local:skills/antigravity_guide?mode=vendor",
				ContentPath: skillPath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: "sha256:antigravity-guide",
			},
		},
	}, Options{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	lockedSkills := lockedExactSupplySubjectsOfKind(lockfile, entity.KindSkill)
	if len(lockedSkills) != 1 || lockedSkills[0].EntityID().Name() != "antigravity_guide" {
		t.Fatalf("Locked skills = %#v, want antigravity_guide", lockedSkills)
	}
}
