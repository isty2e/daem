package cli_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestStatusHumanAndJSONSharePlanOwnedMixedDecisionOrder(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, "instructions/AGENTS.md", "managed instructions\n")
	testkit.WriteFile(t, root, "daem.toml", `
version = 1
targets = ["codex", "claude-code"]

[instructions.project]
source = "instructions/AGENTS.md"
targets = ["codex"]

[[hook]]
name = "test"
event = "Stop"
command = "make test"
targets = ["claude-code"]
`)

	runInstructionModeCLI(t, []string{"lock", "--manifest", manifestPath}, 0)
	human := runInstructionModeCLI(t, []string{"status", "--manifest", manifestPath}, 0)
	testkit.AssertContainsInOrder(
		t,
		human,
		`create resource="instructions/project"`,
		`create resource="hook/test"`,
	)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{"status", "--manifest", manifestPath, "--json"},
		&stdout,
		&stderr,
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("status --json exit = %d; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	payload := clijson.DecodePlan(t, stdout.Bytes())
	if len(payload.Actions) != 2 {
		t.Fatalf("actions = %#v, want two mixed decisions", payload.Actions)
	}
	if payload.Actions[0].ResourceID != "instructions/project" ||
		payload.Actions[1].ResourceID != "hook/test" {
		t.Fatalf("actions = %#v, want the same plan-owned mixed order as human output", payload.Actions)
	}
}
