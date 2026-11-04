package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativeKeyPreviewDoesNotDisableMCPModePolicy(t *testing.T) {
	for _, native := range []bool{false, true} {
		name := "MCP only"
		if native {
			name = "MCP and native key in the same document"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", filepath.Join(root, "home"))
			t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
			t.Setenv("CODEX_HOME", filepath.Join(root, "codex-home"))
			workspace := filepath.Join(root, "workspace")
			manifest := filepath.Join(workspace, "daem.toml")
			content := `version = 1
targets = ["codex"]
[[mcp_server]]
name = "search"
transport = "stdio"
command = "node"
args = ["server.js"]
`
			if native {
				content += `[[host_config]]
id = "preferences"
path = "config.toml"
source = "./preferences.toml"
ownership = "keys"
`
				testkit.WriteFile(t, workspace, "preferences.toml", "model = 'managed'\n")
			}
			testkit.WriteFile(t, workspace, "daem.toml", content)
			const original = "# Shared config\n[mcp_servers.search]\ncommand = 'node'\nargs = ['server.js']\n"
			testkit.WriteFile(t, workspace, ".codex/config.toml", original)
			destination := filepath.Join(workspace, ".codex", "config.toml")
			if err := os.Chmod(destination, 0o644); err != nil {
				t.Fatal(err)
			}
			nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
			plan := clijson.DecodePlan(t, []byte(nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--dry-run", "--manage-existing", "--json")))
			found := false
			for _, action := range plan.Actions {
				if action.Subject == nil || action.Subject.Namespace != "codex.project.mcp-server" {
					continue
				}
				found = true
				if action.Kind != "update" || action.Reason != "file_mode_changed" {
					t.Fatalf("MCP permission policy changed with native declaration: kind=%s reason=%s", action.Kind, action.Reason)
				}
			}
			if !found || plan.HasErrors {
				t.Fatal("preview lost or blocked the MCP participant")
			}
			after, err := os.ReadFile(destination)
			if err != nil || !bytes.Equal(after, []byte(original)) {
				t.Fatal("mixed preview changed the shared configuration", err)
			}
			nativefixture.RequireSharedMode(t, destination, 0o644)
		})
	}
}
