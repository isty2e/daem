package cli_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativeApplyRemovesNonASCIIFileAndWritesSibling(t *testing.T) {
	for _, boundary := range []string{"commit", "return-error", "before-state"} {
		t.Run(boundary, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			nativefixture.SetRecoveryEnvironment(t, root)
			workspace := filepath.Join(root, "workspace")
			manifest := filepath.Join(workspace, "daem.toml")
			old := filepath.Join(workspace, ".codex", "Café.toml")
			next := filepath.Join(workspace, ".codex", "other.toml")
			const original = "model = 'old'\n"
			const desired = "model = 'next'\n"
			writeManifest := func(id, name, ownership, content string) {
				t.Helper()
				testkit.WriteFile(t, workspace, "source.toml", content)
				testkit.WriteFile(t, workspace, "daem.toml", fmt.Sprintf(`version=1
targets=['codex']
[[agent_config]]
id=%q
path=%q
ownership=%q
source='source.toml'
`, id, name, ownership))
			}
			writeManifest("old", filepath.Base(old), "document", original)
			testkit.WriteFile(t, filepath.Dir(old), filepath.Base(old), original)
			if err := os.Chmod(old, 0o600); err != nil {
				t.Fatal(err)
			}
			nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
			nativefixture.RunLifecycleCommand(t, "apply", "--yes", "--manage-existing", "--manifest", manifest)

			writeManifest("next", filepath.Base(next), "keys", desired)
			nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
			paths, err := daempaths.Resolve(manifest)
			if err != nil {
				t.Fatal(err)
			}
			fixture := nativefixture.RecoveryFixture{Manifest: manifest, Destination: next, Paths: paths}
			before := nativefixture.SnapshotRecoveryPaths(t, old, next, paths.StatefilePath, paths.OwnershipRegistryPath)
			nativefixture.RunLifecycleCommand(t, "apply", "--dry-run", "--manifest", manifest)

			switch boundary {
			case "commit":
				nativefixture.RunLifecycleCommand(t, "apply", "--yes", "--manifest", manifest)
				testkit.AssertPathMissing(t, old)
				nativefixture.RequireSharedValues(t, configdocument.TOML, testkit.ReadFile(t, next), desired, true)
				state, err := statefile.DecodePersistedFacts(t.Context(), testkit.ReadFile(t, paths.StatefilePath))
				if err != nil || len(state.ManagedPaths()) != 0 || len(state.ManagedAggregates()) != 1 || state.ManagedAggregates()[0].Contribution().Address().Document().AggregateRoot().RelativePath() != ".codex/other.toml" {
					t.Fatal("apply did not commit only the new key ownership", err)
				}
				nativefixture.RunLifecycleCommand(t, "status", "--check", "--manifest", manifest)
			case "return-error", "before-state":
				code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, boundary, "apply", "--yes")
				if !strings.Contains(stderr, nativefixture.RecoveryReceipt(next)) {
					t.Fatalf("apply did not reach the sibling write: %d %s %s", code, stdout, stderr)
				}
				if boundary == "before-state" {
					if code != nativefixture.RecoveryExit {
						t.Fatalf("apply did not reach its pre-State interruption: %d %s %s", code, stdout, stderr)
					}
					testkit.AssertPathMissing(t, old)
					nativefixture.RequireRecoveryDryRun(t, fixture, "needs_rollback")
					nativefixture.RunRecoveryCommand(t, fixture, "none", "recover", "--yes")
				} else if code != 1 {
					t.Fatalf("the injected publication error was not reported: %d %s %s", code, stdout, stderr)
				}
				if !maps.Equal(before, nativefixture.SnapshotRecoveryPaths(t, old, next, paths.StatefilePath, paths.OwnershipRegistryPath)) {
					t.Fatal("rollback did not restore both files and the original ownership")
				}
			}
			testkit.AssertNoRecoveryArtifacts(t, workspace)
		})
	}
}
