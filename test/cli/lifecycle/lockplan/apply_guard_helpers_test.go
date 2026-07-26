package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func writeStaleManagedInstructionFixture(t *testing.T, tempDir string) (string, string) {
	t.Helper()

	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "old instructions\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "old instructions\n")
	oldHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "new instructions\n")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: oldHash}))
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "project", []string{"codex"}, "project", "AGENTS.md", oldHash),
	))
	return manifestPath, lockfilePath
}
