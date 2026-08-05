package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestREADMEFirstProjectCommandsPreviewAuthorAndPlanApply(t *testing.T) {
	homeRoot := t.TempDir()
	t.Setenv("HOME", homeRoot)
	t.Setenv("CODEX_HOME", filepath.Join(homeRoot, ".codex"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(homeRoot, ".claude"))
	testkit.SetDefaultRootEnv(t, filepath.Join(homeRoot, "xdg"))

	projectRoot := t.TempDir()
	testkit.WithWorkingDirectory(t, projectRoot)

	runREADMECommand(t, "init", "--dry-run")
	testkit.AssertPathMissing(t, filepath.Join(projectRoot, "daem.toml"))

	runREADMECommand(t, "init")
	manifestPath := filepath.Join(projectRoot, "daem.toml")
	manifestBeforeAdd, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}

	testkit.WriteFile(
		t,
		filepath.Join(projectRoot, "instructions"),
		"project.md",
		"# Project instructions\nUse concise, direct answers.\n",
	)
	runREADMECommand(
		t,
		"add", "instruction", "project", "./instructions/project.md",
		"--target", "codex", "--dry-run", "--diff",
	)
	testkit.AssertFileContent(t, manifestPath, string(manifestBeforeAdd))
	testkit.AssertPathMissing(t, filepath.Join(projectRoot, "daem.lock.toml"))

	runREADMECommand(
		t,
		"add", "instruction", "project", "./instructions/project.md",
		"--target", "codex",
	)
	if _, err := os.Stat(filepath.Join(projectRoot, "daem.lock.toml")); err != nil {
		t.Fatalf("persistent add did not write the adjacent lockfile: %v", err)
	}

	runREADMECommand(t, "apply", "--dry-run", "--diff")
	testkit.AssertPathMissing(t, filepath.Join(projectRoot, "AGENTS.md"))
}

func runREADMECommand(t *testing.T, args ...string) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := testkit.RunCLI(args, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("daem %v exited %d; stdout=%q stderr=%q", args, exitCode, stdout.String(), stderr.String())
	}
}
