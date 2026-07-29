package cli_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestRunApplyDryRunJSONPreservesPlanErrorExitSemantics(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "instructions\n")
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t))

	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
targets = ["codex"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run", "--json"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if strings.Contains(stdout.String(), "next:") {
		t.Fatalf("stdout = %q, want JSON only", stdout.String())
	}

	payload := clijson.DecodePlan(t, stdout.Bytes())
	if !payload.HasErrors {
		t.Fatalf("HasErrors = false, payload = %#v", payload)
	}
	if payload.ActionCount != 1 || payload.Actions[0].Reason != "missing_lock" {
		t.Fatalf("actions = %#v", payload.Actions)
	}
}

func TestRunApplyYesJSONReportsWriteResult(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "managed instructions\n")
	instructionHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))

	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`)
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	payload := clijson.DecodeApplyResult(t, stdout.Bytes())
	if payload.SchemaVersion != 15 || payload.Command != "apply" || payload.Mode != "write" {
		t.Fatalf("payload identity = %#v", payload)
	}
	if payload.ActionCount != 1 || payload.StatefilePath != filepath.Join(tempDir, ".daem", "state.json") {
		t.Fatalf("payload = %#v, want one action and statefile path", payload)
	}
	if payload.HasErrors || len(payload.Errors) != 0 {
		t.Fatalf("payload = %#v, want no errors", payload)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "managed instructions\n")
}

func TestRunApplyYesJSONReportsMutatingFailure(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "managed instructions\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "foreign instructions\n")
	instructionHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))

	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`)
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes", "--json"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	payload := clijson.DecodeApplyResult(t, stdout.Bytes())
	if !payload.HasErrors || len(payload.Errors) != 1 {
		t.Fatalf("payload = %#v, want one error", payload)
	}
	if !strings.Contains(payload.Errors[0].Message, "unmanaged_output_exists") {
		t.Fatalf("payload = %#v, want unmanaged output error", payload)
	}
	if payload.ActionCount != 0 || payload.StatefilePath != filepath.Join(tempDir, ".daem", "state.json") {
		t.Fatalf("payload = %#v, want zero actions and statefile path", payload)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "foreign instructions\n")
}

func TestRunStatusJSONIncludesMissingLockfileWithoutHumanPrefix(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "instructions\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
targets = ["codex"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath, "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if strings.Contains(stdout.String(), "lockfile: missing") {
		t.Fatalf("stdout = %q, want JSON only", stdout.String())
	}

	payload := clijson.DecodePlan(t, stdout.Bytes())
	if !payload.Lockfile.Missing || payload.Lockfile.Path != lockfilePath {
		t.Fatalf("lockfile = %#v", payload.Lockfile)
	}
	if !payload.HasErrors || payload.Actions[0].Reason != "missing_lock" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestRunStatusJSONReportsMissingSelectorLockfileAsError(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: alpha\n---\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill_group]]
source = { path = "skills", mode = "vendor" }
include = ["glob:*"]
targets = ["codex"]
`)

	for _, scenario := range []struct {
		name         string
		args         []string
		wantExitCode int
		wantMode     string
	}{
		{
			name:         "status-json",
			args:         []string{"status", "--manifest", manifestPath, "--json"},
			wantExitCode: 0,
			wantMode:     "status",
		},
		{
			name:         "check-json",
			args:         []string{"status", "--manifest", manifestPath, "--check", "--json"},
			wantExitCode: 1,
			wantMode:     "check",
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(scenario.args, &stdout, &stderr)
			if exitCode != scenario.wantExitCode {
				t.Fatalf("exitCode = %d, want %d; stdout = %q stderr = %q", exitCode, scenario.wantExitCode, stdout.String(), stderr.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}

			payload := clijson.DecodePlan(t, stdout.Bytes())
			if payload.Mode != scenario.wantMode {
				t.Fatalf("mode = %q, want %q", payload.Mode, scenario.wantMode)
			}
			if !payload.Lockfile.Missing || payload.Lockfile.Path != lockfilePath {
				t.Fatalf("lockfile = %#v", payload.Lockfile)
			}
			if !payload.HasErrors {
				t.Fatalf("HasErrors = false, payload = %#v", payload)
			}
			if payload.ActionCount != 0 || len(payload.Actions) != 0 {
				t.Fatalf("actions = %#v, action_count = %d; selector-backed missing lockfile should not invent child actions", payload.Actions, payload.ActionCount)
			}
		})
	}
}

func TestRunStatusJSONIncludesStaleLockWithoutHumanNextStep(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "changed instructions\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
targets = ["codex"]
`)
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: testkit.FixtureHash("sha256:stale")}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath, "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if strings.Contains(stdout.String(), "next:") {
		t.Fatalf("stdout = %q, want JSON only", stdout.String())
	}

	payload := clijson.DecodePlan(t, stdout.Bytes())
	if !payload.HasErrors || payload.Actions[0].Reason != "stale_lock" {
		t.Fatalf("payload = %#v", payload)
	}
}
