package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunAddInstructionGitFileWithSpacesWritesManifestAndLockOnly(t *testing.T) {
	testkit.RequireGit(t)
	tempDir := t.TempDir()
	repoPath := testkit.InitGitRepository(t, tempDir)
	instructionPath := filepath.Join(repoPath, "guides", "file with spaces.md")
	testkit.WriteFile(t, filepath.Dir(instructionPath), filepath.Base(instructionPath), "Git guidance.\n")
	commit := testkit.CommitRepository(t, repoPath, "add instruction")
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	testkit.WithWorkingDirectory(t, tempDir)

	args := []string{"add", "instruction", "project", repoPath, "--path", "guides/file with spaces.md", "--ref", "main", "--manifest", manifestPath, "--target", "codex"}
	var stdout, stderr bytes.Buffer
	if exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("add exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	config, err := declarationmanifest.Decode(manifest)
	if err != nil {
		t.Fatal(err)
	}
	gitSource, ok := config.Instructions()[0].Source().Git()
	if !ok || gitSource.Locator().String() != repoPath || gitSource.RepositoryPath().String() != "guides/file with spaces.md" || gitSource.Ref().String() != "main" {
		t.Fatalf("instruction source = %#v", config.Instructions()[0].Source())
	}
	lockPath := filepath.Join(tempDir, "daem.lock.toml")
	lock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("lockfile: %v", err)
	}
	locked, err := lockfile.Load(t.Context(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	instructions := testkit.LockedInstructions(t, locked)
	if len(instructions) != 1 || instructions[0].ResolvedRef != commit {
		t.Fatalf("locked instructions = %#v, want commit %q", instructions, commit)
	}
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "AGENTS.md"))

	beforeManifest, beforeLock := string(manifest), string(lock)
	stdout.Reset()
	stderr.Reset()
	if exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("repeat add exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	repeatedManifest, _ := os.ReadFile(manifestPath)
	repeatedLock, _ := os.ReadFile(lockPath)
	if string(repeatedManifest) != beforeManifest || string(repeatedLock) != beforeLock {
		t.Fatalf("repeat add changed manifest or lock")
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("apply exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "Git guidance.\n")
}

func TestRunAddInstructionGitDryRunPreservesManifestAndHost(t *testing.T) {
	testkit.RequireGit(t)
	tempDir := t.TempDir()
	cacheHome := filepath.Join(tempDir, "cache")
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	repoPath := testkit.InitGitRepository(t, tempDir)
	testkit.WriteFile(t, repoPath, "guide.md", "remote guidance\n")
	testkit.CommitRepository(t, repoPath, "guide")
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := "version = 1\ntargets = [\"codex\"]\n"
	testkit.WriteFile(t, tempDir, "daem.toml", original)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "host stays\n")
	var stdout, stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"add", "instruction", "project", repoPath, "--path", "guide.md", "--ref", "main", "--manifest", manifestPath, "--target", "codex", "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	testkit.AssertFileContent(t, manifestPath, original)
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "host stays\n")
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.lock.toml"))
	testkit.AssertPathMissing(t, filepath.Join(cacheHome, "daem", "sources"))
}

func TestRunAddInstructionGitRejectsNonRegularPathBeforeAuthoring(t *testing.T) {
	testkit.RequireGit(t)
	for _, tc := range []struct {
		name string
		path string
	}{
		{"missing", "missing.md"},
		{"directory", "docs"},
		{"symlink", "link.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			repoPath := testkit.InitGitRepository(t, tempDir)
			testkit.WriteFile(t, repoPath, "guide.md", "guide\n")
			if tc.name == "directory" {
				testkit.WriteFile(t, repoPath, "docs/guide.md", "tracked directory\n")
			}
			if tc.name == "symlink" {
				if err := os.Symlink("guide.md", filepath.Join(repoPath, tc.path)); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			testkit.CommitRepository(t, repoPath, "fixture")
			manifestPath := filepath.Join(tempDir, "daem.toml")
			original := "version = 1\ntargets = [\"codex\"]\n"
			testkit.WriteFile(t, tempDir, "daem.toml", original)
			var stdout, stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI([]string{"add", "instruction", "project", repoPath, "--path", tc.path, "--ref", "main", "--manifest", manifestPath, "--target", "codex"}, &stdout, &stderr)
			if exitCode != 1 {
				t.Fatalf("exitCode = %d, want 1; stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
			}
			if tc.name == "directory" && !strings.Contains(stderr.String(), "expected file artifact") {
				t.Fatalf("directory was not rejected by file-kind validation: %s", stderr.String())
			}
			testkit.AssertFileContent(t, manifestPath, original)
			testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.lock.toml"))
		})
	}
}
