package adopt

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/target"
)

func TestBuildPlanManifestContentMatchesAdoptionRenderer(t *testing.T) {
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
	output := filepath.Join(tempDir, "daem.imported.toml")
	sourceDirectory, err := adoptmodel.NewSourceDirectory(output, filepath.Join(tempDir, "daem.imported.d"))
	if err != nil {
		t.Fatal(err)
	}
	request, err := adoptmodel.NewRequest(
		[]target.Target{target.TargetCodex},
		[]target.Scope{target.ScopeProject},
		output,
		sourceDirectory,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := BuildPlan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := adoptmodel.RenderManifestContent(plan.Sources(), plan.Skills(), plan.Hooks(), plan.MCPServers())
	if err != nil {
		t.Fatal(err)
	}
	if string(plan.ManifestContent()) != string(rendered) {
		t.Fatalf("manifest content diverged from renderer:\nplan:\n%s\nrendered:\n%s", plan.ManifestContent(), rendered)
	}
}
