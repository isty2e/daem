//go:build darwin || linux

package cli_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/test/testkit"
)

func TestNativePiRetirementWithSharedMCP(t *testing.T) {
	for _, boundary := range []string{"complete", "host", "state"} {
		t.Run(boundary, func(t *testing.T) {
			fixture := newNativePiInvocationFixture(t, "")
			manifest := string(testkit.ReadFile(t, fixture.manifest)) + `
[[host_config]]
id = "codex-preferences"
targets = ["codex"]
path = "config.toml"
source = "./codex-preferences.toml"
ownership = "keys"
[[mcp_server]]
name = "search"
targets = ["codex"]
transport = "stdio"
command = "node"
args = ["before.js"]
`
			destination := filepath.Join(fixture.workspace, ".codex", "config.toml")
			t.Setenv("DAEM_TEST_INSTRUCTIONS_TARGET", destination)
			testkit.WriteFile(t, filepath.Dir(destination), filepath.Base(destination), "# User settings\nforeign=true\n")
			testkit.WriteFile(t, fixture.workspace, "codex-preferences.toml", "model='fixture-model'\nobsolete=true\n")
			testkit.WriteFile(t, fixture.workspace, "daem.toml", manifest)
			fixture.require(t, "lock")
			fixture.require(t, "apply", "--yes")
			testkit.WriteFile(t, fixture.workspace, "codex-preferences.toml", "model='fixture-model'\n")
			testkit.WriteFile(t, fixture.workspace, "daem.toml", strings.Replace(manifest, "before.js", "after.js", 1))
			testkit.WriteFile(t, filepath.Dir(fixture.settings), "settings.json", `{"theme":"fixture-theme","foreign":{"retained":true},"packages":["npm:@acme/keep@1"]}`)
			fixture.require(t, "lock")
			if boundary == "complete" {
				fixture.require(t, "apply", "--yes")
			} else {
				recoveryFixture := nativeRecoveryFixture{manifest: fixture.manifest, destination: destination}
				code, stdout, stderr := invokeNativeRecoveryCommand(t, recoveryFixture, "native-ordinary-"+boundary, "apply", "--yes")
				if code != nativeRecoveryExit {
					t.Fatalf("shared retirement did not reach its process-loss boundary: %d %s %s", code, stdout, stderr)
				}
				fixture.require(t, "recover", "--dry-run")
				fixture.require(t, "recover", "--yes")
				fixture.require(t, "apply", "--yes")
			}
			fixture.launchProtection(t)
			before := bytes.TrimPrefix(testkit.ReadFile(t, fixture.instructionsBefore), []byte("present\n"))
			requireNativeSharedValues(t, configdocument.TOML, before, "model='fixture-model'\nobsolete=true\n[mcp_servers.search]\nargs=['before.js']\n", true)
			content := testkit.ReadFile(t, destination)
			requireNativeSharedValues(t, configdocument.TOML, content, "model='fixture-model'\nforeign=true\n[mcp_servers.search]\nargs=['after.js']\n", true)
			requireNativeSharedValues(t, configdocument.TOML, content, "obsolete=true\n", false)
			if !bytes.Contains(content, []byte("# User settings\n")) {
				t.Fatal("retirement lost an unmanaged comment")
			}
			fixture.require(t, "status", "--check", "--json")
			fixture.require(t, "apply", "--yes")
			if got := string(testkit.ReadFile(t, fixture.calls)); got != "install\ninstall\ninstall\n" || !bytes.Equal(content, testkit.ReadFile(t, destination)) {
				t.Fatal("shared retirement/recovery repeated installation or suffix effects")
			}
			testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
		})
	}
}
