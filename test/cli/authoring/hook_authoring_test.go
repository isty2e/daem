package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
)

func TestRunAddHookDryRunPlansCommandHookWithoutWrites(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := "version = 1\ntargets = [\"codex\", \"claude-code\"]\n"
	testkit.WriteFile(t, tempDir, "daem.toml", original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "hook", "protect-env", "PostToolUse", "python3 hooks/protect.py",
		"--manifest", manifestPath,
		"--matcher", "Write",
		"--timeout", "7s",
		"--target", "codex",
		"--target", "claude-code",
		"--scope", "global",
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"add: hook/protect-env",
		"change: append hook resource",
		"[[hook]]",
		`event = "PostToolUse"`,
		`matcher = "Write"`,
		`command = "python3 hooks/protect.py"`,
		`timeout = 7`,
		`targets = ["codex", "claude-code"]`,
		`scope = "global"`,
		"lockfile: would write " + filepath.Join(tempDir, "daem.lock.toml"),
		"next: rerun this authoring command without --dry-run",
		"note: add updates the manifest and lockfile only; host hook config changes only when apply reconciles managed hook aggregates",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, manifestPath, original)
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.lock.toml"))
	testkit.AssertPathMissing(t, filepath.Join(tempDir, ".codex", "hooks.json"))
}

func TestRunAddHookYesWritesManifestOnlyWithLockOnlyWarnings(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	hostPath := filepath.Join(tempDir, ".config", "opencode", "opencode.json")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"opencode\", \"pi\"]\n")
	testkit.WriteFile(t, tempDir, "daem.lock.toml", "lock stays\n")
	testkit.WriteFile(t, filepath.Dir(hostPath), filepath.Base(hostPath), "{}\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "hook", "notify", "pre_apply", "python3 hooks/notify.py",
		"--manifest", manifestPath,
		"--target", "opencode",
		"--target", "pi",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"added: hook/notify",
		"lockfile: wrote " + lockfilePath,
		`warning: target "opencode": command hook reconciliation requires an extension bridge surface; hook remains lock-only until apply/status support exists`,
		`warning: target "pi": command hook reconciliation requires an extension bridge surface; hook remains lock-only until apply/status support exists`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	lockfileContent, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("ReadFile lockfile returned error: %v", err)
	}
	if !strings.Contains(string(lockfileContent), "[locked]") {
		t.Fatalf("lockfile = %s, want regenerated exact lockfile", lockfileContent)
	}
	testkit.AssertFileContent(t, hostPath, "{}\n")

	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("Parse returned error: %v\n%s", err, content)
	}
	if len(config.Hooks()) != 1 || config.Hooks()[0].ID().Name() != "notify" || len(config.Hooks()[0].Targets()) != 2 {
		t.Fatalf("Hooks = %#v, want opencode/pi hook", config.Hooks())
	}
}

func TestRunAddHookRejectsManifestOnlyFlags(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := "version = 1\ntargets = [\"codex\"]\n"
	testkit.WriteFile(t, tempDir, "daem.toml", original)

	for _, removedFlag := range []string{"--status-message", "--target-override", "--event", "--command"} {
		t.Run(removedFlag, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI([]string{
				"add", "hook", "conditional", "PostToolUse", "python3 hooks/protect.py",
				"--manifest", manifestPath,
				removedFlag, "value",
				"--dry-run",
			}, &stdout, &stderr)
			if exitCode != 2 || !strings.Contains(stderr.String(), "flag provided but not defined") {
				t.Fatalf("exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			testkit.AssertFileContent(t, manifestPath, original)
		})
	}
}

func TestRunAddHookDryRunDiffShowsResultingManifestDelta(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := "version = 1\ntargets = [\"codex\"]\n"
	testkit.WriteFile(t, tempDir, "daem.toml", original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "hook", "protect-env", "PreToolUse", "python3 hooks/protect.py",
		"--manifest", manifestPath,
		"--matcher", "Bash",
		"--target", "codex",
		"--dry-run",
		"--diff",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"manifest diff:",
		"--- " + manifestPath,
		"+++ " + manifestPath,
		"+[[hook]]",
		`+name = "protect-env"`,
		`+event = "PreToolUse"`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, manifestPath, original)
}

func TestRunRemoveHookDryRunDiffShowsResultingManifestDelta(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := `
version = 1
targets = ["codex"]

[[hook]]
name = "protect-env"
event = "PreToolUse"
matcher = "Bash"
command = "python3 hooks/protect.py"
targets = ["codex"]
`
	testkit.WriteFile(t, tempDir, "daem.toml", original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"remove", "hook", "protect-env", "--manifest", manifestPath, "--dry-run", "--diff"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"manifest diff:",
		"--- " + manifestPath,
		"+++ " + manifestPath,
		"-[[hook]]",
		`-name = "protect-env"`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, manifestPath, original)
}
