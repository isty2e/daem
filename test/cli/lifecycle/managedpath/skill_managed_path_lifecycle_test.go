package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/workflow/migrate"
	"github.com/isty2e/daem/test/testkit"
)

func TestManagedSkillSecondApplyIsNoOpAndExternalDeletionReconverges(t *testing.T) {
	root, manifestPath, statefilePath, skillHash := prepareManagedSkillProject(t, []string{"codex"})
	installed := filepath.Join(root, ".agents", "skills", "oracle")
	stateBefore, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("read state before second apply: %v", err)
	}

	stdout, stderr, exitCode := runManagedSkillCLI("apply", "--manifest", manifestPath, "--yes")
	if exitCode != 0 || stderr != "" || !strings.Contains(stdout, "applied: 0 actions") {
		t.Fatalf("second apply exit=%d stdout=%q stderr=%q, want no-op", exitCode, stdout, stderr)
	}
	stateAfter, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("read state after second apply: %v", err)
	}
	if !bytes.Equal(stateAfter, stateBefore) {
		t.Fatal("second no-op apply rewrote managed state")
	}

	if err := os.RemoveAll(installed); err != nil {
		t.Fatalf("remove installed Skill externally: %v", err)
	}
	stdout, stderr, exitCode = runManagedSkillCLI("apply", "--manifest", manifestPath, "--yes")
	if exitCode != 0 || stderr != "" || !strings.Contains(stdout, "applied: 1 action") {
		t.Fatalf("reconverging apply exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if got := testkit.HashDirectory(t, installed); got != skillHash {
		t.Fatalf("restored Skill hash=%q, want %q", got, skillHash)
	}
	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("load reconverged state: %v", err)
	}
	testkit.AssertSkillPathState(t, state, "oracle", "codex", "project", ".agents/skills/oracle", testkit.InstalledHash(t, installed))
}

func TestSourceExecutableChangeOnlyAffectsNewSkillFiles(t *testing.T) {
	root, manifest, statePath, _ := prepareManagedSkillProject(t, []string{"codex"})
	source := filepath.Join(root, "skills", "oracle", "SKILL.md")
	installed := filepath.Join(root, ".agents", "skills", "oracle", "SKILL.md")
	stateBefore, lockBefore := testkit.ReadFile(t, statePath), testkit.ReadFile(t, filepath.Join(root, "daem.lock.toml"))
	if err := os.Chmod(installed, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(source, 0o755); err != nil {
		t.Fatal(err)
	}
	assertManagedSkillCLI(t, 0, "lock", "--manifest", manifest)
	if bytes.Equal(lockBefore, testkit.ReadFile(t, filepath.Join(root, "daem.lock.toml"))) {
		t.Fatal("source execution change was lost in the lockfile")
	}
	assertManagedSkillCLI(t, 0, "status", "--manifest", manifest, "--check")
	assertManagedSkillCLI(t, 0, "apply", "--manifest", manifest, "--yes")
	if info, err := os.Stat(installed); err != nil || info.Mode().Perm() != 0o444 {
		t.Fatalf("existing skill mode: %v, %v", info, err)
	}
	if !bytes.Equal(stateBefore, testkit.ReadFile(t, statePath)) {
		t.Fatal("source chmod rewrote installed state")
	}
}

func TestBaselineTreeMigratesRetiredSkillWithoutAdoptingItsHostContents(t *testing.T) {
	targets := []string{"codex"}
	root, manifest, statePath, legacy := prepareManagedSkillProject(t, targets)
	const destination = ".agents/skills/oracle"
	installed := filepath.Join(root, destination)
	file := filepath.Join(installed, "SKILL.md")
	original := filepath.Join(root, "skills", "oracle")
	testkit.WriteStatefile(t, statePath, testkit.Snapshot(t, testkit.SkillPathState(t, "oracle", targets, "project", destination, legacy)))
	before := testkit.ReadFile(t, statePath)
	if err := os.Chmod(file, 0o744); err != nil {
		t.Fatal(err)
	}
	writeManagedSkillManifest(t, root, targets, false)
	assertManagedSkillCLI(t, 0, "lock", "--manifest", manifest)
	migrate := func(code int, tree, mode string) {
		t.Helper()
		assertManagedSkillCLI(t, code, "migrate", "metadata", "--manifest", manifest, "--baseline-tree", tree, mode, "--json")
	}
	assertManagedSkillCLI(t, 1, "apply", "--manifest", manifest, "--yes", "--json")
	migrate(1, installed, "--yes")
	migrate(0, original, "--dry-run")
	if !bytes.Equal(testkit.ReadFile(t, statePath), before) {
		t.Fatal("preview or rejected proof changed state")
	}

	content := testkit.ReadFile(t, file)
	testkit.WriteFile(t, installed, "cache/data", "local cache\n")
	if err := os.WriteFile(file, []byte("user edits"), 0o600); err != nil {
		t.Fatal(err)
	}
	migrate(0, original, "--yes")
	after := testkit.ReadFile(t, statePath)
	if bytes.Equal(after, before) {
		t.Fatal("original tree did not convert legacy state")
	}
	migrate(0, original, "--yes")
	if !bytes.Equal(after, testkit.ReadFile(t, statePath)) {
		t.Fatal("repeat changed migrated state")
	}
	assertManagedSkillCLI(t, 1, "apply", "--manifest", manifest, "--yes", "--json")
	testkit.AssertFileContent(t, file, "user edits")
	if info, err := os.Stat(file); err != nil || info.Mode().Perm() != 0o744 {
		t.Fatalf("migration changed installed permissions: %v, %v", info, err)
	}
	if err := os.WriteFile(file, content, 0o600); err != nil {
		t.Fatal(err)
	}
	assertManagedSkillCLI(t, 0, "apply", "--manifest", manifest, "--yes")
	if _, err := os.Stat(installed); !os.IsNotExist(err) {
		t.Fatalf("retired skill still present: %v", err)
	}
}

func TestBaselineTreeRechecksStateAfterConfirmation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("changes Unix permission bits between approval and execution")
	}
	targets := []string{"codex"}
	root, manifest, path, legacy := prepareManagedSkillProject(t, targets)
	const destination = ".agents/skills/oracle"
	testkit.WriteStatefile(t, path, testkit.Snapshot(t, testkit.SkillPathState(t, "oracle", targets, "project", destination, legacy)))
	input := migrate.MetadataInput{ManifestPath: manifest, BaselineTree: filepath.Join(root, "skills", "oracle")}
	prepare := func() *migrate.PreparedMetadata {
		t.Helper()
		plan, err := migrate.PlanMetadata(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(plan.Close)
		return plan
	}

	plan := prepare()
	edited := append(testkit.ReadFile(t, path), '\n')
	if err := os.WriteFile(path, edited, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Execute(t.Context()); !errors.As(err, new(mutation.StaleSnapshotError)) || !bytes.Equal(edited, testkit.ReadFile(t, path)) {
		t.Fatalf("conversion did not leave the edited state alone: %v", err)
	}

	plan = prepare()
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o640 || bytes.Equal(edited, testkit.ReadFile(t, path)) {
		t.Fatalf("expected converted state with the new permissions: %v, %v", info, err)
	}
}

func TestManagedSkillExternalModificationBlocksWithoutMutation(t *testing.T) {
	root, manifestPath, statefilePath, _ := prepareManagedSkillProject(t, []string{"codex"})
	installedFile := filepath.Join(root, ".agents", "skills", "oracle", "SKILL.md")
	testkit.WriteFile(t, root, ".agents/skills/oracle/SKILL.md", "---\nname: oracle\ndescription: externally changed\n---\n")
	stateBefore, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("read state before drift check: %v", err)
	}

	stdout, stderr, exitCode := runManagedSkillCLI("apply", "--manifest", manifestPath, "--dry-run")
	if exitCode != 1 || stderr != "" || !strings.Contains(stdout, "reason=drifted_output") {
		t.Fatalf("drift apply exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	testkit.AssertFileContent(t, installedFile, "---\nname: oracle\ndescription: externally changed\n---\n")
	stateAfter, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("read state after drift check: %v", err)
	}
	if !bytes.Equal(stateAfter, stateBefore) {
		t.Fatal("blocked drift check mutated managed state")
	}
}

func TestSharedManagedSkillPathRetainsRemainingConsumerThenDeletesLast(t *testing.T) {
	root, manifestPath, statefilePath, _ := prepareManagedSkillProject(t, []string{"codex", "antigravity-cli"})
	installed := filepath.Join(root, ".agents", "skills", "oracle")
	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("load shared state: %v", err)
	}
	testkit.AssertManagedPathState(
		t, state, "skill", "oracle", []string{"antigravity-cli", "codex"},
		"project", ".agents/skills/oracle", testkit.InstalledHash(t, installed), "directory",
	)

	writeManagedSkillManifest(t, root, []string{"codex"}, true)
	baseline := testkit.InstalledHash(t, installed)
	testkit.WriteFile(t, installed, "scratch/cache", "keep until last removal\n")
	skillHash := testkit.HashDirectory(t, installed)
	assertManagedSkillCLI(t, 0, "lock", "--manifest", manifestPath)
	stdout, stderr, exitCode := runManagedSkillCLI("apply", "--manifest", manifestPath, "--target", "antigravity-cli", "--yes")
	if exitCode != 0 || stderr != "" || !strings.Contains(stdout, "applied: 1 action") {
		t.Fatalf("partial removal exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if got := testkit.HashDirectory(t, installed); got != skillHash {
		t.Fatalf("partially shared Skill hash=%q, want %q", got, skillHash)
	}
	state, err = statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("load partially shared state: %v", err)
	}
	testkit.AssertSkillPathState(t, state, "oracle", "codex", "project", ".agents/skills/oracle", baseline)

	writeManagedSkillManifest(t, root, []string{"codex"}, false)
	assertManagedSkillCLI(t, 0, "lock", "--manifest", manifestPath)
	stdout, stderr, exitCode = runManagedSkillCLI("apply", "--manifest", manifestPath, "--target", "codex", "--yes")
	if exitCode != 0 || stderr != "" || !strings.Contains(stdout, "applied: 1 action") {
		t.Fatalf("final removal exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if _, err := os.Stat(installed); !os.IsNotExist(err) {
		t.Fatalf("last-consumer removal left Skill path: %v", err)
	}
	state, err = statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("load final state: %v", err)
	}
	testkit.AssertSkillPathStateMissing(t, state, "oracle", "project", ".agents/skills/oracle")
}

func TestManagedSkillDistinctPlacementsRemoveOnlySelectedProjection(t *testing.T) {
	root, manifestPath, statefilePath, skillHash := prepareManagedSkillProject(t, []string{"codex", "claude-code"})
	codexPath := filepath.Join(root, ".agents", "skills", "oracle")
	claudePath := filepath.Join(root, ".claude", "skills", "oracle")
	if got := testkit.HashDirectory(t, codexPath); got != skillHash {
		t.Fatalf("Codex Skill hash=%q, want %q", got, skillHash)
	}
	if got := testkit.HashDirectory(t, claudePath); got != skillHash {
		t.Fatalf("Claude Skill hash=%q, want %q", got, skillHash)
	}

	writeManagedSkillManifest(t, root, []string{"codex"}, true)
	assertManagedSkillCLI(t, 0, "lock", "--manifest", manifestPath)
	stdout, stderr, exitCode := runManagedSkillCLI(
		"apply", "--manifest", manifestPath, "--target", "claude-code", "--yes",
	)
	if exitCode != 0 || stderr != "" || !strings.Contains(stdout, "applied: 1 action") {
		t.Fatalf("selected projection removal exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if got := testkit.HashDirectory(t, codexPath); got != skillHash {
		t.Fatalf("Claude removal changed Codex Skill hash=%q, want %q", got, skillHash)
	}
	if _, err := os.Stat(claudePath); !os.IsNotExist(err) {
		t.Fatalf("Claude projection removal left path: %v", err)
	}
	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("load state after selected projection removal: %v", err)
	}
	testkit.AssertSkillPathState(t, state, "oracle", "codex", "project", ".agents/skills/oracle", testkit.InstalledHash(t, codexPath))
	testkit.AssertSkillPathStateMissing(t, state, "oracle", "project", ".claude/skills/oracle")
}

func TestManagedSkillScopeRelocationAcquiresAndReleasesGlobalOwnership(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	testkit.SetDefaultRootEnv(t, home)
	manifestPath := filepath.Join(root, "daem.toml")
	statefilePath := filepath.Join(root, ".daem", "state.json")
	testkit.WriteFile(t, root, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	skillHash := testkit.HashDirectory(t, filepath.Join(root, "skills", "oracle"))

	writeManagedSkillManifestWithScope(t, root, "project")
	assertManagedSkillCLI(t, 0, "lock", "--manifest", manifestPath)
	assertManagedSkillCLI(t, 0, "apply", "--manifest", manifestPath, "--yes")
	projectPath := filepath.Join(root, ".agents", "skills", "oracle")
	globalPath := filepath.Join(home, ".agents", "skills", "oracle")

	writeManagedSkillManifestWithScope(t, root, "global")
	assertManagedSkillCLI(t, 0, "lock", "--manifest", manifestPath)
	assertManagedSkillCLI(t, 0, "apply", "--manifest", manifestPath, "--yes")
	if _, err := os.Stat(projectPath); !os.IsNotExist(err) {
		t.Fatalf("project-to-global relocation left project path: %v", err)
	}
	if got := testkit.HashDirectory(t, globalPath); got != skillHash {
		t.Fatalf("global Skill hash=%q, want %q", got, skillHash)
	}
	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatal(err)
	}
	testkit.AssertSkillPathState(t, state, "oracle", "codex", "global", "~/.agents/skills/oracle", testkit.InstalledHash(t, globalPath))
	testkit.AssertOwnershipClaimCount(t, manifestPath, 1)

	writeManagedSkillManifestWithScope(t, root, "project")
	assertManagedSkillCLI(t, 0, "lock", "--manifest", manifestPath)
	assertManagedSkillCLI(t, 0, "apply", "--manifest", manifestPath, "--yes")
	if _, err := os.Stat(globalPath); !os.IsNotExist(err) {
		t.Fatalf("global-to-project relocation left global path: %v", err)
	}
	if got := testkit.HashDirectory(t, projectPath); got != skillHash {
		t.Fatalf("restored project Skill hash=%q, want %q", got, skillHash)
	}
	state, err = statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatal(err)
	}
	testkit.AssertSkillPathState(t, state, "oracle", "codex", "project", ".agents/skills/oracle", testkit.InstalledHash(t, projectPath))
	testkit.AssertOwnershipClaimCount(t, manifestPath, 0)
	testkit.AssertNoRecoveryArtifacts(t, root)
}

func TestManagedSkillAdmittedRootSelectionRelocatesWithinScope(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	statefilePath := filepath.Join(root, ".daem", "state.json")
	testkit.WriteFile(t, root, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	skillHash := testkit.HashDirectory(t, filepath.Join(root, "skills", "oracle"))
	defaultPath := filepath.Join(root, ".opencode", "skills", "oracle")
	alternatePath := filepath.Join(root, ".agents", "skills", "oracle")

	writeManagedSkillManifestWithPlacement(t, root, "", false)
	assertManagedSkillCLI(t, 0, "lock", "--manifest", manifestPath)
	assertManagedSkillCLI(t, 0, "apply", "--manifest", manifestPath, "--yes")
	if got := testkit.HashDirectory(t, defaultPath); got != skillHash {
		t.Fatalf("default Skill hash=%q, want %q", got, skillHash)
	}
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(t, testkit.SkillPathState(t, "oracle", []string{"opencode"}, "project", ".opencode/skills/oracle", skillHash)))

	writeManagedSkillManifestWithPlacement(t, root, ".agents/skills", false)
	assertManagedSkillCLI(t, 0, "lock", "--manifest", manifestPath)
	stateBefore, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, exitCode := runManagedSkillCLI("apply", "--manifest", manifestPath, "--dry-run")
	if exitCode != 0 || stderr != "" ||
		!strings.Contains(stdout, "dry-run: 1 output entry") ||
		!strings.Contains(stdout, `destination=".agents/skills/oracle"`) ||
		!strings.Contains(stdout, `detail="managed destination changed"`) {
		t.Fatalf("relocation dry-run exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if got := testkit.HashDirectory(t, defaultPath); got != skillHash {
		t.Fatalf("dry-run changed default Skill hash=%q, want %q", got, skillHash)
	}
	if _, err := os.Stat(alternatePath); !os.IsNotExist(err) {
		t.Fatalf("dry-run created alternate Skill path: %v", err)
	}
	stateAfterDryRun, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stateAfterDryRun, stateBefore) {
		t.Fatal("dry-run changed managed state")
	}
	if err := os.Chmod(filepath.Join(defaultPath, "SKILL.md"), 0o744); err != nil {
		t.Fatal(err)
	}

	assertManagedSkillCLI(t, 0, "apply", "--manifest", manifestPath, "--yes")
	if _, err := os.Stat(defaultPath); !os.IsNotExist(err) {
		t.Fatalf("alternate relocation left default Skill path: %v", err)
	}
	if got := testkit.HashDirectory(t, alternatePath); got != skillHash {
		t.Fatalf("alternate Skill hash=%q, want %q", got, skillHash)
	}
	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatal(err)
	}
	testkit.AssertSkillPathState(t, state, "oracle", "opencode", "project", ".agents/skills/oracle", testkit.InstalledHash(t, alternatePath))

	writeManagedSkillManifestWithPlacement(t, root, "", false)
	assertManagedSkillCLI(t, 0, "lock", "--manifest", manifestPath)
	assertManagedSkillCLI(t, 0, "apply", "--manifest", manifestPath, "--yes")
	if _, err := os.Stat(alternatePath); !os.IsNotExist(err) {
		t.Fatalf("default relocation left alternate Skill path: %v", err)
	}
	if got := testkit.HashDirectory(t, defaultPath); got != skillHash {
		t.Fatalf("restored default Skill hash=%q, want %q", got, skillHash)
	}
	testkit.AssertNoRecoveryArtifacts(t, root)
}

func TestManagedSkillAdmittedRootRelocationRejectsOccupiedDestinationAndChangedSource(t *testing.T) {
	t.Run("occupied destination", func(t *testing.T) {
		root := prepareOpenCodePlacementProject(t)
		manifestPath := filepath.Join(root, "daem.toml")
		defaultPath := filepath.Join(root, ".opencode", "skills", "oracle")
		alternatePath := filepath.Join(root, ".agents", "skills", "oracle")
		skillHash := testkit.HashDirectory(t, defaultPath)

		writeManagedSkillManifestWithPlacement(t, root, ".agents/skills", false)
		assertManagedSkillCLI(t, 0, "lock", "--manifest", manifestPath)
		testkit.WriteFile(t, root, ".agents/skills/oracle/foreign.txt", "foreign\n")

		stdout, stderr, exitCode := runManagedSkillCLI("apply", "--manifest", manifestPath, "--dry-run")
		if exitCode != 1 || stderr != "" || !strings.Contains(stdout, "reason=unmanaged_output_exists") {
			t.Fatalf("occupied relocation exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
		}
		if got := testkit.HashDirectory(t, defaultPath); got != skillHash {
			t.Fatalf("occupied relocation changed source hash=%q, want %q", got, skillHash)
		}
		testkit.AssertFileContent(t, filepath.Join(alternatePath, "foreign.txt"), "foreign\n")
	})

	t.Run("changed old output", func(t *testing.T) {
		root := prepareOpenCodePlacementProject(t)
		manifestPath := filepath.Join(root, "daem.toml")
		defaultPath := filepath.Join(root, ".opencode", "skills", "oracle")
		alternatePath := filepath.Join(root, ".agents", "skills", "oracle")
		testkit.WriteFile(t, root, ".opencode/skills/oracle/SKILL.md", "externally changed\n")

		writeManagedSkillManifestWithPlacement(t, root, ".agents/skills", false)
		assertManagedSkillCLI(t, 0, "lock", "--manifest", manifestPath)
		stdout, stderr, exitCode := runManagedSkillCLI("apply", "--manifest", manifestPath, "--dry-run")
		if exitCode != 1 || stderr != "" || !strings.Contains(stdout, "reason=drifted_output") {
			t.Fatalf("drifted relocation exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
		}
		testkit.AssertFileContent(t, filepath.Join(defaultPath, "SKILL.md"), "externally changed\n")
		if _, err := os.Stat(alternatePath); !os.IsNotExist(err) {
			t.Fatalf("drifted relocation created alternate path: %v", err)
		}
	})

	t.Run("missing old output", func(t *testing.T) {
		root := prepareOpenCodePlacementProject(t)
		manifestPath := filepath.Join(root, "daem.toml")
		defaultPath := filepath.Join(root, ".opencode", "skills", "oracle")
		alternatePath := filepath.Join(root, ".agents", "skills", "oracle")
		if err := os.RemoveAll(defaultPath); err != nil {
			t.Fatal(err)
		}

		writeManagedSkillManifestWithPlacement(t, root, ".agents/skills", false)
		assertManagedSkillCLI(t, 0, "lock", "--manifest", manifestPath)
		stdout, stderr, exitCode := runManagedSkillCLI("apply", "--manifest", manifestPath, "--dry-run")
		if exitCode != 1 || stderr != "" || !strings.Contains(stdout, "reason=drifted_output") {
			t.Fatalf("missing-old relocation exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
		}
		if _, err := os.Stat(alternatePath); !os.IsNotExist(err) {
			t.Fatalf("missing-old relocation created alternate path: %v", err)
		}
	})
}

func TestManagedSkillGroupInheritsAdmittedRootSelection(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	writeManagedSkillManifestWithPlacement(t, root, ".agents/skills", true)

	assertManagedSkillCLI(t, 0, "lock", "--manifest", manifestPath)
	assertManagedSkillCLI(t, 0, "apply", "--manifest", manifestPath, "--yes")
	wantHash := testkit.HashDirectory(t, filepath.Join(root, "skills", "oracle"))
	if got := testkit.HashDirectory(t, filepath.Join(root, ".agents", "skills", "oracle")); got != wantHash {
		t.Fatalf("group-selected Skill hash=%q, want %q", got, wantHash)
	}
	if _, err := os.Stat(filepath.Join(root, ".opencode", "skills", "oracle")); !os.IsNotExist(err) {
		t.Fatalf("group selection also wrote default path: %v", err)
	}
}

func TestManagedSkillUnsupportedPlacementFailsBeforeMutation(t *testing.T) {
	for _, mode := range []string{"symlink", "hardlink"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			manifestPath := filepath.Join(root, "daem.toml")
			testkit.WriteFile(t, root, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
			testkit.WriteFile(t, root, "daem.toml", fmt.Sprintf(`
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]
				install_mode = %q
				`, mode))
			assertManagedSkillCLI(t, 0, "lock", "--manifest", manifestPath)
			stdout, stderr, exitCode := runManagedSkillCLI("apply", "--manifest", manifestPath, "--dry-run")
			if exitCode != 1 || !strings.Contains(stdout, "Apply preview (not executable)") || !strings.Contains(stderr, "unsupported placement mode") {
				t.Fatalf("unsupported placement exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
			}
			if _, err := os.Stat(filepath.Join(root, ".agents", "skills", "oracle")); !os.IsNotExist(err) {
				t.Fatalf("unsupported placement wrote Skill output: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, ".daem")); !os.IsNotExist(err) {
				t.Fatalf("unsupported placement wrote state or journal directory: %v", err)
			}
		})
	}
}

func TestManagedSkillRelocationRequiresAbsentDestinationAndMovesStateAtomically(t *testing.T) {
	root, manifestPath, statefilePath, skillHash := prepareManagedSkillProject(t, []string{"codex"})
	oldPath := filepath.Join(root, ".agents", "skills", "oracle")
	newPath := filepath.Join(root, ".agents", "skills", "review")
	writeManagedSkillManifestWithInstallName(t, root, []string{"codex"}, "review")
	assertManagedSkillCLI(t, 0, "lock", "--manifest", manifestPath)
	testkit.WriteFile(t, root, ".agents/skills/review/foreign.txt", "foreign\n")

	stdout, stderr, exitCode := runManagedSkillCLI("apply", "--manifest", manifestPath, "--dry-run")
	if exitCode != 1 || stderr != "" || !strings.Contains(stdout, "reason=unmanaged_output_exists") {
		t.Fatalf("blocked relocation exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if got := testkit.HashDirectory(t, oldPath); got != skillHash {
		t.Fatalf("blocked relocation changed old path hash=%q, want %q", got, skillHash)
	}
	testkit.AssertFileContent(t, filepath.Join(newPath, "foreign.txt"), "foreign\n")
	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("load state after blocked relocation: %v", err)
	}
	testkit.AssertSkillPathState(t, state, "oracle", "codex", "project", ".agents/skills/oracle", testkit.InstalledHash(t, oldPath))

	if err := os.RemoveAll(newPath); err != nil {
		t.Fatalf("remove relocation conflict: %v", err)
	}
	testkit.WriteFile(t, oldPath, "cache/data", "old placement only\n")
	stdout, stderr, exitCode = runManagedSkillCLI("apply", "--manifest", manifestPath, "--yes")
	if exitCode != 0 || stderr != "" || !strings.Contains(stdout, "applied: 1 action") {
		t.Fatalf("relocation exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("relocation left old Skill path: %v", err)
	}
	if got := testkit.HashDirectory(t, newPath); got != skillHash {
		t.Fatalf("relocated Skill hash=%q, want %q", got, skillHash)
	}
	state, err = statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("load relocated state: %v", err)
	}
	testkit.AssertSkillPathStateMissing(t, state, "oracle", "project", ".agents/skills/oracle")
	testkit.AssertSkillPathState(t, state, "oracle", "codex", "project", ".agents/skills/review", testkit.InstalledHash(t, newPath))
	testkit.AssertNoRecoveryArtifacts(t, root)
}

func TestManagedSkillRelocationRejectsSourceDestinationOverlap(t *testing.T) {
	root, manifestPath, statefilePath, skillHash := prepareManagedSkillProject(t, []string{"codex"})
	oldPath := filepath.Join(root, ".agents", "skills", "oracle")
	newPath := filepath.Join(root, ".agents", "skills", "review")
	testkit.WriteFile(t, root, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
id = "oracle"
name = "review"
source = { path = ".agents/skills/oracle", mode = "vendor" }
targets = ["codex"]
	`)
	assertManagedSkillCLI(t, 0, "lock", "--manifest", manifestPath)
	stateBefore, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatal(err)
	}

	stdout, stderr, exitCode := runManagedSkillCLI("apply", "--manifest", manifestPath, "--dry-run")
	if exitCode != 1 || !strings.Contains(stdout, "Apply preview (not executable)") || !strings.Contains(stderr, "overlaps managed mutation destination") {
		t.Fatalf("source-overlap dry-run exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	stdout, stderr, exitCode = runManagedSkillCLI("apply", "--manifest", manifestPath, "--yes")
	if exitCode != 1 || stdout != "" || !strings.Contains(stderr, "overlaps managed mutation destination") {
		t.Fatalf("source-overlap write exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if got := testkit.HashDirectory(t, oldPath); got != skillHash {
		t.Fatalf("source-overlap changed old Skill hash=%q, want %q", got, skillHash)
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatalf("source-overlap created new Skill path: %v", err)
	}
	stateAfter, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stateAfter, stateBefore) {
		t.Fatal("source-overlap changed managed state")
	}
	testkit.AssertNoRecoveryArtifacts(t, root)
}

func TestManagedSkillManageExistingCannotTransferAnotherSubjectPath(t *testing.T) {
	root, manifestPath, statefilePath, skillHash := prepareManagedSkillProject(t, []string{"codex"})
	installed := filepath.Join(root, ".agents", "skills", "oracle")
	stateBefore, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, root, "daem.toml", `
version = 1
targets = ["antigravity-cli"]

[[skill]]
id = "review"
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["antigravity-cli"]
`)
	assertManagedSkillCLI(t, 0, "lock", "--manifest", manifestPath)

	stdout, stderr, exitCode := runManagedSkillCLI(
		"apply", "--manifest", manifestPath, "--target", "antigravity-cli", "--manage-existing", "--dry-run",
	)
	if exitCode != 1 || stderr != "" || !strings.Contains(stdout, "reason=destination_conflict") {
		t.Fatalf("cross-subject adoption exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if got := testkit.HashDirectory(t, installed); got != skillHash {
		t.Fatalf("cross-subject adoption changed installed hash=%q, want %q", got, skillHash)
	}
	stateAfter, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stateAfter, stateBefore) {
		t.Fatal("cross-subject adoption changed prior managed authority")
	}
}

func prepareManagedSkillProject(t *testing.T, targets []string) (string, string, string, string) {
	t.Helper()
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	statefilePath := filepath.Join(root, ".daem", "state.json")
	testkit.WriteFile(t, root, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	writeManagedSkillManifest(t, root, targets, true)
	assertManagedSkillCLI(t, 0, "lock", "--manifest", manifestPath)
	assertManagedSkillCLI(t, 0, "apply", "--manifest", manifestPath, "--yes")
	return root, manifestPath, statefilePath, testkit.HashDirectory(t, filepath.Join(root, "skills", "oracle"))
}

func writeManagedSkillManifest(t *testing.T, root string, targets []string, includeSkill bool) {
	t.Helper()
	encodedTargets, err := json.Marshal(targets)
	if err != nil {
		t.Fatalf("encode manifest targets: %v", err)
	}
	content := fmt.Sprintf("version = 1\ntargets = %s\n", encodedTargets)
	if includeSkill {
		content += fmt.Sprintf("\n[[skill]]\nname = \"oracle\"\nsource = { path = \"skills/oracle\", mode = \"vendor\" }\ntargets = %s\n", encodedTargets)
	}
	testkit.WriteFile(t, root, "daem.toml", content)
}

func writeManagedSkillManifestWithInstallName(t *testing.T, root string, targets []string, installName string) {
	t.Helper()
	encodedTargets, err := json.Marshal(targets)
	if err != nil {
		t.Fatalf("encode manifest targets: %v", err)
	}
	content := fmt.Sprintf(
		"version = 1\ntargets = %s\n\n[[skill]]\nid = \"oracle\"\nname = %q\nsource = { path = \"skills/oracle\", mode = \"vendor\" }\ntargets = %s\n",
		encodedTargets,
		installName,
		encodedTargets,
	)
	testkit.WriteFile(t, root, "daem.toml", content)
}

func writeManagedSkillManifestWithScope(t *testing.T, root string, scope string) {
	t.Helper()
	sourcePath := "skills/oracle"
	if scope == "global" {
		sourcePath = filepath.ToSlash(filepath.Join(root, "skills", "oracle"))
	}
	testkit.WriteFile(t, root, "daem.toml", fmt.Sprintf(
		"version = 1\ntargets = [\"codex\"]\n\n[[skill]]\nname = \"oracle\"\nsource = { path = %q, mode = \"vendor\" }\ntargets = [\"codex\"]\nscope = %q\n",
		sourcePath, scope,
	))
}

func prepareOpenCodePlacementProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	testkit.WriteFile(t, root, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	writeManagedSkillManifestWithPlacement(t, root, "", false)
	manifestPath := filepath.Join(root, "daem.toml")
	assertManagedSkillCLI(t, 0, "lock", "--manifest", manifestPath)
	assertManagedSkillCLI(t, 0, "apply", "--manifest", manifestPath, "--yes")
	return root
}

func writeManagedSkillManifestWithPlacement(t *testing.T, root string, installTo string, group bool) {
	t.Helper()
	kind := "skill"
	declaration := `[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }`
	if group {
		kind = "skill_group"
		declaration = `[[skill_group]]
names = ["oracle"]
source = { path = "skills", mode = "vendor" }`
	}
	content := "version = 1\ntargets = [\"opencode\"]\n\n" +
		declaration + "\ntargets = [\"opencode\"]\nscope = \"project\"\n"
	if installTo != "" {
		content += fmt.Sprintf(
			"\n[%s.target.opencode]\ninstall_to = %q\n",
			kind,
			installTo,
		)
	}
	testkit.WriteFile(t, root, "daem.toml", content)
}

func assertManagedSkillCLI(t *testing.T, wantExit int, args ...string) string {
	t.Helper()
	stdout, stderr, exitCode := runManagedSkillCLI(args...)
	if exitCode != wantExit || stderr != "" {
		t.Fatalf("daem %v exit=%d stdout=%q stderr=%q", args, exitCode, stdout, stderr)
	}
	return stdout
}

func runManagedSkillCLI(args ...string) (string, string, int) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
	return stdout.String(), stderr.String(), exitCode
}
