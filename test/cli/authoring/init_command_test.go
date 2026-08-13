package cli_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/declarationartifact"
)

func TestRunInitDryRunPreviewsDefaultManifestWithoutWrites(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	workingDir, err := filepath.EvalSymlinks(tempDir)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	manifestPath := filepath.Join(workingDir, "daem.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"init", "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"init: create manifest",
		"manifest: " + manifestPath,
		"planned:",
		`targets = ["codex"]`,
		"next: rerun daem init without --dry-run",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertPathMissing(t, manifestPath)
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "state", "daem"))
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "cache", "daem"))
}

func TestRunInitYesCreatesExplicitManifestParentsOnly(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "nested", "config", "daem.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"init", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "created: manifest") {
		t.Fatalf("stdout = %q, want created summary", stdout.String())
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if _, err := declarationmanifest.Decode(content); err != nil {
		t.Fatalf("Parse returned error: %v\n%s", err, content)
	}
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "nested", "config", "daem.lock.toml"))
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "nested", "config", ".daem"))
}

func TestRunInitRefusesOverwriteWithoutForce(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := "version = 1\ntargets = [\"claude-code\"]\n"
	testkit.WriteFile(t, tempDir, "daem.toml", original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"init", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode == 0 {
		t.Fatalf("exitCode = 0, stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "manifest already exists") || !strings.Contains(stderr.String(), "--force") {
		t.Fatalf("stderr = %q, want overwrite diagnostic", stderr.String())
	}
	testkit.AssertFileContent(t, manifestPath, original)
}

func TestRunInitForceOverwritesExistingManifest(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", "[malformed\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"init", "--manifest", manifestPath, "--force"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "overwritten: manifest") {
		t.Fatalf("stdout = %q, want overwrite summary", stdout.String())
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !strings.Contains(string(content), `targets = ["codex"]`) {
		t.Fatalf("manifest = %s, want minimal init template", content)
	}
}

func TestRunInitForceRejectsOversizedManifestInPreviewAndWrite(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "dry-run", args: []string{"--dry-run"}},
		{name: "write"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			manifestPath := filepath.Join(root, "daem.toml")
			file, err := os.Create(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := file.Truncate(declarationartifact.MaximumBytes + 1); err != nil {
				_ = file.Close()
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}

			args := []string{"init", "--manifest", manifestPath, "--force"}
			args = append(args, test.args...)
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
			if exitCode == 0 {
				t.Fatalf("exitCode = 0, stdout = %q", stdout.String())
			}
			if !strings.Contains(stderr.String(), "exceeds") ||
				!strings.Contains(stderr.String(), fmt.Sprint(declarationartifact.MaximumBytes)) {
				t.Fatalf("stderr = %q, want bounded declaration diagnostic", stderr.String())
			}
			info, err := os.Stat(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			if info.Size() != declarationartifact.MaximumBytes+1 {
				t.Fatalf("manifest size = %d, want original oversized file", info.Size())
			}
		})
	}
}
