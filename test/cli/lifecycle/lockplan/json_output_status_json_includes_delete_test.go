package cli_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestRunStatusJSONIncludesDeleteSafetyAndPreviousState(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "instructions\n")
	testkit.WriteFile(t, tempDir, "CLAUDE.md", "old\n")
	instructionHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))
	removedHash := testkit.HashPath(t, filepath.Join(tempDir, "CLAUDE.md"))

	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
targets = ["codex"]
`)
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash}))
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "removed", []string{"claude-code"}, "project", "CLAUDE.md", removedHash),
	))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath, "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	payload := clijson.DecodePlan(t, stdout.Bytes())
	var deleteActionFound bool
	for _, action := range payload.Actions {
		if action.Kind != "delete" {
			continue
		}
		deleteActionFound = true
		if action.Safety != "deletable" {
			t.Fatalf("delete action safety = %q, want deletable", action.Safety)
		}
		if action.PreviousState == nil || action.PreviousState.ResourceID != "instructions/removed" || action.PreviousState.ContentHash != removedHash {
			t.Fatalf("previous_state = %#v", action.PreviousState)
		}
	}
	if !deleteActionFound {
		t.Fatalf("actions = %#v, want delete action", payload.Actions)
	}
}

func TestRunApplyJSONFlagValidation(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "json requires explicit non-interactive mode", args: []string{"apply", "--json"}, want: "--json requires --dry-run or --yes"},
		{name: "json excludes diff", args: []string{"apply", "--dry-run", "--json", "--diff"}, want: "--json and --diff are mutually exclusive"},
	}

	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(scenario.args, &stdout, &stderr)
			if exitCode != 2 {
				t.Fatalf("exitCode = %d, want 2", exitCode)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), scenario.want) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), scenario.want)
			}
		})
	}
}
