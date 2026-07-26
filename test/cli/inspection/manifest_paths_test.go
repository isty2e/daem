package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestRunLockDryRunWithoutManifestUsesCWDManifestWhenPresent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test fixes Unix-style default path contract")
	}

	root := t.TempDir()
	configHome := filepath.Join(root, "config")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	testkit.WithWorkingDirectory(t, root)

	testkit.WriteFile(t, root, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "AGENTS.md"
`)
	testkit.WriteFile(t, root, "AGENTS.md", "project instructions\n")
	testkit.WriteFile(t, filepath.Join(configHome, "daem"), "daem.toml", "version = \"wrong\"\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), filepath.Join(root, "daem.lock.toml")) {
		t.Fatalf("stdout = %q, want cwd lockfile path", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(root, "daem.lock.toml")); !os.IsNotExist(err) {
		t.Fatalf("lock --dry-run wrote cwd lockfile or stat failed unexpectedly: %v", err)
	}
}

func TestRunLockDryRunWithoutManifestFallsBackToUserDefaultWhenCWDManifestMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test fixes Unix-style default path contract")
	}

	root := t.TempDir()
	configHome := filepath.Join(root, "config")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	testkit.WithWorkingDirectory(t, root)

	sourcePath := filepath.Join(root, "global.md")
	testkit.WriteFile(t, root, "global.md", "global instructions\n")
	testkit.WriteFile(t, filepath.Join(configHome, "daem"), "daem.toml", `
version = 1
targets = ["codex"]

[instructions.global]
source = "`+filepath.ToSlash(sourcePath)+`"
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), filepath.Join(configHome, "daem", "daem.lock.toml")) {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(configHome, "daem", "daem.lock.toml")); !os.IsNotExist(err) {
		t.Fatalf("lock --dry-run wrote default lockfile or stat failed unexpectedly: %v", err)
	}
}

func TestRunLockDryRunWithoutManifestDoesNotSearchParentDirectories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test fixes Unix-style default path contract")
	}

	root := t.TempDir()
	configHome := filepath.Join(root, "config")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	testkit.WriteFile(t, root, "daem.toml", "version = \"wrong\"\n")
	child := filepath.Join(root, "child")
	if err := os.MkdirAll(child, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	testkit.WithWorkingDirectory(t, child)

	sourcePath := filepath.Join(root, "global.md")
	testkit.WriteFile(t, root, "global.md", "global instructions\n")
	testkit.WriteFile(t, filepath.Join(configHome, "daem"), "daem.toml", `
version = 1
targets = ["codex"]

[instructions.global]
source = "`+filepath.ToSlash(sourcePath)+`"
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), filepath.Join(configHome, "daem", "daem.lock.toml")) {
		t.Fatalf("stdout = %q, want default user lockfile path", stdout.String())
	}
}

func TestRunLockWithoutManifestRejectsProjectResourceFromUserDefault(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test fixes Unix-style default path contract")
	}

	root := t.TempDir()
	configHome := filepath.Join(root, "config")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	testkit.WithWorkingDirectory(t, root)

	testkit.WriteFile(t, filepath.Join(configHome, "daem"), "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "AGENTS.md"
`)
	testkit.WriteFile(t, filepath.Join(configHome, "daem"), "AGENTS.md", "instructions\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"project-scoped instruction \"project\" requires a project manifest",
		"use --manifest ./daem.toml or set scope = \"global\"",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}
