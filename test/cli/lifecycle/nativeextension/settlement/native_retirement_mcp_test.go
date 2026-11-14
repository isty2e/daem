//go:build darwin || linux

package cli_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativePiRetirementWithSharedMCP(t *testing.T) {
	for _, boundary := range []string{"complete", "host", "state"} {
		t.Run(boundary, func(t *testing.T) {
			fixture := nativefixture.NewPiInvocation(t, "")
			manifest := string(testkit.ReadFile(t, fixture.Manifest)) + `
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
			destination := filepath.Join(fixture.Workspace, ".codex", "config.toml")
			t.Setenv("DAEM_TEST_INSTRUCTIONS_TARGET", destination)
			testkit.WriteFile(t, filepath.Dir(destination), filepath.Base(destination), "# User settings\nforeign=true\n")
			testkit.WriteFile(t, fixture.Workspace, "codex-preferences.toml", "model='fixture-model'\nobsolete=true\n")
			testkit.WriteFile(t, fixture.Workspace, "daem.toml", manifest)
			fixture.Require(t, "lock")
			fixture.Require(t, "apply", "--yes")
			testkit.WriteFile(t, fixture.Workspace, "codex-preferences.toml", "model='fixture-model'\n")
			testkit.WriteFile(t, fixture.Workspace, "daem.toml", strings.Replace(manifest, "before.js", "after.js", 1))
			testkit.WriteFile(t, filepath.Dir(fixture.Settings), "settings.json", `{"theme":"fixture-theme","foreign":{"retained":true},"packages":["npm:@acme/keep@1"]}`)
			fixture.Require(t, "lock")
			if boundary == "complete" {
				fixture.Require(t, "apply", "--yes")
			} else {
				recoveryFixture := nativefixture.RecoveryFixture{Manifest: fixture.Manifest, Destination: destination}
				code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, recoveryFixture, "native-ordinary-"+boundary, "apply", "--yes")
				if code != nativefixture.RecoveryExit {
					t.Fatalf("shared retirement did not reach its process-loss boundary: %d %s %s", code, stdout, stderr)
				}
				fixture.Require(t, "recover", "--dry-run")
				fixture.Require(t, "recover", "--yes")
				fixture.Require(t, "apply", "--yes")
			}
			fixture.LaunchProtection(t)
			before := bytes.TrimPrefix(testkit.ReadFile(t, fixture.InstructionsBefore), []byte("present\n"))
			nativefixture.RequireSharedValues(t, configdocument.TOML, before, "model='fixture-model'\nobsolete=true\n[mcp_servers.search]\nargs=['before.js']\n", true)
			content := testkit.ReadFile(t, destination)
			nativefixture.RequireSharedValues(t, configdocument.TOML, content, "model='fixture-model'\nforeign=true\n[mcp_servers.search]\nargs=['after.js']\n", true)
			nativefixture.RequireSharedValues(t, configdocument.TOML, content, "obsolete=true\n", false)
			if !bytes.Contains(content, []byte("# User settings\n")) {
				t.Fatal("retirement lost an unmanaged comment")
			}
			fixture.Require(t, "status", "--check", "--json")
			fixture.Require(t, "apply", "--yes")
			if got := string(testkit.ReadFile(t, fixture.Calls)); got != "install\ninstall\ninstall\n" || !bytes.Equal(content, testkit.ReadFile(t, destination)) {
				t.Fatal("shared retirement/recovery repeated installation or suffix effects")
			}
			testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
		})
	}
}
