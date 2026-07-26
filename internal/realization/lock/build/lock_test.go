package build

import (
	"context"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/instructions"
	"github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestBuildLocksLocalSkillsAndInstructions(t *testing.T) {
	tempDir := t.TempDir()
	alphaSkillPath := writeSkill(t, tempDir, "skills/alpha")
	zetaSkillPath := writeSkill(t, tempDir, "skills/zeta")
	instructionsPath := writeFile(t, tempDir, "instructions/AGENTS.md", "project instructions\n")

	config := desired.Spec{
		Skills: []skill.Skill{
			projectCopySkill(t, "zeta", sourcetest.Local(t, "skills/zeta", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}, false),
			projectCopySkill(t, "alpha", sourcetest.Local(t, "skills/alpha", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}, false),
		},
		Instructions: []instructions.Instructions{
			desiredtest.Instructions(t, instructions.Spec{
				Name: "project", Source: sourcetest.Local(t, "instructions/AGENTS.md", source.LocalSourceModeVendor),
				Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject,
			}),
		},
	}

	lockfile, err := buildWithTestOptions(context.Background(), lockEnvironment(t, config), stubResolver{
		artifacts: map[string]resolutionFixture{
			"local:skills/zeta?mode=vendor": {
				SourceID:    "local:skills/zeta?mode=vendor",
				ContentPath: zetaSkillPath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: "sha256:zeta",
			},
			"local:skills/alpha?mode=vendor": {
				SourceID:    "local:skills/alpha?mode=vendor",
				ContentPath: alphaSkillPath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: "sha256:alpha",
			},
			"local:instructions/AGENTS.md?mode=vendor": {
				SourceID:    "local:instructions/AGENTS.md?mode=vendor",
				ContentPath: instructionsPath,
				Kind:        artifact.ArtifactKindFile,
				ContentHash: "sha256:instructions",
			},
		},
	}, Options{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if lockfile.Version != lock.CurrentVersion {
		t.Fatalf("Version = %d, want %d", lockfile.Version, lock.CurrentVersion)
	}

	lockedSkills := lockedExactSupplySubjectsOfKind(lockfile, entity.KindSkill)
	if len(lockedSkills) != 2 {
		t.Fatalf("len(locked skills) = %d, want 2", len(lockedSkills))
	}

	if lockedSkills[0].EntityID().Name() != "alpha" || lockedSkills[1].EntityID().Name() != "zeta" {
		t.Fatalf("skills not sorted by name: %#v", lockedSkills)
	}
	for _, lockedSkill := range lockedSkills {
		if _, correlated := lockedSkill.SkillSetMemberCorrelation(); correlated {
			t.Fatalf("direct skill has selector correlation: %#v", lockedSkill)
		}
	}

	if hooks := lockedSubjectsOfKind(lockfile, entity.KindHook); len(hooks) != 0 {
		t.Fatalf("len(locked hooks) = %d, want 0", len(hooks))
	}

	lockedInstructions := lockedSubjectsOfKind(lockfile, entity.KindInstructions)
	if len(lockedInstructions) != 2 {
		t.Fatalf("len(locked instructions) = %d, want supply and projection", len(lockedInstructions))
	}

	realized := 0
	for _, lockedInstruction := range lockedInstructions {
		if lockedInstruction.EntityID().Name() != "project" {
			t.Fatalf("locked instructions name = %q", lockedInstruction.EntityID().Name())
		}
		if _, correlated := lockedInstruction.SkillSetMemberCorrelation(); correlated {
			t.Fatalf("instructions unexpectedly have skill-set correlation: %#v", lockedInstruction)
		}
		if _, ok := lockedInstruction.Realization(); ok {
			realized++
		}
	}
	if realized != 1 {
		t.Fatalf("realized Instructions subjects = %d, want 1", realized)
	}
}

func TestBuildPreservesResolvedRef(t *testing.T) {
	tempDir := t.TempDir()
	skillPath := writeSkill(t, tempDir, "skills/demo")
	resolvedRef := strings.Repeat("a", 40)
	sourceSpec := mustGitSource(t, "https://example.test/demo.git", "skills/demo", "main")
	sourceID := mustSourceID(t, sourceSpec)
	config := desired.Spec{
		Skills: []skill.Skill{
			projectCopySkill(t, "demo", sourceSpec, []target.Target{target.TargetCodex}, false),
		},
	}

	lockfile, err := buildWithTestOptions(context.Background(), lockEnvironment(t, config), stubResolver{
		artifacts: map[string]resolutionFixture{
			sourceID: {
				SourceID:    artifact.SourceID(sourceID),
				ContentPath: skillPath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: "sha256:demo",
				ResolvedRef: artifact.ResolvedRef(resolvedRef),
			},
		},
	}, Options{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	identity := mustExactSupply(t, mustLockedSubject(t, lockfile, entity.KindSkill, "demo"))
	if identity.ResolvedRef() != artifact.ResolvedRef(resolvedRef) {
		t.Fatalf("ResolvedRef = %q, want %q", identity.ResolvedRef(), resolvedRef)
	}
}

func TestBuildPinsSelectorGitChildrenToResolvedRootRef(t *testing.T) {
	tempDir := t.TempDir()
	alphaPath := writeSkill(t, tempDir, "repo-root/alpha")
	betaPath := writeSkill(t, tempDir, "repo-root/beta")
	resolvedRef := strings.Repeat("a", 40)
	rootSource := mustGitSource(t, "https://example.test/skills.git", ".", "main")
	alphaSourceID := mustSourceID(t, mustGitSource(t, "https://example.test/skills.git", "alpha", resolvedRef))
	betaSourceID := mustSourceID(t, mustGitSource(t, "https://example.test/skills.git", "beta", resolvedRef))
	config := desired.Spec{
		SkillSets: []skill.SkillSet{
			projectCopySkillSet(t, rootSource, []skill.Selector{desiredtest.Selector(t, "glob:*")}, []target.Target{target.TargetCodex}, false),
		},
	}

	lockfile, err := buildWithTestOptions(context.Background(), lockEnvironment(t, config), &rootListingResolver{
		root: mustRootListing(
			t,
			rootSource,
			artifact.ResolvedRef(resolvedRef),
			artifact.ArtifactKindDirectory,
			[]string{"alpha", "beta"},
		),
		artifacts: map[string]resolutionFixture{
			alphaSourceID: {
				SourceID:    artifact.SourceID(alphaSourceID),
				ContentPath: alphaPath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: "sha256:alpha-a",
				ResolvedRef: artifact.ResolvedRef(resolvedRef),
			},
			betaSourceID: {
				SourceID:    artifact.SourceID(betaSourceID),
				ContentPath: betaPath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: "sha256:beta-a",
				ResolvedRef: artifact.ResolvedRef(resolvedRef),
			},
		},
	}, Options{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	lockedSkills := lockedExactSupplySubjectsOfKind(lockfile, entity.KindSkill)
	if len(lockedSkills) != 2 {
		t.Fatalf("len(locked skills) = %d, want 2", len(lockedSkills))
	}
	for _, lockedSkill := range lockedSkills {
		identity := mustExactSupply(t, lockedSkill)
		name := lockedSkill.EntityID().Name()
		if identity.ResolvedRef() != artifact.ResolvedRef(resolvedRef) {
			t.Fatalf("locked skill %q resolved_ref = %q, want %q", name, identity.ResolvedRef(), resolvedRef)
		}
		expectedSourceID := map[string]string{"alpha": alphaSourceID, "beta": betaSourceID}[name]
		if identity.SourceID() != artifact.SourceID(expectedSourceID) {
			t.Fatalf("locked skill %q source_id = %q, want %q", name, identity.SourceID(), expectedSourceID)
		}
		if _, correlated := lockedSkill.SkillSetMemberCorrelation(); !correlated {
			t.Fatalf("locked skill %q is missing selector declaration correlation", name)
		}
	}
}

func TestBuildListsSelectorRootWithoutResolvingUnselectedChildren(t *testing.T) {
	tempDir := t.TempDir()
	alphaPath := writeSkill(t, tempDir, "skills/alpha")
	config := desired.Spec{
		SkillSets: []skill.SkillSet{
			projectCopySkillSet(
				t, sourcetest.Local(t, "skills", source.LocalSourceModeVendor),
				[]skill.Selector{desiredtest.Selector(t, "glob:alpha")},
				[]target.Target{target.TargetCodex}, false,
			),
		},
	}
	resolver := &rootListingResolver{
		root: mustRootListing(
			t,
			sourcetest.Local(t, "skills", source.LocalSourceModeVendor),
			"",
			artifact.ArtifactKindDirectory,
			[]string{"alpha", "beta"},
		),
		artifacts: map[string]resolutionFixture{
			"local:skills/alpha?mode=vendor": {
				SourceID:    "local:skills/alpha?mode=vendor",
				ContentPath: alphaPath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: "sha256:alpha",
			},
		},
	}

	lockfile, err := buildWithTestOptions(context.Background(), lockEnvironment(t, config), resolver, Options{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	lockedSkills := lockedExactSupplySubjectsOfKind(lockfile, entity.KindSkill)
	if len(lockedSkills) != 1 || lockedSkills[0].EntityID().Name() != "alpha" {
		t.Fatalf("locked skills = %#v, want only alpha", lockedSkills)
	}
	if _, correlated := lockedSkills[0].SkillSetMemberCorrelation(); !correlated {
		t.Fatalf("locked skill is missing selector declaration correlation: %#v", lockedSkills[0])
	}
	if len(resolver.resolved) != 1 || resolver.resolved[0] != "local:skills/alpha?mode=vendor" {
		t.Fatalf("resolved source IDs = %#v, want only selected alpha child", resolver.resolved)
	}
}

func TestBuildUsesSkillResourceIDAsLockName(t *testing.T) {
	tempDir := t.TempDir()
	skillPath := writeSkill(t, tempDir, "skills/review")
	config := desired.Spec{
		Skills: []skill.Skill{
			desiredtest.Skill(t, skill.Spec{
				Name: "codex_global_review", InstallName: "review",
				Source:  sourcetest.Local(t, "skills/review", source.LocalSourceModeVendor),
				Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject, InstallMode: skill.InstallModeCopy,
			}),
		},
	}

	lockfile, err := buildWithTestOptions(context.Background(), lockEnvironment(t, config), stubResolver{
		artifacts: map[string]resolutionFixture{
			"local:skills/review?mode=vendor": {
				SourceID:    "local:skills/review?mode=vendor",
				ContentPath: skillPath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: "sha256:review",
			},
		},
	}, Options{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	lockedSkills := lockedExactSupplySubjectsOfKind(lockfile, entity.KindSkill)
	if len(lockedSkills) != 1 || lockedSkills[0].EntityID().Name() != "codex_global_review" {
		t.Fatalf("locked skills = %#v, want resource id lock name", lockedSkills)
	}
}

func TestBuildLocksS3Sources(t *testing.T) {
	tempDir := t.TempDir()
	skillPath := writeSkill(t, tempDir, "skills/oracle")
	instructionsPath := writeFile(t, tempDir, "instructions/AGENTS.md", "project instructions\n")

	config := desired.Spec{
		Skills: []skill.Skill{
			projectCopySkill(
				t, "oracle",
				sourcetest.S3(t, "s3://daem/skills/oracle.tar.gz", "", "us-east-1", source.S3ObjectFormatTarGzip),
				[]target.Target{target.TargetCodex}, false,
			),
		},
		Instructions: []instructions.Instructions{
			desiredtest.Instructions(t, instructions.Spec{
				Name: "project", Source: sourcetest.S3(t, "s3://daem/instructions/AGENTS.md", "", "", source.S3ObjectFormatFile),
				Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject,
			}),
		},
	}

	lockfile, err := buildWithTestOptions(context.Background(), lockEnvironment(t, config), stubResolver{
		artifacts: map[string]resolutionFixture{
			"s3:s3://daem/skills/oracle.tar.gz?format=tar.gz&region=us-east-1": {
				SourceID:    "s3:s3://daem/skills/oracle.tar.gz?format=tar.gz&region=us-east-1",
				ContentPath: skillPath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: "sha256:oracle",
				ResolvedRef: "skill-version",
			},
			"s3:s3://daem/instructions/AGENTS.md": {
				SourceID:    "s3:s3://daem/instructions/AGENTS.md",
				ContentPath: instructionsPath,
				Kind:        artifact.ArtifactKindFile,
				ContentHash: "sha256:instructions",
				ResolvedRef: "instructions-version",
			},
		},
	}, Options{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	skillIdentity := mustExactSupply(t, mustLockedSubject(t, lockfile, entity.KindSkill, "oracle"))
	if skillIdentity.SourceID() != "s3:s3://daem/skills/oracle.tar.gz?format=tar.gz&region=us-east-1" {
		t.Fatalf("skill source_id = %q", skillIdentity.SourceID())
	}
	if skillIdentity.ResolvedRef() != "skill-version" {
		t.Fatalf("skill resolved_ref = %q", skillIdentity.ResolvedRef())
	}
	instructionsIdentity := mustExactSupply(t, mustLockedSubject(t, lockfile, entity.KindInstructions, "project"))
	if instructionsIdentity.SourceID() != "s3:s3://daem/instructions/AGENTS.md" {
		t.Fatalf("instructions source_id = %q", instructionsIdentity.SourceID())
	}
	if instructionsIdentity.ResolvedRef() != "instructions-version" {
		t.Fatalf("instructions resolved_ref = %q", instructionsIdentity.ResolvedRef())
	}
}
