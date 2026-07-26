package lock

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/instructions"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	daempaths "github.com/isty2e/daem/internal/paths"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	lock "github.com/isty2e/daem/internal/realization/lock"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	sourceresolution "github.com/isty2e/daem/internal/supply/source/resolution"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
	targetavailability "github.com/isty2e/daem/internal/target/availability"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

type panicRootListerResolver struct {
	resolver  acquisition.BatchResolver
	listCalls int
}

func lockObservations(
	ctx context.Context,
	paths daempaths.Paths,
	environment desired.Environment,
	locked lock.File,
	selection targetselection.Selection,
) (ObservationSet, error) {
	epoch, err := ResolveSourceEpoch(ctx, paths, environment, locked, selection)
	if err != nil {
		return ObservationSet{}, err
	}
	return epoch.Observations(ctx)
}

func (resolver *panicRootListerResolver) ResolveBatch(
	ctx context.Context,
	requests []acquisition.Request,
	options acquisition.BatchOptions,
) ([]acquisition.Result, error) {
	return resolver.resolver.ResolveBatch(ctx, requests, options)
}

func (resolver *panicRootListerResolver) ListSourceRoot(
	context.Context,
	source.Source,
) (source.RootListing, error) {
	resolver.listCalls++
	panic("lock observation rediscovered a selector source root")
}

func TestLockObservationsReportsFreshAndStaleInstructionLocks(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeTestFile(t, tempDir, "instructions/project.md", "project instructions\n")
	writeTestFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/project.md"
targets = ["codex"]
`)
	environment := parseTestManifest(t, string(mustReadTestFile(t, manifestPath)))
	selection := testSelection(t, environment, "codex")
	paths := resolveTestPaths(t, manifestPath)
	sourceHash := hashTestPath(t, filepath.Join(tempDir, "instructions", "project.md"), artifact.ArtifactKindFile)

	freshLocks := snapshottest.File(t, instructionLockSubjects(t, sourceHash)...)
	observations, err := lockObservations(context.Background(), paths, environment, freshLocks, selection)
	if err != nil {
		t.Fatalf("lockObservations fresh returned error: %v", err)
	}
	exact := observations.ExactSupplies()
	if len(exact) != 1 || exact[0].Stale() {
		t.Fatalf("fresh observations = %#v, want one non-stale project observation", exact)
	}

	staleLocks := snapshottest.File(t, instructionLockSubjects(t, artifact.HashFileContent([]byte("old instructions")))...)
	observations, err = lockObservations(context.Background(), paths, environment, staleLocks, selection)
	if err != nil {
		t.Fatalf("lockObservations stale returned error: %v", err)
	}
	exact = observations.ExactSupplies()
	if len(exact) != 1 || !exact[0].Stale() {
		t.Fatalf("stale observations = %#v, want one stale observation", exact)
	}
}

func TestLockObservationsReportsHookAssetThroughExactSupply(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeTestFile(t, tempDir, "hooks/guard.sh", "#!/bin/sh\nexit 0\n")
	writeTestFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[hook_asset.guard]
source = "hooks/guard.sh"
kind = "file"
scope = "project"
executable = true

[[hook]]
name = "guard"
event = "PreToolUse"
command = "{hook_file:guard} --check"
targets = ["codex"]
scope = "project"
`)
	environment := parseTestManifest(t, string(mustReadTestFile(t, manifestPath)))
	selection := testSelection(t, environment, "codex")
	paths := resolveTestPaths(t, manifestPath)
	resolver, err := sourceresolution.NewResolver(paths)
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	locked, err := lockbuild.BuildWithOptions(context.Background(), environment, resolver, lockbuild.Options{
		HookContributionEncoder: hookcodec.CanonicalHookContribution,
	})
	if err != nil {
		t.Fatalf("lockbuild.BuildWithOptions returned error: %v", err)
	}
	assetID, err := entity.New(entity.KindHookAsset, "guard")
	if err != nil {
		t.Fatal(err)
	}
	lockedAsset, ok := locked.Locked.ExactSupplySubject(assetID)
	if !ok {
		t.Fatalf("locked subjects = %#v, want guard exact Supply", locked.Locked.Subjects())
	}

	observations, err := lockObservations(context.Background(), paths, environment, locked, selection)
	if err != nil {
		t.Fatalf("lockObservations fresh returned error: %v", err)
	}
	exact := observations.ExactSupplies()
	if len(exact) != 1 || exact[0].Subject() != lockedAsset.SubjectID() || exact[0].Stale() {
		t.Fatalf("fresh observations = %#v, want one fresh guard exact Supply", exact)
	}

	writeTestFile(t, tempDir, "hooks/guard.sh", "#!/bin/sh\nexit 1\n")
	observations, err = lockObservations(context.Background(), paths, environment, locked, selection)
	if err != nil {
		t.Fatalf("lockObservations stale returned error: %v", err)
	}
	exact = observations.ExactSupplies()
	if len(exact) != 1 || exact[0].Subject() != lockedAsset.SubjectID() || !exact[0].Stale() {
		t.Fatalf("stale observations = %#v, want one stale guard exact Supply", exact)
	}
}

func TestLockObservationsSkipsHookAssetsConsumedOnlyByUnselectedTargets(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeTestFile(t, tempDir, "hooks/shared.sh", "#!/bin/sh\nexit 0\n")
	writeTestFile(t, tempDir, "hooks/claude-only.sh", "#!/bin/sh\nexit 0\n")
	writeTestFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex", "claude-code"]

[hook_asset.shared]
source = "hooks/shared.sh"
kind = "file"
scope = "project"
executable = true

[hook_asset.claude-only]
source = "hooks/claude-only.sh"
kind = "file"
scope = "project"
executable = true

[[hook]]
name = "shared"
event = "PreToolUse"
command = "{hook_file:shared} --check"
targets = ["codex", "claude-code"]
scope = "project"

[[hook]]
name = "claude-only"
event = "PreToolUse"
command = "{hook_file:claude-only} --check"
targets = ["claude-code"]
scope = "project"
`)
	environment := parseTestManifest(t, string(mustReadTestFile(t, manifestPath)))
	selection := testSelection(t, environment, "codex")
	paths := resolveTestPaths(t, manifestPath)
	resolver, err := sourceresolution.NewResolver(paths)
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	locked, err := lockbuild.BuildWithOptions(context.Background(), environment, resolver, lockbuild.Options{
		HookContributionEncoder: hookcodec.CanonicalHookContribution,
	})
	if err != nil {
		t.Fatalf("lockbuild.BuildWithOptions returned error: %v", err)
	}
	sharedID, err := entity.New(entity.KindHookAsset, "shared")
	if err != nil {
		t.Fatal(err)
	}
	lockedShared, ok := locked.Locked.ExactSupplySubject(sharedID)
	if !ok {
		t.Fatalf("locked subjects = %#v, want shared exact Supply", locked.Locked.Subjects())
	}
	writeTestFile(t, tempDir, "hooks/shared.sh", "#!/bin/sh\nexit 1\n")
	writeTestFile(t, tempDir, "hooks/claude-only.sh", "#!/bin/sh\nexit 1\n")

	observations, err := lockObservations(context.Background(), paths, environment, locked, selection)
	if err != nil {
		t.Fatalf("lockObservations returned error: %v", err)
	}
	exact := observations.ExactSupplies()
	if len(exact) != 1 || exact[0].Subject() != lockedShared.SubjectID() || !exact[0].Stale() {
		t.Fatalf("observations = %#v, want only stale shared HookAsset", exact)
	}
}

func TestLockObservationsReplaysRepairedSkillLocks(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeTestFile(t, tempDir, "skills/oracle/skill.md", "---\nname: oracle\ndescription: oracle\n---\n")
	writeTestFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]
compat_repair = true
`)
	environment := parseTestManifest(t, string(mustReadTestFile(t, manifestPath)))
	selection := testSelection(t, environment, "codex")
	paths := resolveTestPaths(t, manifestPath)
	resolver, err := sourceresolution.NewResolver(paths)
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	locked, err := lockbuild.BuildWithOptions(context.Background(), environment, resolver, lockbuild.Options{})
	if err != nil {
		t.Fatalf("lockbuild.BuildWithOptions returned error: %v", err)
	}
	skillID, err := entity.New(entity.KindSkill, "oracle")
	if err != nil {
		t.Fatalf("entity.New returned error: %v", err)
	}
	lockedSkill, ok := locked.Locked.ExactSupplySubject(skillID)
	if !ok {
		t.Fatalf("locked subjects = %#v, want oracle exact Supply", locked.Locked.Subjects())
	}
	if _, ok := lockedSkill.RepairRecipe(); !ok {
		t.Fatalf("locked oracle subject = %#v, want repair recipe", lockedSkill)
	}

	recipe, _ := lockedSkill.RepairRecipe()
	epoch, err := ResolveSourceEpoch(context.Background(), paths, environment, locked, selection)
	if err != nil {
		t.Fatalf("ResolveSourceEpoch returned error: %v", err)
	}
	rawResolution, ok := epoch.SkillResolution(environment.Skills()[0], selection)
	if !ok {
		t.Fatal("raw oracle skill resolution is unavailable")
	}
	if !rawResolution.Identity().Equal(recipe.Input()) {
		t.Fatalf(
			"source epoch skill identity = %q, want raw repair input %q",
			rawResolution.Identity().ContentHash(),
			recipe.Input().ContentHash(),
		)
	}

	observations, err := epoch.Observations(context.Background())
	if err != nil {
		t.Fatalf("SourceEpoch.Observations returned error: %v", err)
	}
	supplies := observations.ExactSupplies()
	if len(supplies) != 1 || supplies[0].Subject() != lockedSkill.SubjectID() || supplies[0].Stale() {
		t.Fatalf("observations = %#v, want repaired oracle lock to be non-stale", supplies)
	}
}

func TestLockObservationsReplaysLockedSkillSetChildrenWithoutRediscovery(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeTestFile(t, tempDir, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: alpha\n---\n")
	writeTestFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill_group]]
source = { path = "skills", mode = "vendor" }
include = ["glob:*"]
targets = ["codex"]
`)

	environment := parseTestManifest(t, string(mustReadTestFile(t, manifestPath)))
	selection := testSelection(t, environment, "codex")
	paths := resolveTestPaths(t, manifestPath)
	resolver, err := sourceresolution.NewResolver(paths)
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	locked, err := lockbuild.BuildWithOptions(context.Background(), environment, resolver, lockbuild.Options{})
	if err != nil {
		t.Fatalf("lockbuild.BuildWithOptions returned error: %v", err)
	}
	generated, err := locked.Locked.SkillSetChildren(environment.Skills(), environment.SkillSets())
	if err != nil {
		t.Fatalf("SkillSetChildren returned error: %v", err)
	}
	runtimeEnvironment, err := environment.WithGeneratedSkills(generated)
	if err != nil {
		t.Fatalf("WithGeneratedSkills returned error: %v", err)
	}

	writeTestFile(t, tempDir, "skills/beta/SKILL.md", "---\nname: beta\ndescription: beta\n---\n")
	guardedResolver := &panicRootListerResolver{resolver: resolver}
	observations, err := lockObservationsWithResolver(
		context.Background(),
		guardedResolver,
		runtimeEnvironment,
		locked,
		selection,
	)
	if err != nil {
		t.Fatalf("lockObservationsWithResolver returned error: %v", err)
	}
	if guardedResolver.listCalls != 0 {
		t.Fatalf("ListSourceRoot calls = %d, want 0", guardedResolver.listCalls)
	}
	alphaID, err := entity.New(entity.KindSkill, "alpha")
	if err != nil {
		t.Fatalf("entity.New returned error: %v", err)
	}
	lockedAlpha, ok := locked.Locked.ExactSupplySubject(alphaID)
	if !ok {
		t.Fatalf("locked subjects = %#v, want alpha exact Supply", locked.Locked.Subjects())
	}
	supplies := observations.ExactSupplies()
	if len(supplies) != 1 || supplies[0].Subject() != lockedAlpha.SubjectID() || supplies[0].Stale() {
		t.Fatalf("observations = %#v, want only fresh locked child alpha", supplies)
	}
}

func parseTestManifest(t *testing.T, content string) desired.Environment {
	t.Helper()

	environment, err := declarationmanifest.Decode([]byte(content))
	if err != nil {
		t.Fatalf("declarationmanifest.Decode returned error: %v", err)
	}
	return environment
}

func testSelection(t *testing.T, environment desired.Environment, requested ...string) targetselection.Selection {
	t.Helper()

	availableTargets := targetavailability.FromEnvironment(environment)
	selection, err := targetselection.ForAvailableTargets(availableTargets, requested)
	if err != nil {
		t.Fatalf("ForAvailableTargets returned error: %v", err)
	}
	return selection
}

func resolveTestPaths(t *testing.T, manifestPath string) daempaths.Paths {
	t.Helper()

	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatalf("paths.Resolve returned error: %v", err)
	}
	return paths
}

func writeTestFile(t *testing.T, root string, relativePath string, content string) {
	t.Helper()

	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func mustReadTestFile(t *testing.T, path string) []byte {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %q returned error: %v", path, err)
	}
	return content
}

func hashTestPath(t *testing.T, path string, wantKind artifact.ArtifactKind) artifact.ContentHash {
	t.Helper()

	contentHash, artifactKind, err := access.HashPath(context.Background(), path)
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}
	if artifactKind != wantKind {
		t.Fatalf("artifactKind = %q, want %q", artifactKind, wantKind)
	}
	return contentHash
}

func instructionLockSubjects(t *testing.T, contentHash artifact.ContentHash) []lock.LockedSubjectContract {
	t.Helper()
	fileUse, err := lock.NewExactFileUse(target.ScopeProject, false)
	if err != nil {
		t.Fatal(err)
	}
	supply := snapshottest.ExactSupplyContract(t, snapshottest.ExactSupplyInput{
		Kind:         entity.KindInstructions,
		Name:         "project",
		SourceID:     "local:instructions/project.md?mode=vendor",
		ArtifactKind: artifact.ArtifactKindFile,
		ContentHash:  contentHash,
		ExactFileUse: &fileUse,
	})
	value := desiredtest.Instructions(t, instructions.Spec{
		Name: "project", Source: sourcetest.Local(t, "instructions/project.md", source.LocalSourceModeVendor),
		Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject,
	})
	projections, err := refine.InstructionsPathProjections(value)
	if err != nil {
		t.Fatal(err)
	}
	return append([]lock.LockedSubjectContract{supply}, projections...)
}
