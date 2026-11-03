package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativePreparedWriteRefusesChangedSourceAndCancellation(t *testing.T) {
	for _, fault := range []string{"source", "cancel"} {
		t.Run(fault, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", filepath.Join(root, "home"))
			t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
			workspace := filepath.Join(root, "workspace")
			manifest := filepath.Join(workspace, "daem.toml")
			testkit.WriteFile(t, workspace, "daem.toml", `version = 1
targets = ["codex"]
[[host_config]]
id = "preferences"
path = "config.toml"
source = "./preferences.toml"
ownership = "keys"
`)
			testkit.WriteFile(t, workspace, "preferences.toml", "model='locked'\n")
			if code, stdout, stderr := testkit.RunCapturedCLI("lock", "--manifest", manifest); code != 0 {
				t.Fatalf("lock failed: %d %s %s", code, stdout, stderr)
			}
			prepared, err := applyworkflow.PlanWrite(t.Context(), applyworkflow.CommandInput{ManifestPath: manifest})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = prepared.Close() })
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if fault == "source" {
				testkit.WriteFile(t, workspace, "preferences.toml", "model='changed'\n")
			} else {
				cancel()
			}
			result, err := applyworkflow.ExecuteWithOptions(ctx, prepared, applyworkflow.ExecuteOptions{})
			if err == nil || result.ExecutionAttempted {
				t.Fatal("stale or canceled native plan reached an effect boundary", err)
			}
			paths, err := daempaths.Resolve(manifest)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{filepath.Join(workspace, ".codex"), paths.StatefilePath, paths.OwnershipRegistryPath} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("refused native execution changed document or ownership", path, err)
				}
			}
		})
	}
}

func requireNativeReadOnlyWritePlan(t *testing.T, manifest string) {
	t.Helper()
	prepared, err := applyworkflow.PlanWrite(t.Context(), applyworkflow.CommandInput{ManifestPath: manifest})
	if err != nil {
		t.Fatal("native CLI write plan was not admitted", err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeEmptyKeyApplyDoesNotInventEffects(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex-home"))
	workspace := filepath.Join(root, "workspace")
	manifest := filepath.Join(workspace, "daem.toml")
	testkit.WriteFile(t, workspace, "daem.toml", `version = 1
targets = ["codex"]
[[host_config]]
id = "empty"
path = "config.toml"
source = "./empty.toml"
ownership = "keys"
`)
	testkit.WriteFile(t, workspace, "empty.toml", "")
	for _, args := range [][]string{{"lock", "--manifest", manifest}, {"apply", "--manifest", manifest, "--yes"}, {"status", "--manifest", manifest, "--check", "--json"}} {
		code, stdout, stderr := testkit.RunCapturedCLI(args...)
		if code != 0 {
			t.Fatalf("empty native intent failed: %q code=%d stdout=%s stderr=%s", args, code, stdout, stderr)
		}
		if args[0] == "status" {
			nativefixture.RequireLifecycleClean(t, stdout)
		}
	}
	paths, err := daempaths.Resolve(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(workspace, ".codex"), paths.StatefilePath, paths.OwnershipRegistryPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("empty positive intent invented a document or persisted ownership", path, err)
		}
	}
}
