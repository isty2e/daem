package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
	"github.com/isty2e/daem/test/testkit"
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
			if code, stdout, stderr := runOwnershipCLI("lock", "--manifest", manifest); code != 0 {
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
		code, stdout, stderr := runOwnershipCLI(args...)
		if code != 0 {
			t.Fatalf("empty native intent failed: %q code=%d stdout=%s stderr=%s", args, code, stdout, stderr)
		}
		if args[0] == "status" {
			requireNativeLifecycleClean(t, stdout)
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

func requireNativeLifecycleMetadata(t *testing.T, manifest string, managed bool) {
	t.Helper()
	requireNativeLifecycleMetadataForScope(t, manifest, managed, target.ScopeProject)
}

func requireNativeLifecycleMetadataForScope(t *testing.T, manifest string, managed bool, scope target.Scope) {
	t.Helper()
	paths, err := daempaths.Resolve(manifest)
	if err != nil {
		t.Fatal(err)
	}
	codecs, err := nativeconfig.Codecs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	codec, err := statefile.NewNativeCodec(codecs.Contributions())
	if err != nil {
		t.Fatal(err)
	}

	budget := recovery.NewPhysicalCaptureBudget(nil)
	state, err := statefile.LoadOptionalWithOptions(t.Context(), paths.StatefilePath, statefile.ReadOptions{Codec: codec, PhysicalPathBudget: budget})
	if err != nil || state.ContainsHostConfig() != managed {
		t.Fatal("persisted native ownership does not match the applied lifecycle", err)
	}
	reader, err := ownershipstore.NewRecoveryReader(paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := reader.LoadForClaimRemovals(t.Context(), nil, recovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}

	wantBindings, wantClaims := 0, 0
	if managed {
		wantBindings = 1
		if scope == target.ScopeGlobal {
			wantClaims = len(state.ManagedAggregates())
		}
	}
	if len(registry.DocumentBindings()) != wantBindings || len(registry.Claims()) != wantClaims {
		t.Fatalf("native %s lifecycle metadata: bindings=%d want=%d claims=%d want=%d", scope, len(registry.DocumentBindings()), wantBindings, len(registry.Claims()), wantClaims)
	}
	for _, claim := range registry.Claims() {
		if claim.State() != ownership.ClaimActive || claim.OperationID() != "" {
			t.Fatal("settled global claim retains reservation debt")
		}
	}
}
