package cli_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestRunLockMissingManifestSuggestsInitDryRun(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "missing.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	assertMissingManifestInitHint(t, stderr.String(), manifestPath)
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunApplyMissingManifestSuggestsInitDryRun(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "missing.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	assertMissingManifestInitHint(t, stderr.String(), manifestPath)
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunListMissingManifestSuggestsInitDryRun(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "missing.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"list", "resources", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	assertMissingManifestInitHint(t, stderr.String(), manifestPath)
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunAddSkillMissingManifestSuggestsInitDryRun(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "missing.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"add", "skill", "https://example.com/repo.git", "--name", "example", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	assertMissingManifestInitHint(t, stderr.String(), manifestPath)
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunApplyMissingLockfileDoesNotSuggestInit(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 || stdout.Len() != 0 {
		t.Fatalf("exitCode = %d, stdout = %q, stderr = %q; want pre-result failure", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "read lockfile") || !strings.Contains(stderr.String(), "next: run daem lock") {
		t.Fatalf("stderr = %q, want missing-lockfile diagnosis and lock remediation", stderr.String())
	}
	if strings.Contains(stderr.String(), "daem init") {
		t.Fatalf("stderr = %q, existing manifest was misclassified as missing", stderr.String())
	}
}

func assertMissingManifestInitHint(t *testing.T, stderr string, manifestPath string) {
	t.Helper()

	if !strings.Contains(stderr, "no such file or directory") {
		t.Fatalf("stderr = %q, want missing file diagnostic", stderr)
	}
	expected := "next: run " + testkit.ExpectedShellCommand(t, "daem", "init", "--manifest", manifestPath, "--dry-run")
	if !strings.Contains(stderr, expected) {
		t.Fatalf("stderr = %q, want %q", stderr, expected)
	}
}
