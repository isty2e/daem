package cli_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativeApplyRemovesNonASCIIFileAndWritesSibling(t *testing.T) {
	for _, boundary := range []string{"commit", "return-error", "before-state", "different-spelling", "ascii-spelling", "global-protection"} {
		t.Run(boundary, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			nativefixture.SetRecoveryEnvironment(t, root)
			workspace := filepath.Join(root, "workspace")
			manifest := filepath.Join(workspace, "daem.toml")
			directory, scope, source := filepath.Join(workspace, ".codex"), "project", "source.toml"
			if boundary == "global-protection" {
				directory, scope, source = os.Getenv("CODEX_HOME"), "global", filepath.Join(workspace, source)
			}
			name, alternate := "Café.toml", ""
			switch boundary {
			case "different-spelling":
				alternate = "Cafe\u0301.toml"
			case "ascii-spelling":
				name, alternate = "K.toml", "K.toml"
			}
			old := filepath.Join(directory, name)
			next := filepath.Join(directory, "other.toml")
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
scope=%q
ownership=%q
source=%q
`, id, name, scope, ownership, source))
			}
			testkit.WriteFile(t, filepath.Dir(old), filepath.Base(old), original)
			if err := os.Chmod(old, 0o600); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(filepath.Dir(old))
			if err != nil || len(entries) != 1 {
				t.Fatal("cannot identify the stored filename", err)
			}
			selectedName := entries[0].Name()
			if alternate != "" {
				if alternate == selectedName {
					alternate = name
				}
				stored, err := os.Stat(old)
				if err != nil {
					t.Fatal(err)
				}
				alias, err := os.Stat(filepath.Join(filepath.Dir(old), alternate))
				if os.IsNotExist(err) {
					t.Skip("the volume treats the Unicode spellings as distinct files")
				}
				if err != nil || !os.SameFile(stored, alias) {
					t.Fatal("the alternate spelling does not identify the original file", err)
				}
				selectedName = alternate
			}
			writeManifest("old", selectedName, "document", original)
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
			reason := ""
			switch boundary {
			case "different-spelling", "ascii-spelling":
				reason = "path spelling cannot be matched after deletion"
			case "global-protection":
				reason = "path cannot be shown to stay outside reserved data after deletion"
			}
			if reason != "" {
				code, stdout, stderr := testkit.RunCapturedCLI("apply", "--dry-run", "--json", "--manifest", manifest)
				if code != 1 || stderr != "" {
					t.Fatalf("expected the removal to be refused at preview: %d\n%s%s", code, stdout, stderr)
				}
				failure := clijson.DecodeFailure(t, stdout, "apply", "operation")
				if !strings.Contains(failure.Error.Message, selectedName) || !strings.Contains(failure.Error.Message, reason) {
					t.Fatalf("the refusal did not explain why the selected path cannot be removed: %s", failure.Error.Message)
				}
				if len(failure.Guidance) != 1 || !slices.Equal(failure.Guidance[0].Argv, []string{"daem", "unmanage", "agent-config", "old", "--dry-run"}) {
					t.Fatalf("the refusal did not offer to release ownership without deleting: %+v", failure.Guidance)
				}
				code, stdout, stderr = testkit.RunCapturedCLI("apply", "--yes", "--manifest", manifest)
				if code != 1 || !strings.Contains(stderr, reason) {
					t.Fatalf("write apply did not refuse for the same reason: %d\n%s%s", code, stdout, stderr)
				}
				nativefixture.RunLifecycleCommand(t, "unmanage", "agent-config", "old", "--dry-run", "--manifest", manifest)
				if !maps.Equal(before, nativefixture.SnapshotRecoveryPaths(t, old, next, paths.StatefilePath, paths.OwnershipRegistryPath)) {
					t.Fatal("refused removal changed the files or their ownership")
				}
				testkit.AssertNoRecoveryArtifacts(t, workspace)
				return
			}
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
