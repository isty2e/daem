package cli_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/execcheck"
)

func TestRunStatusAndDryRunExposeManagedClaudeCarrierRemoval(t *testing.T) {
	fixture := writeCLIClaudeGlobalExtensionCarrierLockFixture(t)
	writeCLIClaudeGlobalManagedCarrierState(t, fixture)
	configRoot := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configRoot)
	testkit.WriteFile(
		t,
		configRoot,
		"plugins/installed_plugins.json",
		`{"version":2,"plugins":{"context7@market":[{"scope":"user"}]}}`,
	)
	statefilePath := filepath.Join(fixture.root, ".daem", "state.json")
	stateBefore := testkit.ReadFile(t, statefilePath)
	canary := execcheck.New(t, "claude")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"remove", "extension", "context7-global",
		"--manifest", fixture.manifestPath,
		"--target", "claude-code",
		"--scope", "global",
		"--json",
	}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("remove exitCode = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{
		"status",
		"--manifest", fixture.manifestPath,
		"--target", "claude-code",
		"--check",
		"--json",
	}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("status exitCode = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	payload := clijson.DecodePlan(t, stdout.Bytes())
	if payload.HasErrors {
		t.Fatalf("status payload = %#v, want admitted removal", payload)
	}
	assertManagedClaudeCarrierRemoval(t, payload.CarrierAbsences)

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{
		"apply",
		"--manifest", fixture.manifestPath,
		"--target", "claude-code",
		"--dry-run",
		"--json",
	}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("dry-run apply exitCode = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	dryRunPayload := clijson.DecodePlan(t, stdout.Bytes())
	if dryRunPayload.HasErrors {
		t.Fatalf("dry-run apply payload = %#v, want admitted removal", dryRunPayload)
	}
	assertManagedClaudeCarrierRemoval(t, dryRunPayload.CarrierAbsences)
	if !bytes.Equal(testkit.ReadFile(t, statefilePath), stateBefore) {
		t.Fatal("dry-run apply changed durable state")
	}
	execcheck.AssertClean(t, canary, "status and dry-run after managed carrier omission")
}
