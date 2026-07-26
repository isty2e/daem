package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAddHookRejectsAntigravityCLIWithoutMutation(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	original := "version = 1\ntargets = [\"codex\"]\n"
	writeAntigravityHookCLIFile(t, manifestPath, original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithoutTerminal([]string{
		"add",
		"hook",
		"protect-env",
		"PreToolUse",
		"python3 hooks/protect.py",
		"--manifest", manifestPath,
		"--target", "antigravity-cli",
		"--dry-run",
	}, &stdout, &stderr)

	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{
		`target "antigravity-cli"`,
		"cannot author Antigravity CLI command hooks",
		"not TP04-07 admitted",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
	assertAntigravityHookCLIFileContent(t, manifestPath, original)
	assertAntigravityHookCLIPathMissing(t, lockfilePath)
}

func TestRunRemoveHookCanCleanAntigravityCLITargetWithoutWritingDryRun(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := `version = 1
targets = ["codex", "antigravity-cli"]

[[hook]]
name = "protect-env"
event = "PreToolUse"
command = "python3 hooks/protect.py"
targets = ["codex", "antigravity-cli"]
`
	writeAntigravityHookCLIFile(t, manifestPath, original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithoutTerminal([]string{
		"remove",
		"hook",
		"protect-env",
		"--manifest", manifestPath,
		"--target", "antigravity-cli",
		"--dry-run",
		"--diff",
	}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"update hook targets",
		`-targets = ["codex", "antigravity-cli"]`,
		`+targets = ["codex"]`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	assertAntigravityHookCLIFileContent(t, manifestPath, original)
}

func writeAntigravityHookCLIFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func assertAntigravityHookCLIFileContent(t *testing.T, path string, want string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(content) != want {
		t.Fatalf("content = %q, want %q", string(content), want)
	}
}

func assertAntigravityHookCLIPathMissing(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); err == nil {
		t.Fatalf("path %q exists, want missing", path)
	} else if !os.IsNotExist(err) {
		t.Fatalf("Stat returned error: %v", err)
	}
}
