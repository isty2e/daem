package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

type statusCheckFixtureInput struct {
	sourceContent string
	hostContent   string
	writeLockfile bool
}
type statusCheckFixture struct {
	manifestPath string
	lockfilePath string
}

func writeStatusCheckFixture(t *testing.T, input statusCheckFixtureInput) statusCheckFixture {
	t.Helper()

	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	sourcePath := filepath.Join(tempDir, "instructions/AGENTS.md")
	hostPath := filepath.Join(tempDir, "AGENTS.md")

	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", input.sourceContent)
	testkit.WriteFile(t, tempDir, "AGENTS.md", input.hostContent)
	sourceHash := testkit.HashPath(t, sourcePath)
	stateHash := testkit.HashPath(t, hostPath)

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"

[instructions.project.target.codex]
render_to = "AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if input.writeLockfile {
		testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: sourceHash}))
	}
	testkit.WriteStatefile(t, filepath.Join(tempDir, ".daem", "state.json"), testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "project", []string{"codex"}, "project", "AGENTS.md", stateHash),
	))
	if _, err := os.Stat(hostPath); err != nil {
		t.Fatalf("host file setup failed: %v", err)
	}

	return statusCheckFixture{
		manifestPath: manifestPath,
		lockfilePath: lockfilePath,
	}
}
