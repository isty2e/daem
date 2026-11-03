//go:build darwin || linux

package cli_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func nativePiMCPInvocationFixture(t *testing.T, mutation string) (nativefixture.PiInvocation, string) {
	t.Helper()
	fixture := nativefixture.NewPiInvocation(t, mutation)
	manifest := string(testkit.ReadFile(t, fixture.Manifest)) + "\n[[mcp_server]]\nname='search'\ntargets=['opencode']\ntransport='stdio'\ncommand='node'\nargs=['pending.js']\n"
	testkit.WriteFile(t, fixture.Workspace, "daem.toml", manifest)
	fixture.Require(t, "lock")
	return fixture, filepath.Join(fixture.Workspace, "opencode.json")
}

func TestNativePiInvocationFailureStopsMCPChanges(t *testing.T) {
	for _, mutation := range []string{"key", "command", "ordinary"} {
		t.Run(mutation, func(t *testing.T) {
			fixture, destination := nativePiMCPInvocationFixture(t, mutation)
			t.Setenv("DAEM_TEST_INSTRUCTIONS_TARGET", destination)
			if code, stdout, stderr := fixture.Run("apply", "--yes"); code == 0 {
				t.Fatalf("failed invocation or changed MCP document was accepted: %s %s", stdout, stderr)
			}
			fixture.LaunchProtection(t)
			if mutation == "ordinary" {
				if got := string(testkit.ReadFile(t, destination)); got != "host-produced\n" {
					t.Fatal("suffix overwrote an unplanned host mutation")
				}
			} else {
				testkit.AssertPathMissing(t, destination)
			}
			state, err := statefile.Decode(testkit.ReadFile(t, filepath.Join(fixture.Workspace, ".daem", "state.json")))
			if err != nil {
				t.Fatal(err)
			}
			for _, managed := range state.ManagedAggregates() {
				if managed.Contribution().Target() == "opencode" {
					t.Fatal("failed suffix recorded an unapplied MCP contribution")
				}
			}
			if got := string(testkit.ReadFile(t, fixture.Calls)); got != "install\ninstall\n" {
				t.Fatal("failed MCP suffix skipped or replayed its command")
			}
			testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
		})
	}
}

func TestNativePiMCPSuffixProcessLossRecoversWithoutCommandReplay(t *testing.T) {
	for _, boundary := range []string{"host", "state"} {
		t.Run(boundary, func(t *testing.T) {
			fixture, destination := nativePiMCPInvocationFixture(t, "")
			recoveryFixture := nativefixture.RecoveryFixture{Manifest: fixture.Manifest, Destination: destination}
			code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, recoveryFixture, "native-ordinary-"+boundary, "apply", "--yes")
			if code != nativefixture.RecoveryExit {
				t.Fatalf("MCP suffix did not reach its actual process-loss boundary: %d %s %s", code, stdout, stderr)
			}
			fixture.LaunchProtection(t)
			fixture.Require(t, "recover", "--dry-run")
			fixture.Require(t, "recover", "--yes")
			fixture.Require(t, "apply", "--yes")
			if !bytes.Contains(testkit.ReadFile(t, destination), []byte("pending.js")) {
				t.Fatal("MCP recovery/retry did not settle its desired contribution")
			}
			if got := string(testkit.ReadFile(t, fixture.Calls)); got != "install\ninstall\n" {
				t.Fatal("MCP recovery/retry replayed a completed installation")
			}
			fixture.Require(t, "status", "--check", "--json")
			testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
		})
	}
}

func TestNativePiSharedCodexMCPSuffixRecoversWithoutCommandReplay(t *testing.T) {
	for _, boundary := range []string{"host", "state"} {
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
args = ["shared.js"]
`
			destination := filepath.Join(fixture.Workspace, ".codex", "config.toml")
			testkit.WriteFile(t, fixture.Workspace, "codex-preferences.toml", "model='fixture-model'\n")
			testkit.WriteFile(t, filepath.Dir(destination), filepath.Base(destination), "# User settings\nforeign=true\n")
			testkit.WriteFile(t, fixture.Workspace, "daem.toml", manifest)
			fixture.Require(t, "lock")
			fixture.Require(t, "apply", "--yes")
			testkit.WriteFile(t, fixture.Workspace, "daem.toml", strings.Replace(manifest, "shared.js", "updated.js", 1))
			testkit.WriteFile(t, filepath.Dir(fixture.Settings), "settings.json", `{"theme":"fixture-theme","foreign":{"retained":true},"packages":["npm:@acme/keep@1"]}`)
			fixture.Require(t, "lock")
			recoveryFixture := nativefixture.RecoveryFixture{Manifest: fixture.Manifest, Destination: destination}
			code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, recoveryFixture, "native-ordinary-"+boundary, "apply", "--yes")
			if code != nativefixture.RecoveryExit {
				t.Fatalf("shared MCP suffix did not reach its process-loss boundary: %d %s %s", code, stdout, stderr)
			}
			fixture.LaunchProtection(t)
			fixture.Require(t, "recover", "--yes")
			fixture.Require(t, "apply", "--yes")
			content := testkit.ReadFile(t, destination)
			nativefixture.RequireSharedValues(t, configdocument.TOML, content, "model='fixture-model'\nforeign=true\n[mcp_servers.search]\nargs=['updated.js']\n", true)
			if !bytes.Contains(content, []byte("# User settings\n")) {
				t.Fatal("shared MCP recovery lost an unmanaged comment")
			}
			if got := string(testkit.ReadFile(t, fixture.Calls)); got != "install\ninstall\ninstall\n" {
				t.Fatal("shared MCP recovery replayed its completed installation")
			}
			fixture.Require(t, "status", "--check", "--json")
			testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
		})
	}
}
