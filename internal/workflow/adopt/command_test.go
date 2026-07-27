package adopt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/profile"
)

func TestBuildCommandPlanDefaultsProjectScopeAndDedupesTargets(t *testing.T) {
	tempDir := t.TempDir()
	oldWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWorkingDirectory); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	if err := os.WriteFile("AGENTS.md", []byte("# Agents\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(tempDir, "daem.imported.toml")

	result, err := BuildCommandPlan(context.Background(), CommandInput{
		TargetValues: []string{"codex", "codex"},
		ManifestPath: outputPath,
	})
	if err != nil {
		t.Fatalf("BuildCommandPlan returned error: %v", err)
	}
	if result.OutputPath() != outputPath {
		t.Fatalf("OutputPath = %q, want %q", result.OutputPath(), outputPath)
	}
	if result.Merge() {
		t.Fatalf("Merge = true, want false")
	}
	if result.AdoptionPlan().ResourceCount() != 1 {
		t.Fatalf("ResourceCount = %d, want 1", result.AdoptionPlan().ResourceCount())
	}
}

func TestBuildCommandPlanRefusesActiveRecoveryBeforeLiveScan(t *testing.T) {
	root := t.TempDir()
	outputPath := filepath.Join(root, "daem.toml")
	if err := os.MkdirAll(filepath.Join(root, ".daem", "recovery", "active-operation"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("must not be scanned\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := BuildCommandPlan(context.Background(), CommandInput{
		TargetValues: []string{"codex"},
		ManifestPath: outputPath,
	})
	if err == nil || !strings.Contains(err.Error(), "daem recover --dry-run") {
		t.Fatalf("BuildCommandPlan error = %v, want active recovery refusal", err)
	}
	if result.OutputPath() != outputPath {
		t.Fatalf("OutputPath = %q, want resolved request path %q", result.OutputPath(), outputPath)
	}
	if len(result.AdoptionPlan().Scans()) != 0 {
		t.Fatalf("active recovery plan scanned live paths: %#v", result.AdoptionPlan().Scans())
	}
}

func TestBuildCommandPlanPreservesHintFactsWhenNothingImports(t *testing.T) {
	tempDir := t.TempDir()
	oldWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWorkingDirectory); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	outputPath := filepath.Join(tempDir, "daem.imported.toml")

	result, err := BuildCommandPlan(context.Background(), CommandInput{
		TargetValues: []string{"codex"},
		ManifestPath: outputPath,
	})
	if err == nil {
		t.Fatal("BuildCommandPlan returned nil error for empty live config")
	}
	if !IsNothingToImport(err) {
		t.Fatalf("error = %v, want nothing-to-import classification", err)
	}
	if result.OutputPath() != outputPath {
		t.Fatalf("OutputPath = %q, want %q", result.OutputPath(), outputPath)
	}
	if result.Merge() {
		t.Fatalf("Merge = true, want false")
	}
}

func TestBuildCommandPlanRejectsMissingTargetBeforeResolvingRequest(t *testing.T) {
	result, err := BuildCommandPlan(context.Background(), CommandInput{})
	if err == nil {
		t.Fatal("BuildCommandPlan returned nil error for missing target")
	}
	if result.OutputPath() != "" {
		t.Fatalf("OutputPath = %q, want empty path before request resolution", result.OutputPath())
	}
}

func TestImportTargetDiagnosticCatalogUsesProfilePolicyOrder(t *testing.T) {
	got := targetValues(profile.ImportableTargets())
	want := "codex, claude-code, opencode, pi, antigravity-cli"
	if got != want {
		t.Fatalf("import target values = %q, want %q", got, want)
	}
}
