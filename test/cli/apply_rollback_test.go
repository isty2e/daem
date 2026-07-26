package cli_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"

	"github.com/isty2e/daem/internal/target"
)

func TestRunApplyRollsBackHostWritesWhenLaterWriteFails(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	homeDir := filepath.Join(tempDir, "home")
	blockedDir := filepath.Join(homeDir, ".claude")
	t.Setenv("HOME", homeDir)
	testkit.WriteFile(t, tempDir, "instructions/alpha.md", "alpha instructions\n")
	testkit.WriteFile(t, tempDir, "instructions/beta.md", "beta instructions\n")
	betaSourcePath := filepath.Join(tempDir, "instructions/beta.md")
	alphaHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/alpha.md"))
	betaHash := testkit.HashPath(t, betaSourcePath)

	if err := os.MkdirAll(blockedDir, 0o700); err != nil {
		t.Fatalf("MkdirAll blockedDir returned error: %v", err)
	}
	if err := os.Chmod(blockedDir, 0o500); err != nil {
		t.Fatalf("Chmod blockedDir returned error: %v", err)
	}
	defer os.Chmod(blockedDir, 0o700)

	if err := os.WriteFile(manifestPath, fmt.Appendf(nil, `
version = 1
targets = ["codex", "claude-code"]

[instructions.alpha]
source = "instructions/alpha.md"
targets = ["codex"]

[instructions.alpha.target.codex]
render_to = "AGENTS.md"

[instructions.beta]
source = %q
targets = ["claude-code"]
scope = "global"

[instructions.beta.target.claude-code]
render_to = "CLAUDE.md"
`, betaSourcePath), 0o600); err != nil {
		t.Fatalf("WriteFile manifest returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(
		t,
		testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "alpha", SourceID: "local:instructions/alpha.md?mode=vendor", ContentHash: alphaHash},
		testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "beta", SourceID: "local:" + filepath.ToSlash(betaSourcePath) + "?mode=vendor", ContentHash: betaHash, Targets: []target.Target{target.TargetClaudeCode}, Scope: target.ScopeGlobal},
	))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `write destination "~/.claude/CLAUDE.md"`) {
		t.Fatalf("stderr = %q, want blocked write diagnostic", stderr.String())
	}
	if !strings.Contains(stderr.String(), "host changes rolled back") {
		t.Fatalf("stderr = %q, want rollback confirmation", stderr.String())
	}

	if _, err := os.Stat(filepath.Join(tempDir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("first host write was not rolled back or stat failed: %v", err)
	}
	if err := os.Chmod(blockedDir, 0o700); err != nil {
		t.Fatalf("Chmod blockedDir after apply returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(blockedDir, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("failed destination was created or stat failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempDir, ".daem", "state.json")); !os.IsNotExist(err) {
		t.Fatalf("failed apply wrote statefile or stat failed: %v", err)
	}
	assertNoApplyRollbackTempDirs(t, tempDir)
}

func TestRunApplyRollsBackUpdatedHostFileWhenLaterWriteFails(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	homeDir := filepath.Join(tempDir, "home")
	blockedDir := filepath.Join(homeDir, ".claude")
	t.Setenv("HOME", homeDir)
	testkit.WriteFile(t, tempDir, "instructions/alpha.md", "new alpha instructions\n")
	testkit.WriteFile(t, tempDir, "instructions/beta.md", "beta instructions\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "old alpha instructions\n")
	betaSourcePath := filepath.Join(tempDir, "instructions/beta.md")
	alphaHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/alpha.md"))
	betaHash := testkit.HashPath(t, betaSourcePath)
	oldAlphaHash := testkit.HashPath(t, filepath.Join(tempDir, "AGENTS.md"))

	if err := os.MkdirAll(blockedDir, 0o700); err != nil {
		t.Fatalf("MkdirAll blockedDir returned error: %v", err)
	}
	if err := os.Chmod(blockedDir, 0o500); err != nil {
		t.Fatalf("Chmod blockedDir returned error: %v", err)
	}
	defer os.Chmod(blockedDir, 0o700)

	if err := os.WriteFile(manifestPath, fmt.Appendf(nil, `
version = 1
targets = ["codex", "claude-code"]

[instructions.alpha]
source = "instructions/alpha.md"
targets = ["codex"]

[instructions.alpha.target.codex]
render_to = "AGENTS.md"

[instructions.beta]
source = %q
targets = ["claude-code"]
scope = "global"

[instructions.beta.target.claude-code]
render_to = "CLAUDE.md"
`, betaSourcePath), 0o600); err != nil {
		t.Fatalf("WriteFile manifest returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(
		t,
		testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "alpha", SourceID: "local:instructions/alpha.md?mode=vendor", ContentHash: alphaHash},
		testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "beta", SourceID: "local:" + filepath.ToSlash(betaSourcePath) + "?mode=vendor", ContentHash: betaHash, Targets: []target.Target{target.TargetClaudeCode}, Scope: target.ScopeGlobal},
	))
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "alpha", []string{"codex"}, "project", "AGENTS.md", oldAlphaHash),
	))
	stateBefore, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("ReadFile statefile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "host changes rolled back") {
		t.Fatalf("stderr = %q, want rollback confirmation", stderr.String())
	}

	if err := os.Chmod(blockedDir, 0o700); err != nil {
		t.Fatalf("Chmod blockedDir after apply returned error: %v", err)
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "old alpha instructions\n")
	stateAfter, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("ReadFile statefile after apply returned error: %v", err)
	}
	if string(stateAfter) != string(stateBefore) {
		t.Fatalf("failed apply mutated statefile:\nbefore:\n%s\nafter:\n%s", stateBefore, stateAfter)
	}
	assertNoApplyRollbackTempDirs(t, tempDir)
}

func TestRunApplyRollsBackDeletedHostFileWhenLaterWriteFails(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	homeDir := filepath.Join(tempDir, "home")
	blockedDir := filepath.Join(homeDir, ".claude")
	t.Setenv("HOME", homeDir)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "removed instructions\n")
	testkit.WriteFile(t, tempDir, "instructions/beta.md", "beta instructions\n")
	betaSourcePath := filepath.Join(tempDir, "instructions/beta.md")
	removedHash := testkit.HashPath(t, filepath.Join(tempDir, "AGENTS.md"))
	betaHash := testkit.HashPath(t, betaSourcePath)

	if err := os.MkdirAll(blockedDir, 0o700); err != nil {
		t.Fatalf("MkdirAll blockedDir returned error: %v", err)
	}
	if err := os.Chmod(blockedDir, 0o500); err != nil {
		t.Fatalf("Chmod blockedDir returned error: %v", err)
	}
	defer os.Chmod(blockedDir, 0o700)

	if err := os.WriteFile(manifestPath, fmt.Appendf(nil, `
version = 1
targets = ["claude-code"]

[instructions.beta]
source = %q
targets = ["claude-code"]
scope = "global"

[instructions.beta.target.claude-code]
render_to = "CLAUDE.md"
`, betaSourcePath), 0o600); err != nil {
		t.Fatalf("WriteFile manifest returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "beta", SourceID: "local:" + filepath.ToSlash(betaSourcePath) + "?mode=vendor", ContentHash: betaHash, Targets: []target.Target{target.TargetClaudeCode}, Scope: target.ScopeGlobal}))
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "removed", []string{"codex"}, "project", "AGENTS.md", removedHash),
	))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "host changes rolled back") {
		t.Fatalf("stderr = %q, want rollback confirmation", stderr.String())
	}

	if err := os.Chmod(blockedDir, 0o700); err != nil {
		t.Fatalf("Chmod blockedDir after apply returned error: %v", err)
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "removed instructions\n")
	if _, err := os.Stat(filepath.Join(tempDir, ".daem", "state.json")); err != nil {
		t.Fatalf("statefile disappeared after failed apply: %v", err)
	}
	assertNoApplyRollbackTempDirs(t, tempDir)
}

func assertNoApplyRollbackTempDirs(t *testing.T, root string) {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(root, ".daem"))
	if err != nil {
		t.Fatalf("ReadDir .daem returned error: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".apply-rollback.") {
			t.Fatalf("apply rollback temp directory was not cleaned up: %s", entry.Name())
		}
	}
}
