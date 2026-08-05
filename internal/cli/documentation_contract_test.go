package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandLifecycleDocumentationCoversEveryUserCommand(t *testing.T) {
	repositoryRoot := filepath.Join("..", "..")
	metaCommands := map[string]struct{}{
		"--help": {}, "--version": {}, "-h": {}, "help": {},
	}
	documents := map[string]string{
		"README.md":                     "| Author | `unmanage extension` | Release daem authority while preserving host-installed state |",
		filepath.Join("docs", "cli.md"): "| Author | `unmanage` | Release exact daem authority for one extension while preserving host state. |",
	}
	for path, unmanageRow := range documents {
		content, err := os.ReadFile(filepath.Join(repositoryRoot, path))
		if err != nil {
			t.Fatal(err)
		}
		section := markdownSection(t, string(content), "## Command Lifecycle")
		for command := range commandAdmissionCatalog {
			if _, meta := metaCommands[command]; meta {
				continue
			}
			if !strings.Contains(section, "`"+command) {
				t.Errorf("%s command lifecycle omits %q", path, command)
			}
		}
		if !strings.Contains(section, unmanageRow) {
			t.Errorf("%s does not classify unmanage as host-preserving authoring", path)
		}
	}
}

func TestREADMEAgentSkillBootstrapSelectsAndPersistsUserWorkspace(t *testing.T) {
	path := filepath.Join("..", "..", "README.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	section := markdownSection(t, string(content), "## Agent Skill")
	for _, required := range []string{
		`DAEM_USER_MANIFEST="${XDG_CONFIG_HOME:-$HOME/.config}/daem/daem.toml"`,
		`daem init --manifest "$DAEM_USER_MANIFEST" --dry-run`,
		`daem init --manifest "$DAEM_USER_MANIFEST"`,
		`--target codex --scope global --manifest "$DAEM_USER_MANIFEST"`,
		`daem apply --manifest "$DAEM_USER_MANIFEST" --dry-run --diff`,
		`daem apply --manifest "$DAEM_USER_MANIFEST"`,
	} {
		if !strings.Contains(section, required) {
			t.Errorf("README agent-skill bootstrap omits %q", required)
		}
	}
	if strings.Count(section, "daem add skill https://github.com/isty2e/daem.git") != 2 {
		t.Error("README agent-skill bootstrap must show preview and persistent add commands")
	}
}

func TestShippedDaemSkillUsesTargetedImport(t *testing.T) {
	path := filepath.Join("..", "..", "skills", "daem", "SKILL.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		if strings.Contains(line, "daem import --") && !strings.Contains(line, "--target") {
			t.Errorf("%s contains import command without required --target: %q", path, strings.TrimSpace(line))
		}
	}
	if !strings.Contains(string(content), "`daem import --target <target> --dry-run`") {
		t.Errorf("%s does not show the canonical targeted import preview", path)
	}
}

func markdownSection(t *testing.T, content string, heading string) string {
	t.Helper()
	start := strings.Index(content, heading)
	if start < 0 {
		t.Fatalf("documentation does not contain heading %q", heading)
	}
	remainder := content[start+len(heading):]
	if end := strings.Index(remainder, "\n## "); end >= 0 {
		return remainder[:end]
	}
	return remainder
}
