package cli_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestLockChangeReasonsDistinguishRevisionFromContent(t *testing.T) {
	testkit.RequireGit(t)

	for _, changeContent := range []bool{false, true} {
		t.Run(fmt.Sprintf("content_changed=%t", changeContent), func(t *testing.T) {
			root := t.TempDir()
			repo := testkit.InitGitRepository(t, root)
			testkit.WriteFile(t, repo, "AGENTS.md", "original instructions\n")
			firstRevision := testkit.CommitRepository(t, repo, "initial instructions")
			manifest := filepath.Join(root, "daem.toml")
			testkit.WriteFile(t, root, "daem.toml", fmt.Sprintf(`
version = 1
targets = ["codex"]

[instructions.project]
source = { git = %q, path = "AGENTS.md", ref = "main" }
`, filepath.ToSlash(repo)))
			runLockReasonCLI(t, "lock", "--manifest", manifest)
			runLockReasonCLI(t, "apply", "--manifest", manifest, "--yes")
			lockPath := filepath.Join(root, "daem.lock.toml")
			before, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}

			if changeContent {
				testkit.WriteFile(t, repo, "AGENTS.md", "updated instructions\n")
			} else {
				testkit.WriteFile(t, repo, "unselected.txt", "advance revision without changing instructions\n")
			}
			secondRevision := testkit.CommitRepository(t, repo, "advance source")
			wantReasons := "resolved_ref,derivation"
			wantAction := "noop"
			wantExecuted := 0
			wantContent := "original instructions\n"
			if changeContent {
				wantReasons = "resolved_ref,content_hash,derivation"
				wantAction = "update"
				wantExecuted = 1
				wantContent = "updated instructions\n"
			}

			for _, args := range [][]string{
				{"lock", "--dry-run"},
				{"lock", "--dry-run", "--verbose"},
				{"outdated"},
				{"outdated", "--verbose"},
			} {
				output := runLockReasonCLI(t, append(args, "--manifest", manifest)...)
				if !bytes.Contains(output, []byte(" changed="+wantReasons)) {
					t.Fatalf("%v output = %s, want change reasons %q", args, output, wantReasons)
				}
				for _, private := range []string{repo, firstRevision, secondRevision, "sha256:"} {
					if !strings.Contains(strings.Join(args, " "), "--verbose") && bytes.Contains(output, []byte(private)) {
						t.Fatalf("default %v output discloses %q: %s", args, private, output)
					}
				}
			}

			payload := clijson.DecodeLock(t, runLockReasonCLI(t, "lock", "--dry-run", "--json", "--manifest", manifest))
			if payload.ChangeCounts.Changed != 1 {
				t.Fatalf("changed lock entries = %d, want 1", payload.ChangeCounts.Changed)
			}
			found := false
			for _, change := range payload.SubjectChanges {
				if change.Status != "changed" {
					continue
				}
				found = true
				if change.Before == nil || change.After == nil || change.Before.ExactSupply == nil || change.After.ExactSupply == nil {
					t.Fatalf("changed entry lacks exact identities: %+v", change)
				}
				old, new := change.Before.ExactSupply, change.After.ExactSupply
				if old.ResolvedRef != firstRevision || new.ResolvedRef != secondRevision || old.SourceID != new.SourceID || old.Kind != new.Kind {
					t.Fatalf("exact identity revision transition = %+v -> %+v", old, new)
				}
				if (old.ContentHash != new.ContentHash) != changeContent {
					t.Fatalf("content hashes = %q -> %q, content_changed=%t", old.ContentHash, new.ContentHash, changeContent)
				}
			}
			if !found {
				t.Fatal("lock JSON omitted the changed identity")
			}
			afterPreview, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, afterPreview) {
				t.Fatal("dry-run or outdated changed the lockfile")
			}

			written := runLockReasonCLI(t, "lock", "--manifest", manifest)
			if !bytes.Contains(written, []byte(" changed="+wantReasons)) {
				t.Fatalf("lock output = %s, want change reasons %q", written, wantReasons)
			}
			plan := clijson.DecodePlan(t, runLockReasonCLI(t, "status", "--json", "--manifest", manifest))
			if plan.HasErrors || len(plan.Actions) != 1 || plan.Actions[0].Kind != wantAction {
				t.Fatalf("status plan = %+v, want one %s output entry", plan, wantAction)
			}
			result := clijson.DecodeApplyResult(t, runLockReasonCLI(t, "apply", "--yes", "--json", "--manifest", manifest))
			if result.HasErrors || result.ActionCount != wantExecuted {
				t.Fatalf("apply result = %+v, want %d executed actions", result, wantExecuted)
			}
			testkit.AssertFileContent(t, filepath.Join(root, "AGENTS.md"), wantContent)
		})
	}
}

func TestLockContentHashChangeDoesNotPredictNormalizedInstructionWork(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable-class changes require Unix file permissions")
	}

	root := t.TempDir()
	manifest := filepath.Join(root, "daem.toml")
	source := filepath.Join(root, "instructions", "AGENTS.md")
	destination := filepath.Join(root, "AGENTS.md")
	const content = "unchanged instruction bytes\n"
	testkit.WriteFile(t, root, "instructions/AGENTS.md", content)
	if err := os.Chmod(source, 0o644); err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, root, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`)
	runLockReasonCLI(t, "lock", "--manifest", manifest)
	runLockReasonCLI(t, "apply", "--yes", "--manifest", manifest)
	before, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(source, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"lock", "--dry-run"}, {"lock", "--dry-run", "--verbose"}} {
		output := runLockReasonCLI(t, append(args, "--manifest", manifest)...)
		if !bytes.Contains(output, []byte(" changed=content_hash,derivation")) {
			t.Fatalf("mode-only lock output = %s, want content_hash and derivation changes", output)
		}
	}
	runLockReasonCLI(t, "lock", "--manifest", manifest)
	plan := clijson.DecodePlan(t, runLockReasonCLI(t, "apply", "--dry-run", "--json", "--manifest", manifest))
	if plan.HasErrors || len(plan.Actions) != 1 || plan.Actions[0].Kind != "noop" || plan.Actions[0].Reason != "already_current" {
		t.Fatalf("normalized instruction plan = %+v, want one current no-op", plan)
	}
	result := clijson.DecodeApplyResult(t, runLockReasonCLI(t, "apply", "--yes", "--json", "--manifest", manifest))
	if result.HasErrors || result.ActionCount != 0 {
		t.Fatalf("normalized instruction apply = %+v, want zero executed actions", result)
	}
	after, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if after.Mode() != before.Mode() || !after.ModTime().Equal(before.ModTime()) || !os.SameFile(before, after) {
		t.Fatal("mode-only source change rewrote the normalized output")
	}
	testkit.AssertFileContent(t, destination, content)
}

func runLockReasonCLI(t *testing.T, args ...string) []byte {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exit := testkit.RunCLI(args, &stdout, &stderr); exit != 0 || stderr.Len() != 0 {
		t.Fatalf("RunCLI(%v) exit=%d stdout=%q stderr=%q", args, exit, stdout.String(), stderr.String())
	}
	return stdout.Bytes()
}
