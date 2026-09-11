package cli_test

import (
	"bytes"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
)

func TestGitInstructionManifestRefRefresh(t *testing.T) {
	testkit.RequireGit(t)
	for _, pinned := range []bool{false, true} {
		t.Run(fmt.Sprintf("pinned=%t", pinned), func(t *testing.T) {
			root := t.TempDir()
			repo := testkit.InitGitRepository(t, root)
			testkit.WriteFile(t, repo, "AGENTS.md", "first guidance\n")
			first := testkit.CommitRepository(t, repo, "first")
			ref := "main"
			if pinned {
				ref = first
			}
			manifest := filepath.Join(root, "daem.toml")
			testkit.WriteFile(t, root, "daem.toml", fmt.Sprintf("version = 1\ntargets = [\"codex\"]\n[instructions.project]\nsource = { git = %q, path = \"AGENTS.md\", ref = %q }\n", repo, ref))
			testkit.WithWorkingDirectory(t, root)
			run := func(args ...string) {
				t.Helper()
				var stdout, stderr bytes.Buffer
				if exit := testkit.RunVerboseCLI(args, &stdout, &stderr); exit != 0 {
					t.Fatalf("%v: exit %d, stderr %s, stdout %s", args, exit, stderr.String(), stdout.String())
				}
			}
			run("lock", "--manifest", manifest)
			run("apply", "--manifest", manifest, "--yes")
			testkit.AssertFileContent(t, filepath.Join(root, "AGENTS.md"), "first guidance\n")
			testkit.WriteFile(t, repo, "AGENTS.md", "second guidance\n")
			second := testkit.CommitRepository(t, repo, "second")
			run("lock", "--manifest", manifest)
			locked, err := lockfile.Load(t.Context(), filepath.Join(root, "daem.lock.toml"))
			if err != nil {
				t.Fatal(err)
			}
			wantCommit, wantContent := second, "second guidance\n"
			if pinned {
				wantCommit, wantContent = first, "first guidance\n"
			}
			instructions := testkit.LockedInstructions(t, locked)
			if len(instructions) != 1 || instructions[0].ResolvedRef != wantCommit {
				t.Fatalf("instructions=%#v, want %s", instructions, wantCommit)
			}
			run("apply", "--manifest", manifest, "--yes")
			testkit.AssertFileContent(t, filepath.Join(root, "AGENTS.md"), wantContent)
		})
	}
}

func TestRunAddInstructionGitFlagsBeforeOperands(t *testing.T) {
	testkit.RequireGit(t)
	root := t.TempDir()
	repo := testkit.InitGitRepository(t, root)
	testkit.WriteFile(t, repo, "AGENTS.md", "guidance\n")
	testkit.CommitRepository(t, repo, "guide")
	manifest := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	var stdout, stderr bytes.Buffer
	if exit := testkit.RunVerboseCLI([]string{"add", "instruction", "--path=AGENTS.md", "--ref", "main", "--manifest", manifest, "project", repo, "--dry-run"}, &stdout, &stderr); exit != 0 {
		t.Fatalf("exit %d, stderr %s", exit, stderr.String())
	}
	testkit.AssertPathMissing(t, filepath.Join(root, "daem.lock.toml"))
}
