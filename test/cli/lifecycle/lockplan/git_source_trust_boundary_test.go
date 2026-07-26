package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunLockRejectsGitOptionInjectionBeforeProcessLaunch(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	markerPath := filepath.Join(tempDir, "git-invoked")
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	secret := "synthetic-secret"

	testkit.WriteFile(t, binDir, "git", "#!/bin/sh\nprintf invoked > \"$DAEM_GIT_MARKER\"\nexit 99\n")
	if err := os.Chmod(filepath.Join(binDir, "git"), 0o700); err != nil {
		t.Fatalf("Chmod fake git returned error: %v", err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("DAEM_GIT_MARKER", markerPath)

	manifest := `
version = 1
targets = ["codex"]

[[skill]]
name = "demo"
source = { git = "https://example.com/repo.git", path = ".", ref = "--upload-pack=` + secret + `" }
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatalf("WriteFile manifest returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{"lock", "--manifest", manifestPath},
		&stdout,
		&stderr,
	)
	if exitCode == 0 {
		t.Fatalf("exitCode = 0, stdout = %q", stdout.String())
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("fake git was launched or marker stat failed: %v", err)
	}
	if _, err := os.Stat(lockfilePath); !os.IsNotExist(err) {
		t.Fatalf("lockfile was written or stat failed: %v", err)
	}
	combined := stdout.String() + stderr.String()
	if strings.Contains(combined, secret) {
		t.Fatalf("CLI output disclosed rejected ref: %q", combined)
	}
	if !strings.Contains(combined, "must not begin with an option prefix") {
		t.Fatalf("CLI output = %q, want rejection class", combined)
	}
}

func TestRunAddSkillRejectsWhitespaceRefWithoutRepairOrProcessLaunch(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	markerPath := filepath.Join(tempDir, "git-invoked")
	manifestPath := filepath.Join(tempDir, "daem.toml")

	testkit.WriteFile(t, binDir, "git", "#!/bin/sh\nprintf invoked > \"$DAEM_GIT_MARKER\"\nexit 99\n")
	if err := os.Chmod(filepath.Join(binDir, "git"), 0o700); err != nil {
		t.Fatalf("Chmod fake git returned error: %v", err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("DAEM_GIT_MARKER", markerPath)
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "skill", "https://example.com/repo.git",
		"--ref", " main",
		"--manifest", manifestPath,
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode == 0 {
		t.Fatalf("exitCode = 0, stdout = %q", stdout.String())
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("fake git was launched or marker stat failed: %v", err)
	}
	if !strings.Contains(stderr.String(), "git ref has surrounding whitespace") {
		t.Fatalf("stderr = %q, want no-repair whitespace rejection", stderr.String())
	}
}

func TestRunLockRejectsCredentialLocatorAcrossOutputModes(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		json     bool
		terminal bool
	}{
		{name: "human"},
		{name: "json", json: true},
		{name: "terminal progress", terminal: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tempDir := t.TempDir()
			binDir := filepath.Join(tempDir, "bin")
			markerPath := filepath.Join(tempDir, "git-invoked")
			manifestPath := filepath.Join(tempDir, "daem.toml")
			lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
			secret := "synthetic-locator-secret"

			writeMarkerGit(t, binDir)
			t.Setenv("PATH", binDir)
			t.Setenv("DAEM_GIT_MARKER", markerPath)
			manifest := `
version = 1
targets = ["codex"]

[[skill]]
name = "demo"
source = { git = "https://user:` + secret + `@example.com/repo.git", path = ".", ref = "main" }
`
			if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
				t.Fatalf("WriteFile manifest returned error: %v", err)
			}

			args := []string{"lock", "--manifest", manifestPath}
			if testCase.json {
				args = append(args, "--json")
			}
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLIWithOptions(args, clipkg.RunOptions{
				Stdout:           &stdout,
				Stderr:           &stderr,
				StderrIsTerminal: testCase.terminal,
			})
			if exitCode == 0 {
				t.Fatalf("exitCode = 0, stdout = %q", stdout.String())
			}
			if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
				t.Fatalf("fake git was launched or marker stat failed: %v", err)
			}
			if _, err := os.Stat(lockfilePath); !os.IsNotExist(err) {
				t.Fatalf("lockfile was written or stat failed: %v", err)
			}
			combined := stdout.String() + stderr.String()
			if strings.Contains(combined, secret) || strings.Contains(combined, "user:") {
				t.Fatalf("output disclosed rejected locator: %q", combined)
			}
			if !strings.Contains(combined, "must not contain userinfo") {
				t.Fatalf("output = %q, want userinfo rejection", combined)
			}
		})
	}
}

func TestRunLockSanitizesBackendGitStderrAcrossOutputModes(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		json     bool
		terminal bool
	}{
		{name: "human"},
		{name: "json", json: true},
		{name: "terminal progress", terminal: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tempDir := t.TempDir()
			binDir := filepath.Join(tempDir, "bin")
			markerPath := filepath.Join(tempDir, "git-invoked")
			manifestPath := filepath.Join(tempDir, "daem.toml")
			lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
			secret := "synthetic-backend-secret"

			writeDiagnosticGit(t, binDir)
			t.Setenv("PATH", binDir)
			t.Setenv("DAEM_GIT_MARKER", markerPath)
			t.Setenv("DAEM_FAKE_SECRET", secret)
			testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
name = "demo"
source = { git = "https://example.com/repo.git", path = ".", ref = "main" }
`)

			args := []string{"lock", "--manifest", manifestPath}
			if testCase.json {
				args = append(args, "--json")
			}
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLIWithOptions(args, clipkg.RunOptions{
				Stdout:           &stdout,
				Stderr:           &stderr,
				StderrIsTerminal: testCase.terminal,
			})
			if exitCode == 0 {
				t.Fatalf("exitCode = 0, stdout = %q", stdout.String())
			}
			if _, err := os.Stat(markerPath); err != nil {
				t.Fatalf("fake git did not execute: %v", err)
			}
			if _, err := os.Stat(lockfilePath); !os.IsNotExist(err) {
				t.Fatalf("lockfile was written or stat failed: %v", err)
			}
			combined := stdout.String() + stderr.String()
			if strings.Contains(combined, secret) || strings.Contains(combined, "user:") {
				t.Fatalf("output disclosed backend credential: %q", combined)
			}
			if !strings.Contains(combined, "https://<redacted>@example.com/repo.git") {
				t.Fatalf("output = %q, want redacted backend URL", combined)
			}
		})
	}
}

func TestRunAddSkillInjectionPreservesManifestAndLockfile(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	markerPath := filepath.Join(tempDir, "git-invoked")
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	secret := "synthetic-ref-secret"
	originalManifest := "version = 1\ntargets = [\"codex\"]\n"
	originalLock := "sentinel lock bytes\n"

	writeMarkerGit(t, binDir)
	t.Setenv("PATH", binDir)
	t.Setenv("DAEM_GIT_MARKER", markerPath)
	if err := os.WriteFile(manifestPath, []byte(originalManifest), 0o600); err != nil {
		t.Fatalf("WriteFile manifest returned error: %v", err)
	}
	if err := os.WriteFile(lockfilePath, []byte(originalLock), 0o600); err != nil {
		t.Fatalf("WriteFile lockfile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "skill", "https://example.com/repo.git",
		"--ref", "--upload-pack=" + secret,
		"--manifest", manifestPath,
		"--yes",
	}, &stdout, &stderr)
	if exitCode == 0 {
		t.Fatalf("exitCode = 0, stdout = %q", stdout.String())
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("fake git was launched or marker stat failed: %v", err)
	}
	assertTrustBoundaryFileContent(t, manifestPath, originalManifest)
	assertTrustBoundaryFileContent(t, lockfilePath, originalLock)
	if strings.Contains(stdout.String()+stderr.String(), secret) {
		t.Fatalf("output disclosed rejected ref: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func writeMarkerGit(t *testing.T, binDir string) {
	t.Helper()
	testkit.WriteFile(t, binDir, "git", "#!/bin/sh\nprintf invoked > \"$DAEM_GIT_MARKER\"\nexit 99\n")
	if err := os.Chmod(filepath.Join(binDir, "git"), 0o700); err != nil {
		t.Fatalf("Chmod fake git returned error: %v", err)
	}
}

func writeDiagnosticGit(t *testing.T, binDir string) {
	t.Helper()
	testkit.WriteFile(t, binDir, "git", "#!/bin/sh\nprintf invoked > \"$DAEM_GIT_MARKER\"\nprintf 'fatal: https://user:%s@example.com/repo.git\\n' \"$DAEM_FAKE_SECRET\" >&2\nexit 97\n")
	if err := os.Chmod(filepath.Join(binDir, "git"), 0o700); err != nil {
		t.Fatalf("Chmod fake git returned error: %v", err)
	}
}

func assertTrustBoundaryFileContent(t *testing.T, path string, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %q returned error: %v", path, err)
	}
	if string(content) != want {
		t.Fatalf("file %q = %q, want %q", path, content, want)
	}
}
