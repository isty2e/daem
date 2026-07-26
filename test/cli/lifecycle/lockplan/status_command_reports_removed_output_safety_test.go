package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunStatusReportsRemovedOutputSafetyStates(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")

	testkit.WriteFile(t, tempDir, "instructions/active.md", "active managed\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "active managed\n")
	testkit.WriteFile(t, tempDir, "CLAUDE.md", "clean managed\n")
	testkit.WriteFile(t, tempDir, "GEMINI.md", "edited managed\n")
	activeHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/active.md"))
	cleanHash := testkit.HashPath(t, filepath.Join(tempDir, "CLAUDE.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.active]
source = "instructions/active.md"

[instructions.active.target.codex]
render_to = "AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "active", SourceID: "local:instructions/active.md?mode=vendor", ContentHash: activeHash}))
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "active", []string{"codex"}, "project", "AGENTS.md", activeHash),
		testkit.InstructionPathState(t, "clean", []string{"claude-code"}, "project", "CLAUDE.md", cleanHash),
		testkit.InstructionPathState(t, "drifted", []string{"antigravity-cli"}, "project", "GEMINI.md", "sha256:state-baseline"),
	))
	stateBefore, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("ReadFile statefile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	output := stdout.String()
	for _, want := range []string{
		"status: 3 actions",
		`noop resource="instructions/active" target=codex scope=project destination="AGENTS.md" mode=copy reason=already_current`,
		`delete resource="instructions/clean" target=claude-code scope=project destination="CLAUDE.md" mode= reason=removed_from_manifest safety=deletable`,
		`error resource="instructions/drifted" target=antigravity-cli scope=project destination="GEMINI.md" mode= reason=drifted_output detail="managed output content differs from statefile baseline" safety=drift_blocked`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}

	stateAfter, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("ReadFile statefile after status returned error: %v", err)
	}
	if string(stateAfter) != string(stateBefore) {
		t.Fatalf("status mutated statefile:\nbefore:\n%s\nafter:\n%s", stateBefore, stateAfter)
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "active managed\n")
	testkit.AssertFileContent(t, filepath.Join(tempDir, "CLAUDE.md"), "clean managed\n")
	testkit.AssertFileContent(t, filepath.Join(tempDir, "GEMINI.md"), "edited managed\n")
}

func TestRunStatusReportsMissingLockfileWithoutWriting(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "instructions\n")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"status: 1 actions",
		`error resource="instructions/project" target=codex scope=project destination="AGENTS.md" mode=copy reason=missing_lock`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	if _, err := os.Stat(filepath.Join(tempDir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("status wrote host output or stat failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempDir, ".daem")); !os.IsNotExist(err) {
		t.Fatalf("status created .daem or stat failed: %v", err)
	}
}

func TestRunStatusReportsStaleInstructionLock(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "changed instructions\n")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: testkit.FixtureHash("sha256:stale")}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `reason=stale_lock`) {
		t.Fatalf("stdout = %q, want stale_lock action", stdout.String())
	}
	if !strings.Contains(stdout.String(), "next: run daem lock --manifest") {
		t.Fatalf("stdout = %q, want stale-lock next step", stdout.String())
	}
	if strings.Contains(stdout.String(), "--lockfile") {
		t.Fatalf("stdout = %q, lockfile must derive from the manifest", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(tempDir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("status wrote host output or stat failed: %v", err)
	}
}

func TestRunStatusDoesNotReportDeleteForDeclaredOutputWithLockError(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		lockfile   lock.File
		wantReason string
	}{
		{
			name:       "missing-lock-entry",
			lockfile:   testkit.ExactSupplyLockfile(t),
			wantReason: "reason=missing_lock",
		},
		{
			name:       "stale-lock-entry",
			lockfile:   testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: testkit.FixtureHash("sha256:stale")}),
			wantReason: "reason=stale_lock",
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			tempDir := t.TempDir()
			manifestPath := filepath.Join(tempDir, "daem.toml")
			lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
			testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "managed instructions\n")
			testkit.WriteFile(t, tempDir, "AGENTS.md", "old managed instructions\n")
			oldHash := testkit.HashPath(t, filepath.Join(tempDir, "AGENTS.md"))

			if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`), 0o600); err != nil {
				t.Fatalf("WriteFile returned error: %v", err)
			}
			testkit.WriteLockfile(t, lockfilePath, scenario.lockfile)
			testkit.WriteStatefile(t, filepath.Join(tempDir, ".daem", "state.json"), testkit.Snapshot(
				t,
				testkit.InstructionPathState(t, "project", []string{"codex"}, "project", "AGENTS.md", oldHash),
			))

			var stdout bytes.Buffer
			var stderr bytes.Buffer

			exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath}, &stdout, &stderr)
			if exitCode != 0 {
				t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
			}
			if !strings.Contains(stdout.String(), "status: 1 actions") {
				t.Fatalf("stdout = %q, want one action", stdout.String())
			}
			if !strings.Contains(stdout.String(), scenario.wantReason) {
				t.Fatalf("stdout = %q, want %s", stdout.String(), scenario.wantReason)
			}
			if strings.Contains(stdout.String(), "delete ") {
				t.Fatalf("stdout = %q, want no false delete for declared resource", stdout.String())
			}
		})
	}
}
