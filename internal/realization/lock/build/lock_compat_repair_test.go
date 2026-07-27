package build

import (
	"context"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/supply/artifact"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestBuildCompatRepairKeepsCompatibleSkillAsDirectResolution(t *testing.T) {
	tempDir := t.TempDir()
	skillPath := writeSkillContent(
		t,
		tempDir,
		"skills/oracle",
		"---\nname: oracle\ndescription: Use for oracle review.\n---\n",
	)
	originalHash := contentHashForPath(t, skillPath)
	config := desired.Spec{
		Skills: []skill.Skill{
			desiredtest.Skill(t, skill.Spec{
				Name: "oracle", Source: sourcetest.Local(t, "skills/oracle", source.LocalSourceModeVendor),
				Targets: []target.Target{
					target.TargetCodex,
					target.TargetClaudeCode,
					target.TargetOpenCode,
					target.TargetPi,
					target.TargetAntigravityCLI,
				},
				Scope: target.ScopeProject, InstallMode: skill.InstallModeCopy,
				CompatRepair: true,
			}),
		},
	}

	locked, err := buildWithTestOptions(context.Background(), lockEnvironment(t, config), stubResolver{
		artifacts: map[string]resolutionFixture{
			"local:skills/oracle?mode=vendor": {
				SourceID:    "local:skills/oracle?mode=vendor",
				ContentPath: skillPath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: originalHash,
			},
		},
	}, Options{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	subjects := lockedExactSupplySubjectsOfKind(locked, entity.KindSkill)
	if len(subjects) != 1 {
		t.Fatalf("locked subjects = %#v, want one compatible skill", subjects)
	}
	identity, ok := subjects[0].ExactSupply()
	if !ok || identity.ContentHash() != originalHash {
		t.Fatalf("locked identity = %#v, want original content hash %q", identity, originalHash)
	}
	if _, present := subjects[0].RepairRecipe(); present {
		t.Fatal("compatible skill unexpectedly has a repair recipe")
	}
	derivation, ok := subjects[0].Derivation()
	if !ok || derivation.Kind() != lock.DerivationDirectResolution {
		t.Fatalf("derivation = %#v, want direct resolution", derivation)
	}
	if contentHashForPath(t, skillPath) != originalHash {
		t.Fatal("Build mutated the compatible source")
	}
}

func TestBuildCompatRepairHandlesMixedSkillSetOutcomes(t *testing.T) {
	tempDir := t.TempDir()
	compatiblePath := writeSkillContent(
		t,
		tempDir,
		"skills/compatible",
		"---\nname: compatible\ndescription: Already compatible.\n---\n",
	)
	repairablePath := writeLowercaseSkillContent(
		t,
		tempDir,
		"skills/repairable",
		"---\ndescription: Needs mechanical repair.\n---\n",
	)
	compatibleHash := contentHashForPath(t, compatiblePath)
	repairableHash := contentHashForPath(t, repairablePath)
	config := desired.Spec{
		SkillSets: []skill.SkillSet{
			desiredtest.SkillSet(t, skill.SkillSetSpec{
				Source:  sourcetest.Local(t, "skills", source.LocalSourceModeVendor),
				Include: []skill.Selector{desiredtest.Selector(t, "glob:*")},
				Targets: []target.Target{target.TargetOpenCode}, Scope: target.ScopeProject, InstallMode: skill.InstallModeCopy,
				CompatRepair: true,
			}),
		},
	}

	locked, err := buildWithTestOptions(context.Background(), lockEnvironment(t, config), &rootListingResolver{
		root: mustRootListing(
			t,
			sourcetest.Local(t, "skills", source.LocalSourceModeVendor),
			"",
			artifact.ArtifactKindDirectory,
			[]string{"compatible", "repairable"},
		),
		artifacts: map[string]resolutionFixture{
			"local:skills/compatible?mode=vendor": {
				SourceID:    "local:skills/compatible?mode=vendor",
				ContentPath: compatiblePath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: compatibleHash,
			},
			"local:skills/repairable?mode=vendor": {
				SourceID:    "local:skills/repairable?mode=vendor",
				ContentPath: repairablePath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: repairableHash,
			},
		},
	}, Options{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	subjects := lockedExactSupplySubjectsOfKind(locked, entity.KindSkill)
	if len(subjects) != 2 {
		t.Fatalf("locked subjects = %#v, want two skill group members", subjects)
	}
	for _, subject := range subjects {
		identity, ok := subject.ExactSupply()
		if !ok {
			t.Fatalf("skill %q is missing exact Supply", subject.EntityID().Name())
		}
		derivation, ok := subject.Derivation()
		if !ok {
			t.Fatalf("skill %q is missing derivation", subject.EntityID().Name())
		}
		if _, correlated := subject.SkillSetMemberCorrelation(); !correlated {
			t.Fatalf("skill %q is missing skill set correlation", subject.EntityID().Name())
		}

		switch subject.EntityID().Name() {
		case "compatible":
			if identity.ContentHash() != compatibleHash {
				t.Fatalf("compatible hash = %q, want %q", identity.ContentHash(), compatibleHash)
			}
			if derivation.Kind() != lock.DerivationDirectResolution {
				t.Fatalf("compatible derivation = %q, want direct resolution", derivation.Kind())
			}
			if _, present := subject.RepairRecipe(); present {
				t.Fatal("compatible group member unexpectedly has a repair recipe")
			}
		case "repairable":
			if identity.ContentHash() == repairableHash {
				t.Fatalf("repairable hash = %q, want repaired output", identity.ContentHash())
			}
			if derivation.Kind() != lock.DerivationDeterministicTransform {
				t.Fatalf("repairable derivation = %q, want deterministic transform", derivation.Kind())
			}
			if _, present := subject.RepairRecipe(); !present {
				t.Fatal("repairable group member is missing a repair recipe")
			}
		default:
			t.Fatalf("unexpected skill group member %q", subject.EntityID().Name())
		}
	}
	if contentHashForPath(t, compatiblePath) != compatibleHash {
		t.Fatal("Build mutated the compatible group member source")
	}
	if contentHashForPath(t, repairablePath) != repairableHash {
		t.Fatal("Build mutated the repairable group member source")
	}
}

func TestBuildRepairsManifestDeclaredSkillDuringLock(t *testing.T) {
	tempDir := t.TempDir()
	skillPath := writeLowercaseSkillContent(t, tempDir, "skills/oracle", "---\ndescription: Use for oracle review.\n---\n")
	originalHash := contentHashForPath(t, skillPath)
	config := desired.Spec{
		Skills: []skill.Skill{
			desiredtest.Skill(t, skill.Spec{
				Name: "oracle", Source: sourcetest.Local(t, "skills/oracle", source.LocalSourceModeVendor),
				Targets: []target.Target{target.TargetOpenCode}, Scope: target.ScopeProject, InstallMode: skill.InstallModeCopy,
				CompatRepair: true,
			}),
		},
	}

	locked, err := buildWithTestOptions(context.Background(), lockEnvironment(t, config), stubResolver{
		artifacts: map[string]resolutionFixture{
			"local:skills/oracle?mode=vendor": {
				SourceID:    "local:skills/oracle?mode=vendor",
				ContentPath: skillPath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: originalHash,
			},
		},
	}, Options{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	lockedSkill, identity, recipe := onlyRepairedSkillContract(t, locked, "oracle")
	if identity.SourceID() != "local:skills/oracle?mode=vendor" {
		t.Fatalf("SourceID = %q, want original source id", identity.SourceID())
	}
	if identity.ContentHash() == originalHash {
		t.Fatalf("ContentHash = %q, want repaired hash different from original", identity.ContentHash())
	}
	if recipe.Input().ContentHash() != originalHash {
		t.Fatalf("original ContentHash = %q, want %q", recipe.Input().ContentHash(), originalHash)
	}
	assertRepairOperationKinds(t, recipe.Operations(), []string{"rename", "set_frontmatter_string"})
	if _, correlated := lockedSkill.SkillSetMemberCorrelation(); correlated {
		t.Fatal("direct skill unexpectedly has skill set correlation")
	}
	assertDirectoryEntryMissingExact(t, skillPath, "SKILL.md")
	assertDirectoryEntryExistsExact(t, skillPath, "skill.md")
}

func TestBuildRepairsGitRootSkillDuringLock(t *testing.T) {
	tempDir := t.TempDir()
	skillPath := writeLowercaseSkillContent(t, tempDir, "repo", "---\ndescription: Humanize text.\n---\n")
	originalHash := contentHashForPath(t, skillPath)
	gitSource := mustGitSource(t, "https://github.com/blader/humanizer.git", ".", "main")
	sourceID, err := source.SourceIDFor(gitSource)
	if err != nil {
		t.Fatalf("SourceIDFor returned error: %v", err)
	}
	config := desired.Spec{
		Skills: []skill.Skill{
			desiredtest.Skill(t, skill.Spec{
				Name: "humanizer", Source: gitSource,
				Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject, InstallMode: skill.InstallModeCopy,
				CompatRepair: true,
			}),
		},
	}

	locked, err := buildWithTestOptions(context.Background(), lockEnvironment(t, config), stubResolver{
		artifacts: map[string]resolutionFixture{
			string(sourceID): {
				SourceID:    sourceID,
				ContentPath: skillPath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: originalHash,
				ResolvedRef: artifact.ResolvedRef(strings.Repeat("a", 40)),
			},
		},
	}, Options{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	_, identity, recipe := onlyRepairedSkillContract(t, locked, "humanizer")
	if identity.SourceID() != sourceID {
		t.Fatalf("locked identity = %#v, want root git humanizer source", identity)
	}
	if recipe.Input().SourceID() != sourceID {
		t.Fatalf("OriginalSourceID = %q, want %q", recipe.Input().SourceID(), sourceID)
	}
	if recipe.Input().ResolvedRef() != artifact.ResolvedRef(strings.Repeat("a", 40)) {
		t.Fatalf("OriginalResolvedRef = %q, want full commit", recipe.Input().ResolvedRef())
	}
	assertRepairOperationKinds(t, recipe.Operations(), []string{"rename", "set_frontmatter_string"})
	assertDirectoryEntryMissingExact(t, skillPath, "SKILL.md")
	assertDirectoryEntryExistsExact(t, skillPath, "skill.md")
}

func TestBuildRepairsStrictNameMismatchDuringLock(t *testing.T) {
	tempDir := t.TempDir()
	skillPath := writeSkillContent(t, tempDir, "skills/oracle", "---\nname: other\ndescription: Use for oracle review.\n---\n")
	originalHash := contentHashForPath(t, skillPath)
	config := desired.Spec{
		Skills: []skill.Skill{
			desiredtest.Skill(t, skill.Spec{
				Name: "oracle", Source: sourcetest.Local(t, "skills/oracle", source.LocalSourceModeVendor),
				Targets: []target.Target{target.TargetOpenCode}, Scope: target.ScopeProject, InstallMode: skill.InstallModeCopy,
				CompatRepair: true,
			}),
		},
	}

	locked, err := buildWithTestOptions(context.Background(), lockEnvironment(t, config), stubResolver{
		artifacts: map[string]resolutionFixture{
			"local:skills/oracle?mode=vendor": {
				SourceID:    "local:skills/oracle?mode=vendor",
				ContentPath: skillPath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: originalHash,
			},
		},
	}, Options{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	_, _, recipe := onlyRepairedSkillContract(t, locked, "oracle")
	assertRepairOperationKinds(t, recipe.Operations(), []string{"set_frontmatter_string"})
	if recipe.Input().ContentHash() != originalHash {
		t.Fatalf("OriginalContentHash = %q, want %q", recipe.Input().ContentHash(), originalHash)
	}
	if contentHashForPath(t, skillPath) != originalHash {
		t.Fatal("Build mutated the original strict-name mismatch source")
	}
}

func TestBuildRepairsSkillGroupMemberDuringLock(t *testing.T) {
	tempDir := t.TempDir()
	skillPath := writeLowercaseSkillContent(t, tempDir, "skills/oracle", "---\ndescription: Use for oracle review.\n---\n")
	skillHash := contentHashForPath(t, skillPath)
	config := desired.Spec{
		SkillSets: []skill.SkillSet{
			desiredtest.SkillSet(t, skill.SkillSetSpec{
				Source:  sourcetest.Local(t, "skills", source.LocalSourceModeVendor),
				Include: []skill.Selector{desiredtest.Selector(t, "glob:oracle")},
				Targets: []target.Target{target.TargetOpenCode}, Scope: target.ScopeProject, InstallMode: skill.InstallModeCopy,
				CompatRepair: true,
			}),
		},
	}

	locked, err := buildWithTestOptions(context.Background(), lockEnvironment(t, config), &rootListingResolver{
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
				ContentHash: skillHash,
			},
		},
	}, Options{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	lockedSkill, _, recipe := onlyRepairedSkillContract(t, locked, "oracle")
	assertRepairOperationKinds(t, recipe.Operations(), []string{"rename", "set_frontmatter_string"})
	if _, correlated := lockedSkill.SkillSetMemberCorrelation(); !correlated {
		t.Fatal("group member is missing skill set correlation")
	}
}

func onlyRepairedSkillContract(
	t *testing.T,
	locked lock.File,
	name string,
) (lock.LockedSubjectContract, artifact.ExactIdentity, skillrepair.Recipe) {
	t.Helper()
	subjects := lockedExactSupplySubjectsOfKind(locked, entity.KindSkill)
	if len(subjects) != 1 {
		t.Fatalf("locked subjects = %#v, want one repaired skill", subjects)
	}
	contract := subjects[0]
	if contract.EntityID().Kind() != entity.KindSkill || contract.EntityID().Name() != name {
		t.Fatalf("locked entity = %q, want skill %q", contract.EntityID(), name)
	}
	identity, ok := contract.ExactSupply()
	if !ok {
		t.Fatal("repaired skill is missing exact Supply identity")
	}
	recipe, ok := contract.RepairRecipe()
	if !ok {
		t.Fatal("repaired skill is missing repair recipe")
	}
	return contract, identity, recipe
}
