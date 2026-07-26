package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/doctorenv"
)

const (
	hookDiagnosticMissingTimeout = "hook.command.missing_timeout"
	hookDiagnosticShellSyntax    = "hook.command.shell_syntax"
	hookDiagnosticInterpreter    = "hook.command.broad_interpreter"
	hookDiagnosticLookup         = "hook.command.lookup_ambiguous"
	hookDiagnosticCodexTrust     = "hook.codex.trust_review_required"
)

func TestRunApplyDryRunReportsHookCommandDiagnosticsWithoutMutating(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t))
	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["codex", "claude-code"]

[[hook]]
name = "protect-env"
event = "PreToolUse"
matcher = "^Bash$"
command = "python3 hooks/protect.py | tee logs/protect.log"
targets = ["codex", "claude-code"]

[[hook.target_override]]
target = "claude-code"
matcher = "Bash"
if = "tool_name == 'Bash'"
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	output := stdout.String()
	for _, want := range []string{
		"diagnostics: 9",
		hookDiagnosticMissingTimeout,
		hookDiagnosticShellSyntax,
		hookDiagnosticInterpreter,
		hookDiagnosticLookup,
		hookDiagnosticCodexTrust,
		`command="python3 hooks/protect.py | tee logs/protect.log"`,
		`target=codex`,
		`target=claude-code`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
	for _, path := range []string{
		filepath.Join(tempDir, ".codex", "hooks.json"),
		filepath.Join(tempDir, ".claude", "settings.json"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("dry-run created %q or stat failed: %v", path, err)
		}
	}
}

func TestRunApplyDryRunJSONIncludesHookCommandDiagnostics(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t))
	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["codex"]

[[hook]]
name = "protect-env"
event = "PreToolUse"
matcher = "^Bash$"
command = "python3 hooks/protect.py | tee logs/protect.log"
targets = ["codex"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if strings.Contains(stdout.String(), "diagnostics:") {
		t.Fatalf("stdout = %q, want JSON without human diagnostics", stdout.String())
	}

	payload := clijson.DecodePlan(t, stdout.Bytes())
	if len(payload.Diagnostics) != 5 {
		t.Fatalf("diagnostics = %#v, want 5 codex diagnostics", payload.Diagnostics)
	}
	assertPlanDiagnostic(t, payload, hookDiagnosticMissingTimeout, "warn", "hook/protect-env", "codex")
	assertPlanDiagnostic(t, payload, hookDiagnosticShellSyntax, "warn", "hook/protect-env", "codex")
	assertPlanDiagnostic(t, payload, hookDiagnosticInterpreter, "warn", "hook/protect-env", "codex")
	assertPlanDiagnostic(t, payload, hookDiagnosticLookup, "warn", "hook/protect-env", "codex")
	assertPlanDiagnostic(t, payload, hookDiagnosticCodexTrust, "warn", "hook/protect-env", "codex")
	for _, diagnostic := range payload.Diagnostics {
		if diagnostic.Command != "python3 hooks/protect.py | tee logs/protect.log" || diagnostic.Event != "PreToolUse" {
			t.Fatalf("diagnostic = %#v, want visible command and event", diagnostic)
		}
	}
}

func TestRunDoctorReportsHookCommandDiagnosticsAsWarnings(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	manifestPath := filepath.Join(tempDir, "daem.toml")
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")
	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["codex"]

[[hook]]
name = "protect-env"
event = "Stop"
command = "make test"
targets = ["codex"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--manifest", manifestPath, "--target", "codex"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}

	output := stdout.String()
	for _, want := range []string{
		"warn target=codex resource=hook/protect-env diagnostic=hook.command.missing_timeout",
		"warn target=codex resource=hook/protect-env diagnostic=hook.command.lookup_ambiguous",
		"warn target=codex resource=hook/protect-env diagnostic=hook.codex.trust_review_required",
		`command=\"make test\"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
	if strings.Contains(output, hookDiagnosticInterpreter) || strings.Contains(output, hookDiagnosticShellSyntax) {
		t.Fatalf("stdout = %q, did not want interpreter or shell syntax warnings for make test", output)
	}
}

func assertPlanDiagnostic(
	t *testing.T,
	payload clijson.Plan,
	code string,
	severity string,
	resourceID string,
	target string,
) {
	t.Helper()

	_ = clijson.FindPlanDiagnostic(t, payload, code, severity, resourceID, target)
}
