package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestListPathsReportsSelectionsWithoutMutatingWorkspace(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	testkit.WithWorkingDirectory(t, root)
	testkit.WriteFile(t, root, "daem.toml", `
version = 1
targets = ["opencode"]

[[skill]]
name = "review"
source = { path = "skills/review", mode = "vendor" }
targets = ["opencode"]
scope = "project"

[skill.target.opencode]
install_to = ".agents/skills"
`)
	testkit.WriteFile(t, root, "daem.lock.toml", "sentinel lock\n")
	testkit.WriteFile(t, root, ".daem/state.json", "sentinel state\n")
	testkit.WriteFile(t, root, ".agents/skills/review/SKILL.md", "sentinel output\n")

	sentinels := map[string]string{
		"daem.toml":                      mustReadInspectionFile(t, root, "daem.toml"),
		"daem.lock.toml":                 "sentinel lock\n",
		".daem/state.json":               "sentinel state\n",
		".agents/skills/review/SKILL.md": "sentinel output\n",
	}

	var humanStdout bytes.Buffer
	var humanStderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{"list", "paths", "--manifest", filepath.Join(root, "daem.toml"), "--target", "opencode"},
		&humanStdout,
		&humanStderr,
	)
	if exitCode != 0 || humanStderr.Len() != 0 {
		t.Fatalf("exitCode = %d, stdout = %q, stderr = %q", exitCode, humanStdout.String(), humanStderr.String())
	}
	for _, fragment := range []string{
		"opencode\n  project\n",
		"    skills\n",
		"      write: .agents/skills [selected]",
		"      write: .opencode/skills [default]",
		"    hooks\n",
		"      unsupported: bridge required",
	} {
		if !strings.Contains(humanStdout.String(), fragment) {
			t.Fatalf("human output missing %q:\n%s", fragment, humanStdout.String())
		}
	}

	var jsonStdout bytes.Buffer
	var jsonStderr bytes.Buffer
	exitCode = testkit.RunVerboseCLI(
		[]string{"list", "paths", "--manifest", filepath.Join(root, "daem.toml"), "--json"},
		&jsonStdout,
		&jsonStderr,
	)
	if exitCode != 0 || jsonStderr.Len() != 0 || !json.Valid(jsonStdout.Bytes()) {
		t.Fatalf("exitCode = %d, stdout = %q, stderr = %q", exitCode, jsonStdout.String(), jsonStderr.String())
	}
	if !strings.Contains(jsonStdout.String(), `"selection_source": "manifest-explicit"`) {
		t.Fatalf("JSON output omitted explicit selection:\n%s", jsonStdout.String())
	}

	for relativePath, before := range sentinels {
		after := mustReadInspectionFile(t, root, relativePath)
		if after != before {
			t.Fatalf("%s changed from %q to %q", relativePath, before, after)
		}
	}
}

func TestListPathsRejectsConflictingPresentationModes(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	testkit.WithWorkingDirectory(t, root)
	testkit.WriteFile(t, root, "daem.toml", `version = 1
targets = ["codex"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{"list", "paths", "--json", "--verbose"},
		&stdout,
		&stderr,
	)
	if exitCode != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "--json and --verbose") {
		t.Fatalf("exitCode = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
}

func mustReadInspectionFile(t *testing.T, root string, relativePath string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relativePath)))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
