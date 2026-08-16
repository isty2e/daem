package listworkflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func TestRunBuildsManifestResourceRows(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeFile(t, manifestPath, `
version = 1
targets = ["codex", "claude-code"]

[instructions.project]
source = "AGENTS.md"
targets = ["codex"]

[[skill]]
id = "codex-review"
name = "review"
source = { git = "https://github.com/owner/repo.git", path = "skills/review", ref = "main" }
targets = ["codex"]
scope = "global"

[[skill_group]]
names = ["alpha", "beta"]
source = { path = "skills", mode = "vendor" }
targets = ["claude-code"]

[[hook]]
name = "prime-session"
event = "SessionStart"
command = "bd prime"
targets = ["codex"]
`)

	result, err := Run(context.Background(), Input{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.ManifestPath != manifestPath {
		t.Fatalf("ManifestPath = %q, want %q", result.ManifestPath, manifestPath)
	}
	if got := len(result.Environment.Instructions()); got != 1 {
		t.Fatalf("instructions = %d, want 1", got)
	}
	if got := len(result.Environment.Skills()); got != 3 {
		t.Fatalf("skills = %d, want 3", got)
	}
	if got := len(result.Environment.Hooks()); got != 1 {
		t.Fatalf("hooks = %d, want 1", got)
	}
	if !result.Selection.Includes("codex") || !result.Selection.Includes("claude-code") {
		t.Fatalf("selection = %#v, want manifest targets", result.Selection.Targets())
	}
	groups := result.SkillGroups()
	if groups["alpha"] != "skill_group[0]" || groups["beta"] != "skill_group[0]" {
		t.Fatalf("skill groups = %#v, want expanded group membership", groups)
	}
	groups["alpha"] = "forged"
	if got := result.SkillGroups()["alpha"]; got != "skill_group[0]" {
		t.Fatalf("SkillGroups aliased caller mutation: %q", got)
	}
	if _, err := os.Stat(filepath.Join(tempDir, "daem.lock.toml")); !os.IsNotExist(err) {
		t.Fatalf("lockfile stat error = %v, want missing lockfile", err)
	}
}

func TestRunSelectsResourceTargetAbsentFromHeader(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	writeFile(t, manifestPath, `
version = 1
targets = ["codex"]

[[mcp_server]]
name = "repo-tools"
targets = ["pi"]
scope = "project"
transport = "stdio"
command = "repo-tools"
`)

	unfiltered, err := Run(context.Background(), Input{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !unfiltered.Selection.Includes("codex") || !unfiltered.Selection.Includes("pi") {
		t.Fatalf("unfiltered selection = %#v, want header codex and resource pi", unfiltered.Selection.Targets())
	}

	filtered, err := Run(context.Background(), Input{
		ManifestPath: manifestPath,
		TargetValues: []string{"pi"},
	})
	if err != nil {
		t.Fatalf("Run(--target pi) returned error: %v", err)
	}
	if !filtered.Selection.Includes("pi") || filtered.Selection.Includes("codex") {
		t.Fatalf("filtered selection = %#v, want only pi", filtered.Selection.Targets())
	}
}

func TestRunRejectsSupportedTargetAbsentFromManifest(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	writeFile(t, manifestPath, `
version = 1
targets = ["codex"]

[instructions.project]
source = "AGENTS.md"
`)

	result, err := Run(context.Background(), Input{
		ManifestPath: manifestPath,
		TargetValues: []string{"claude-code"},
	})
	if err == nil {
		t.Fatal("Run accepted target absent from the manifest")
	}
	if !errors.Is(err, targetselection.ErrInvalid) {
		t.Fatalf("error = %v, want target selection classification", err)
	}
	if result.ManifestPath != manifestPath {
		t.Fatalf("ManifestPath = %q, want %q", result.ManifestPath, manifestPath)
	}
}

func TestRunReturnsResolvedManifestPathForMissingManifest(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")

	result, err := Run(context.Background(), Input{ManifestPath: manifestPath})
	if err == nil {
		t.Fatal("Run returned nil error for missing manifest")
	}
	if result.ManifestPath != manifestPath {
		t.Fatalf("ManifestPath = %q, want %q", result.ManifestPath, manifestPath)
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
