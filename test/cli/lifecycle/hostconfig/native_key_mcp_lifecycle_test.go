package cli_test

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativeKeyAndMCPSameFileCLILifecycle(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o644} {
		t.Run(fmt.Sprintf("existing mode %04o", mode), func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", filepath.Join(root, "home"))
			t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
			t.Setenv("CODEX_HOME", filepath.Join(root, "codex-home"))
			workspace := filepath.Join(root, "workspace")
			manifest := filepath.Join(workspace, "daem.toml")
			destination := filepath.Join(workspace, ".codex", "config.toml")
			const outside = "# User preferences\nmodel = 'user-model'\n[tui]\n# User theme\ntheme = 'dark'\n"
			testkit.WriteFile(t, workspace, ".codex/config.toml", outside)
			if err := os.Chmod(destination, mode); err != nil {
				t.Fatal(err)
			}

			writeManifest := func(server string, native bool) {
				t.Helper()
				content := fmt.Sprintf(`version = 1
targets = ["codex"]
[[mcp_server]]
name = "search"
transport = "stdio"
command = "node"
args = [%q]
`, server)
				if native {
					content += `[[agent_config]]
id = "preferences"
path = "config.toml"
source = "./preferences.toml"
ownership = "keys"
`
				}
				testkit.WriteFile(t, workspace, "daem.toml", content)
			}
			for index, server := range []string{"old.js", "new.js"} {
				writeManifest(server, true)
				source := fmt.Sprintf("[tui]\nnotifications = %t\n", index == 0)
				testkit.WriteFile(t, workspace, "preferences.toml", source)
				nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
				nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
				content, err := os.ReadFile(destination)
				if err != nil {
					t.Fatal(err)
				}
				expected := source + fmt.Sprintf("[mcp_servers.search]\ncommand = 'node'\nargs = [%q]\n", server)
				nativefixture.RequireSharedValues(t, configdocument.TOML, content, expected, true)
				if !bytes.Contains(content, []byte("[mcp_servers.search]\n")) {
					t.Fatalf("generated Codex MCP header does not use bare TOML segments: %s", content)
				}
				nativefixture.RequireSharedValues(t, configdocument.TOML, content, outside, true)
				nativefixture.RequireMCPUserComments(t, content)
				nativefixture.RequireSharedMode(t, destination, 0o600)
				nativefixture.RequireLifecycleMetadata(t, manifest, true)
				status := nativefixture.RunLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json")
				nativefixture.RequireLifecycleClean(t, status)
				if plan := clijson.DecodePlan(t, []byte(status)); plan.ActionCount != 2 {
					t.Fatal("mixed Apply did not retain both managed subjects", status)
				}
				nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
			}

			testkit.WriteFile(t, workspace, "daem.toml", `version = 1
targets = ["codex"]
[[agent_config]]
id = "preferences"
path = "config.toml"
source = "./preferences.toml"
ownership = "keys"
`)
			nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
			nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
			withoutMCP, err := os.ReadFile(destination)
			if err != nil {
				t.Fatal(err)
			}
			nativefixture.RequireSharedValues(t, configdocument.TOML, withoutMCP, "[mcp_servers.search]\ncommand='node'\nargs=['new.js']\n", false)
			nativefixture.RequireSharedValues(t, configdocument.TOML, withoutMCP, "[tui]\nnotifications = false\n", true)
			nativefixture.RequireSharedValues(t, configdocument.TOML, withoutMCP, outside, true)
			nativefixture.RequireMCPUserComments(t, withoutMCP)
			nativefixture.RequireLifecycleMetadata(t, manifest, true)
			nativefixture.RequireLifecycleClean(t, nativefixture.RunLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json"))
			nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")

			writeManifest("new.js", true)
			nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
			nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")

			writeManifest("new.js", false)
			nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
			nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
			content, err := os.ReadFile(destination)
			if err != nil {
				t.Fatal(err)
			}
			nativefixture.RequireSharedValues(t, configdocument.TOML, content, "[tui]\nnotifications = false\n", false)
			nativefixture.RequireSharedValues(t, configdocument.TOML, content, "[mcp_servers.search]\ncommand='node'\nargs=['new.js']\n", true)
			nativefixture.RequireSharedValues(t, configdocument.TOML, content, outside, true)
			nativefixture.RequireMCPUserComments(t, content)
			nativefixture.RequireLifecycleMetadata(t, manifest, false)
			nativefixture.RequireLifecycleClean(t, nativefixture.RunLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json"))
			nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")

			testkit.WriteFile(t, workspace, "daem.toml", "version = 1\ntargets = ['codex']\n")
			nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
			nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
			content, err = os.ReadFile(destination)
			if err != nil {
				t.Fatal(err)
			}
			nativefixture.RequireSharedValues(t, configdocument.TOML, content, "[mcp_servers.search]\ncommand='node'\nargs=['new.js']\n", false)
			nativefixture.RequireSharedValues(t, configdocument.TOML, content, outside, true)
			nativefixture.RequireMCPUserComments(t, content)
			nativefixture.RequireLifecycleClean(t, nativefixture.RunLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json"))
		})
	}
}

func TestNativeKeyAndMCPShareAliasedDocument(t *testing.T) {
	for _, name := range []string{"absent", "existing"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			nativefixture.SetRecoveryEnvironment(t, root)
			workspace := filepath.Join(root, "workspace")
			manifest := filepath.Join(workspace, "daem.toml")
			directory := filepath.Join(workspace, ".codex")
			destination := filepath.Join(directory, "config.toml")
			if err := os.MkdirAll(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(".", filepath.Join(directory, "alias")); err != nil {
				t.Fatal(err)
			}

			const outside = "# Kept by the user\nmanual = true\n"
			if name == "existing" {
				testkit.WriteFile(t, workspace, ".codex/config.toml", outside)
				if err := os.Chmod(destination, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			const nativeOnly = `version=1
targets=['codex']
[[agent_config]]
id='preferences'
path='alias/config.toml'
source='prefs.toml'
`
			const mcp = `[[mcp_server]]
name='search'
transport='stdio'
command='echo'
`
			const keyValue = "model = 'fixture'\n"
			const mcpValue = "[mcp_servers.search]\ncommand = 'echo'\n"
			testkit.WriteFile(t, workspace, "prefs.toml", keyValue)
			testkit.WriteFile(t, workspace, "daem.toml", nativeOnly+mcp)
			nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
			before := nativefixture.SnapshotRecoveryPaths(t, destination)

			nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--dry-run", "--diff")
			if !maps.Equal(before, nativefixture.SnapshotRecoveryPaths(t, destination)) {
				t.Fatal("preview changed the shared document")
			}
			nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
			actual := testkit.ReadFile(t, destination)
			nativefixture.RequireSharedValues(t, configdocument.TOML, actual, keyValue+mcpValue, true)
			nativefixture.RequireSharedMode(t, destination, 0o600)

			testkit.WriteFile(t, workspace, "daem.toml", nativeOnly)
			nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
			nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
			actual = testkit.ReadFile(t, destination)
			nativefixture.RequireSharedValues(t, configdocument.TOML, actual, keyValue, true)
			nativefixture.RequireSharedValues(t, configdocument.TOML, actual, mcpValue, false)
			if name == "existing" {
				nativefixture.RequireSharedValues(t, configdocument.TOML, actual, outside, true)
				if !bytes.Contains(actual, []byte("# Kept by the user")) {
					t.Fatalf("shared writes lost the existing comment: %s", actual)
				}
			}
			nativefixture.RequireLifecycleClean(t, nativefixture.RunLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json"))
			testkit.AssertNoRecoveryArtifacts(t, workspace)
		})
	}
}

func TestNativeKeyReplacementWithBrokenMCPRoute(t *testing.T) {
	for _, route := range []struct {
		name, link string
		cyclic     bool
	}{
		{name: "cycle", link: "config.toml", cyclic: true},
		{name: "unresolved", link: "missing/../prefs.toml"},
	} {
		t.Run(route.name, func(t *testing.T) {
			root := t.TempDir()
			nativefixture.SetRecoveryEnvironment(t, root)
			workspace := filepath.Join(root, "workspace")
			manifest := filepath.Join(workspace, "daem.toml")
			destination := filepath.Join(workspace, ".codex", "prefs.toml")
			mcp := filepath.Join(workspace, ".codex", "config.toml")
			testkit.WriteFile(t, workspace, "daem.toml", `version=1
targets=['codex']
[[agent_config]]
id='preferences'
path='prefs.toml'
source='preferences.toml'
[[mcp_server]]
name='search'
transport='stdio'
command='echo'
`)
			testkit.WriteFile(t, workspace, "preferences.toml", "model='managed'\n")
			nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
			nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
			paths, err := daempaths.Resolve(manifest)
			if err != nil {
				t.Fatal(err)
			}
			fixture := nativefixture.RecoveryFixture{Manifest: manifest, Destination: destination, Paths: paths}
			stateBefore, err := statefile.DecodePersistedFacts(t.Context(), testkit.ReadFile(t, paths.StatefilePath))
			if err != nil || len(stateBefore.ManagedAggregates()) != 2 {
				t.Fatal("fixture did not retain both managed settings", err)
			}
			const edited = "model='local'\nuser_setting=true\n"
			mcpValues := testkit.ReadFile(t, mcp)
			testkit.WriteFile(t, workspace, ".codex/prefs.toml", edited)
			linkTo := func(target string) {
				t.Helper()
				if err := os.Remove(mcp); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, mcp); err != nil {
					t.Fatal(err)
				}
			}
			linkTo(route.link)
			before := nativefixture.SnapshotRecoveryEvidence(t, fixture)

			if route.cyclic {
				fingerprint := nativeReplacementFingerprint(t, fixture, "/model")
				if !maps.Equal(before, nativefixture.SnapshotRecoveryEvidence(t, fixture)) {
					t.Fatal("selected-key preview changed host or management evidence")
				}
				replace := []string{"replace", "agent-config", "preferences", "--key", "/model", "--yes", "--expect-plan", fingerprint}
				linkTo("prefs.toml")
				testkit.WriteFile(t, workspace, ".codex/prefs.toml", edited+string(mcpValues))
				shared := nativefixture.SnapshotRecoveryEvidence(t, fixture)
				if current := nativeReplacementFingerprint(t, fixture, "/model"); current == fingerprint {
					t.Fatal("a new shared document was not bound into replacement approval")
				}
				if code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, "none", replace...); code != 1 || !maps.Equal(shared, nativefixture.SnapshotRecoveryEvidence(t, fixture)) {
					t.Fatalf("replacement used consent from before the routes shared a file: %d\n%s%s", code, stdout, stderr)
				}

				linkTo(route.link)
				testkit.WriteFile(t, workspace, ".codex/prefs.toml", edited)
				nativefixture.RunRecoveryCommand(t, fixture, "none", replace...)
				actual := testkit.ReadFile(t, destination)
				nativefixture.RequireSharedValues(t, configdocument.TOML, actual, "model='managed'\nuser_setting=true\n", true)
				stateAfter, err := statefile.DecodePersistedFacts(t.Context(), testkit.ReadFile(t, paths.StatefilePath))
				if err != nil || !slices.EqualFunc(stateBefore.ManagedAggregates(), stateAfter.ManagedAggregates(), durable.ManagedAggregateState.Equal) {
					t.Fatal("selected-key replacement changed retained management", err)
				}

				before = nativefixture.SnapshotRecoveryEvidence(t, fixture)
				code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, "none", "apply", "--yes", "--json")
				if code != 1 || !strings.Contains(clijson.FailureText(t, stdout, stderr, "apply", "operation", true), "config.toml") {
					t.Fatalf("ordinary apply did not report the broken MCP route: %d\n%s%s", code, stdout, stderr)
				}
			} else {
				code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, "none", "replace", "agent-config", "preferences", "--key", "/model", "--dry-run", "--json")
				if code != 1 {
					t.Fatalf("preview accepted an unresolved MCP route: %d\n%s%s", code, stdout, stderr)
				}
				failure := clijson.DecodeFailure(t, stdout, "replace agent-config", "operation")
				if !strings.Contains(failure.Error.Message, "config.toml") || strings.Contains(failure.Error.Message, root) || len(failure.Guidance) != 1 || !strings.Contains(failure.Guidance[0].Message, "links") {
					t.Fatalf("unresolved route needs an actionable diagnostic without a resolved path: %+v", failure)
				}
			}
			if got, err := os.Readlink(mcp); err != nil || got != route.link {
				t.Fatal("command changed the MCP link", got, err)
			}
			if !maps.Equal(before, nativefixture.SnapshotRecoveryEvidence(t, fixture)) {
				t.Fatal("refusal changed host or management evidence")
			}
		})
	}
}
