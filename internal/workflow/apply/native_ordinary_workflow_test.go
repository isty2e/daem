package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/internal/subprocess"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestNativePiOrdinarySuffixUsesOnePreEffectReservation(t *testing.T) {
	storage, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(storage, "home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(storage, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(storage, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(storage, "state"))
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(storage, "pi"))
	root, bin := filepath.Join(storage, "workspace"), filepath.Join(storage, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nset -eu\nif [ \"$1\" = --version ]; then printf '1.0.2\\n'; exit 0; fi\n[ \"$1\" = install ] && [ \"$2\" = 'git:github.com/acme/pi-tools@v1' ] && [ \"$3\" = -l ] || exit 91\n[ ! -e AGENTS.md ] || exit 92\nprintf '%s\\n' '{\"theme\":\"native-test\",\"packages\":[\"git:github.com/acme/pi-tools@v1\"]}' > .pi/settings.json\n"
	if err := os.WriteFile(filepath.Join(bin, "pi"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	manifest := filepath.Join(root, "daem.toml")
	files := map[string]string{
		"daem.toml":       "version=1\ntargets=['pi']\n[instructions.project]\nsource='./instructions.md'\n[[host_config]]\nid='preferences'\npath='settings.json'\nsource='./preferences.json'\nownership='keys'\n[[extension]]\nid='tools'\ncarrier='pi-package'\ntargets=['pi']\nscope='project'\nsource={host_source='git:github.com/acme/pi-tools@v1'}\n",
		"instructions.md": "ordinary suffix\n", "preferences.json": `{"theme":"native-test"}`,
	}
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifest}); err != nil {
		t.Fatal(err)
	}
	prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest})
	if err != nil {
		t.Fatal(err)
	}
	reservations := 0
	result, err := executeWithDependencies(t.Context(), prepared, ExecuteOptions{HostRouteExecutor: subprocess.NewCommandExecutor(subprocess.CommandOptions{})}, executeDependencies{
		reserveForwardEffects: func(authority recoverygate.EffectAuthority, structure operationplan.EffectStructure, demand operationplan.Demand) (*recoverygate.ForwardEffectAuthority, error) {
			reservations++
			if _, err := os.Stat(filepath.Join(root, ".pi", "settings.json")); !os.IsNotExist(err) {
				t.Fatal("containing reservation followed a native effect", err)
			}
			return authority.ReserveForwardEffectStructure(structure, demand)
		},
	})
	if err != nil || reservations != 1 || result.ActionCount != 2 {
		t.Fatalf("mixed suffix reservation=%d actions=%d: %v", reservations, result.ActionCount, err)
	}
	if content, err := os.ReadFile(filepath.Join(root, "AGENTS.md")); err != nil || string(content) != "ordinary suffix\n" {
		t.Fatal("reserved suffix was not executed", err)
	}
}

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
