package apply

import (
	"os"
	"path/filepath"
	"testing"

	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestNativeOrdinaryFirstApplyUsesOneWorkflow(t *testing.T) {
	storage := t.TempDir()
	t.Setenv("HOME", filepath.Join(storage, "home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(storage, "data"))
	t.Setenv("CODEX_HOME", filepath.Join(storage, "codex"))
	root := filepath.Join(storage, "workspace")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "daem.toml")
	files := map[string]string{
		"daem.toml":        "version=1\ntargets=['codex']\n[instructions.project]\nsource='./instructions.md'\n[[host_config]]\nid='preferences'\npath='config.toml'\nsource='./preferences.toml'\nownership='keys'\n",
		"instructions.md":  "ordinary instructions\n",
		"preferences.toml": "model='native-test'\n",
	}
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifest}); err != nil {
		t.Fatal(err)
	}
	planned, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest})
	if err != nil {
		t.Fatal(err)
	}
	result, err := ExecuteWithOptions(t.Context(), planned, ExecuteOptions{})
	if err != nil {
		t.Fatalf("mixed first Apply attempted=%t actions=%d: %+v", result.ExecutionAttempted, result.ActionCount, err)
	}
	for _, path := range []string{"AGENTS.md", filepath.Join(".codex", "config.toml")} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Fatal("mixed output was not published", path, err)
		}
	}

	if err := os.WriteFile(filepath.Join(root, "instructions.md"), []byte("updated ordinary instructions\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifest}); err != nil {
		t.Fatal(err)
	}
	planned, err = PlanWrite(t.Context(), CommandInput{ManifestPath: manifest})
	if err != nil {
		t.Fatal(err)
	}
	result, err = ExecuteWithOptions(t.Context(), planned, ExecuteOptions{})
	if err != nil {
		t.Fatalf("ordinary update with current keys attempted=%t actions=%d: %+v", result.ExecutionAttempted, result.ActionCount, err)
	}

	planned, err = PlanWrite(t.Context(), CommandInput{ManifestPath: manifest})
	if err != nil {
		t.Fatalf("current mixed plan: %+v", err)
	}
	result, err = ExecuteWithOptions(t.Context(), planned, ExecuteOptions{})
	if err != nil {
		t.Fatalf("current mixed execution attempted=%t: %+v", result.ExecutionAttempted, err)
	}
	if result.ActionCount != 0 {
		t.Fatalf("current mixed execution reported %d actions", result.ActionCount)
	}
}
