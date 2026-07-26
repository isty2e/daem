package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/doctorenv"
)

func TestRunDoctorDefaultTargetsReportCapabilityMatrixOnce(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"doctor"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	for _, target := range []string{"codex", "claude-code", "opencode", "pi"} {
		token := "target=" + target + " capability=instructions"
		if count := strings.Count(stdout.String(), token); count != 1 {
			t.Fatalf("%s count = %d, stdout = %q", token, count, stdout.String())
		}
	}
}

func TestRunDoctorRejectsUnknownTarget(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--target", "unknown"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "unknown target") {
		t.Fatalf("stderr = %q, want unknown target diagnostic", stderr.String())
	}
	if !strings.Contains(stderr.String(), "accepted targets: codex, claude-code, opencode, pi") {
		t.Fatalf("stderr = %q, want accepted target values", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunDoctorReportsExplicitManifestParseError(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	manifestPath := filepath.Join(tempDir, "daem.toml")
	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte("version = \"wrong\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("HOME", homeDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--manifest", manifestPath, "--target", "codex"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "error manifest") {
		t.Fatalf("stdout = %q, want manifest error", stdout.String())
	}
}
