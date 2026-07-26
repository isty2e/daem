package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunImportDoesNotScanDotAgentSkillPoolDirectly(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "project instructions\n")
	testkit.WriteFile(t, filepath.Join(tempDir, ".agent", "skills", "direct"), "SKILL.md", "---\nname: direct\ndescription: Direct\n---\n")
	outputPath := filepath.Join(tempDir, "daem.imported.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--manifest", outputPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if strings.Contains(stdout.String(), `resource="skill/direct"`) || strings.Contains(stdout.String(), ".agent/skills/direct") {
		t.Fatalf("stdout = %q, want .agent skill pool ignored", stdout.String())
	}
}

func TestRunImportCustomSourceDirUsesAbsoluteManifestPath(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "project instructions\n")
	outputPath := filepath.Join(tempDir, "config", "daem.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--scope", "project", "--scope", "project", "--manifest", outputPath, "--source-dir", "sources"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if strings.Count(stdout.String(), `resource="instructions/codex_project"`) != 1 {
		t.Fatalf("stdout = %q, want one imported resource", stdout.String())
	}

	testkit.AssertFileContent(t, filepath.Join(tempDir, "config", "sources", "instructions", "codex-project.md"), "project instructions\n")
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile manifest returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("generated manifest did not parse: %v\n%s", err, content)
	}
	source, ok := config.Instructions()[0].Source().Local()
	if !ok {
		t.Fatalf("Source = %#v, want local source", config.Instructions()[0].Source())
	}
	wantSourcePath := filepath.ToSlash(filepath.Join(tempDir, "config", "sources", "instructions", "codex-project.md"))
	if got := source.Path(); got != wantSourcePath {
		t.Fatalf("SourcePath = %q, want custom absolute path %q", got, wantSourcePath)
	}
}

func TestRunImportRejectsSourceDirOutsideOutputDirectory(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "project instructions\n")
	outputPath := filepath.Join(tempDir, "config", "daem.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--manifest", outputPath, "--source-dir", "../outside", "--dry-run"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "source-dir must stay inside the output manifest directory") {
		t.Fatalf("stderr = %q, want source-dir escape diagnostic", stderr.String())
	}
}
