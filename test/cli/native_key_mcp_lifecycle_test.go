package cli_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func requireNativeMCPUserComments(t *testing.T, content []byte) {
	t.Helper()
	for _, comment := range []string{"# User preferences\n", "# User theme\n"} {
		if !bytes.Contains(content, []byte(comment)) {
			t.Fatalf("shared mutation lost an unmanaged comment: %s", content)
		}
	}
}

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
					content += `[[host_config]]
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
				runNativeLifecycleCommand(t, "lock", "--manifest", manifest)
				runNativeLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
				content, err := os.ReadFile(destination)
				if err != nil {
					t.Fatal(err)
				}
				expected := source + fmt.Sprintf("[mcp_servers.search]\ncommand = 'node'\nargs = [%q]\n", server)
				requireNativeSharedValues(t, configdocument.TOML, content, expected, true)
				if !bytes.Contains(content, []byte("[mcp_servers.search]\n")) {
					t.Fatalf("generated Codex MCP header does not use bare TOML segments: %s", content)
				}
				requireNativeSharedValues(t, configdocument.TOML, content, outside, true)
				requireNativeMCPUserComments(t, content)
				requireNativeSharedMode(t, destination, 0o600)
				requireNativeLifecycleMetadata(t, manifest, true)
				status := runNativeLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json")
				requireNativeLifecycleClean(t, status)
				if plan := clijson.DecodePlan(t, []byte(status)); plan.ActionCount != 2 {
					t.Fatal("mixed Apply did not retain both managed subjects", status)
				}
				runNativeLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
			}

			testkit.WriteFile(t, workspace, "daem.toml", `version = 1
targets = ["codex"]
[[host_config]]
id = "preferences"
path = "config.toml"
source = "./preferences.toml"
ownership = "keys"
`)
			runNativeLifecycleCommand(t, "lock", "--manifest", manifest)
			runNativeLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
			withoutMCP, err := os.ReadFile(destination)
			if err != nil {
				t.Fatal(err)
			}
			requireNativeSharedValues(t, configdocument.TOML, withoutMCP, "[mcp_servers.search]\ncommand='node'\nargs=['new.js']\n", false)
			requireNativeSharedValues(t, configdocument.TOML, withoutMCP, "[tui]\nnotifications = false\n", true)
			requireNativeSharedValues(t, configdocument.TOML, withoutMCP, outside, true)
			requireNativeMCPUserComments(t, withoutMCP)
			requireNativeLifecycleMetadata(t, manifest, true)
			requireNativeLifecycleClean(t, runNativeLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json"))
			runNativeLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")

			writeManifest("new.js", true)
			runNativeLifecycleCommand(t, "lock", "--manifest", manifest)
			runNativeLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")

			writeManifest("new.js", false)
			runNativeLifecycleCommand(t, "lock", "--manifest", manifest)
			runNativeLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
			content, err := os.ReadFile(destination)
			if err != nil {
				t.Fatal(err)
			}
			requireNativeSharedValues(t, configdocument.TOML, content, "[tui]\nnotifications = false\n", false)
			requireNativeSharedValues(t, configdocument.TOML, content, "[mcp_servers.search]\ncommand='node'\nargs=['new.js']\n", true)
			requireNativeSharedValues(t, configdocument.TOML, content, outside, true)
			requireNativeMCPUserComments(t, content)
			requireNativeLifecycleMetadata(t, manifest, false)
			requireNativeLifecycleClean(t, runNativeLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json"))
			runNativeLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")

			testkit.WriteFile(t, workspace, "daem.toml", "version = 1\ntargets = ['codex']\n")
			runNativeLifecycleCommand(t, "lock", "--manifest", manifest)
			runNativeLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
			content, err = os.ReadFile(destination)
			if err != nil {
				t.Fatal(err)
			}
			requireNativeSharedValues(t, configdocument.TOML, content, "[mcp_servers.search]\ncommand='node'\nargs=['new.js']\n", false)
			requireNativeSharedValues(t, configdocument.TOML, content, outside, true)
			requireNativeMCPUserComments(t, content)
			requireNativeLifecycleClean(t, runNativeLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json"))
		})
	}
}
