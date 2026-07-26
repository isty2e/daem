package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/test/testkit"
)

func TestExecutableInstructionSourceMaterializesPrivateNonExecutableOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable-bit identity is not available on Windows")
	}

	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	lockfilePath := filepath.Join(root, "daem.lock.toml")
	sourcePath := filepath.Join(root, "instructions", "AGENTS.md")
	destinationPath := filepath.Join(root, "AGENTS.md")
	content := []byte("executable source instructions\n")

	testkit.WriteFile(t, root, "instructions/AGENTS.md", string(content))
	if err := os.Chmod(sourcePath, 0o700); err != nil {
		t.Fatalf("Chmod source returned error: %v", err)
	}
	testkit.WriteFile(t, root, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`)

	runInstructionModeCLI(t, []string{"lock", "--manifest", manifestPath}, 0)
	locked, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	entityID, err := entity.New(entity.KindInstructions, "project")
	if err != nil {
		t.Fatalf("entity.New returned error: %v", err)
	}
	contract, ok := locked.Locked.ExactSupplySubject(entityID)
	if !ok {
		t.Fatal("Instructions exact-Supply subject is missing")
	}
	rawIdentity, ok := contract.ExactSupply()
	if !ok {
		t.Fatal("Instructions raw exact-Supply identity is missing")
	}
	materializedIdentity, ok := contract.MaterializedFileIdentity()
	if !ok {
		t.Fatal("Instructions materialized file identity is missing")
	}
	if rawIdentity.Equal(materializedIdentity) {
		t.Fatal("executable raw Supply identity was not separated from non-executable materialization")
	}
	if materializedIdentity.ContentHash() != artifact.HashFileContent(content) {
		t.Fatalf("materialized hash = %q, want non-executable content identity", materializedIdentity.ContentHash())
	}

	runInstructionModeCLI(t, []string{"apply", "--manifest", manifestPath, "--yes"}, 0)
	assertInstructionMode(t, destinationPath, 0o600)
	testkit.AssertFileContent(t, destinationPath, string(content))

	if err := os.Chmod(destinationPath, 0o644); err != nil {
		t.Fatalf("Chmod destination to 0644 returned error: %v", err)
	}
	stdout := runInstructionModeCLI(t, []string{"status", "--manifest", manifestPath}, 0)
	if !strings.Contains(stdout, `noop resource="instructions/project"`) {
		t.Fatalf("status stdout = %q, want non-executable permission difference to remain current", stdout)
	}

	if err := os.Chmod(destinationPath, 0o700); err != nil {
		t.Fatalf("Chmod destination to 0700 returned error: %v", err)
	}
	stdout = runInstructionModeCLI(t, []string{"apply", "--manifest", manifestPath, "--yes"}, 1)
	if !strings.Contains(stdout, "drifted_output") {
		t.Fatalf("apply output = %q, want executable-bit drift rejection", stdout)
	}
	assertInstructionMode(t, destinationPath, 0o700)
	testkit.AssertNoRecoveryArtifacts(t, root)
}

func TestManagedInstructionCopyToSymlinkChangeCannotCollapseToNoOp(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	destinationPath := filepath.Join(root, "AGENTS.md")
	statePath := filepath.Join(root, ".daem", "state.json")
	testkit.WriteFile(t, root, "instructions/AGENTS.md", "managed instructions\n")
	testkit.WriteFile(t, root, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`)

	runInstructionModeCLI(t, []string{"lock", "--manifest", manifestPath}, 0)
	runInstructionModeCLI(t, []string{"apply", "--manifest", manifestPath, "--yes"}, 0)
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state before mode change: %v", err)
	}
	testkit.WriteFile(t, root, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"

[instructions.project.target.codex]
mode = "symlink"
`)
	runInstructionModeCLI(t, []string{"lock", "--manifest", manifestPath}, 0)

	status := runInstructionModeCLI(t, []string{"status", "--manifest", manifestPath}, 0)
	for _, want := range []string{
		`update resource="instructions/project"`,
		`mode=symlink`,
		`current path kind cannot satisfy the desired placement mode`,
	} {
		if !strings.Contains(status, want) {
			t.Fatalf("status = %q, want %q", status, want)
		}
	}
	for _, args := range [][]string{
		{"apply", "--manifest", manifestPath, "--dry-run"},
		{"apply", "--manifest", manifestPath, "--yes"},
	} {
		output := runInstructionModeCLI(t, args, 1)
		if !strings.Contains(output, `apply symlink mode for "AGENTS.md" is not implemented`) {
			t.Fatalf("apply output = %q, want explicit symlink refusal", output)
		}
	}

	testkit.AssertFileContent(t, destinationPath, "managed instructions\n")
	stateAfter, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state after refused mode change: %v", err)
	}
	if !bytes.Equal(stateAfter, stateBefore) {
		t.Fatal("refused mode change mutated managed state")
	}
	testkit.AssertNoRecoveryArtifacts(t, root)
}

func runInstructionModeCLI(t *testing.T, args []string, wantExit int) string {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
	if exitCode != wantExit {
		t.Fatalf("RunVerboseCLI(%v) exit = %d, want %d; stdout=%q stderr=%q", args, exitCode, wantExit, stdout.String(), stderr.String())
	}
	return stdout.String() + stderr.String()
}

func assertInstructionMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat %q returned error: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode(%q) = %04o, want %04o", path, got, want)
	}
}
