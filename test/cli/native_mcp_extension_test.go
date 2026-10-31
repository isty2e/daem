//go:build darwin || linux

package cli_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

func TestNativePiInvocationWithMCPChanges(t *testing.T) {
	for _, test := range []struct {
		name, host, destination, outside string
		scope                            target.Scope
		syntax                           configdocument.Syntax
	}{
		{"Pi project", "pi", ".pi/mcp.json", `{"foreign":true}`, target.ScopeProject, configdocument.JSON},
		{"Pi global", "pi", "", `{"foreign":true}`, target.ScopeGlobal, configdocument.JSON},
		{"independent OpenCode", "opencode", "opencode.json", `{"foreign":true}`, target.ScopeProject, configdocument.JSON},
		{"Pi project missing", "pi", ".pi/mcp.json", "", target.ScopeProject, configdocument.JSON},
		{"independent OpenCode missing", "opencode", "opencode.json", "", target.ScopeProject, configdocument.JSON},
		{"shared Codex existing", "codex", ".codex/config.toml", "# User settings\nforeign=true\n", target.ScopeProject, configdocument.TOML},
		{"shared Codex missing", "codex", ".codex/config.toml", "", target.ScopeProject, configdocument.TOML},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newNativePiInvocationFixtureAtScope(t, "", test.scope)
			fixture.addInstructions(t, "ordinary instructions\n")
			base := string(testkit.ReadFile(t, fixture.manifest))
			if test.host == "codex" {
				testkit.WriteFile(t, fixture.workspace, "codex-preferences.toml", "model='fixture-model'\n")
				base += "\n[[host_config]]\nid='codex-preferences'\ntargets=['codex']\npath='config.toml'\nsource='./codex-preferences.toml'\nownership='keys'\n"
			}
			destination := filepath.Join(fixture.workspace, filepath.FromSlash(test.destination))
			if test.scope == target.ScopeGlobal {
				destination = filepath.Join(os.Getenv("PI_CODING_AGENT_DIR"), "mcp.json")
			}
			if test.outside != "" {
				testkit.WriteFile(t, filepath.Dir(destination), filepath.Base(destination), test.outside)
			}
			t.Setenv("DAEM_TEST_INSTRUCTIONS_TARGET", destination)
			var previous []byte
			if test.outside != "" {
				previous = []byte(test.outside)
			}
			for index, script := range []string{"first.js", "updated.js", ""} {
				manifest := base
				if script != "" {
					manifest += fmt.Sprintf("\n[[mcp_server]]\nname='search'\ntargets=[%q]\nscope=%q\ntransport='stdio'\ncommand='node'\nargs=[%q]\n", test.host, test.scope, script)
					if test.host == "pi" {
						manifest += "backend='native'\n"
					}
				}
				testkit.WriteFile(t, fixture.workspace, "daem.toml", manifest)
				if index != 0 {
					testkit.WriteFile(t, filepath.Dir(fixture.settings), "settings.json", `{"theme":"fixture-theme","foreign":{"retained":true},"packages":["npm:@acme/keep@1"]}`)
				}
				fixture.require(t, "lock")
				fixture.require(t, "apply", "--dry-run")
				fixture.require(t, "apply", "--yes")
				fixture.launchProtection(t)
				before := testkit.ReadFile(t, fixture.instructionsBefore)
				if previous == nil {
					// A selected sibling key may create the shared document in the prefix.
					if test.host != "codex" && string(before) != "absent\n" {
						t.Fatal("MCP document appeared before the invocation")
					}
				} else if !bytes.Equal(before, append([]byte("present\n"), previous...)) && test.host != "codex" {
					t.Fatal("MCP suffix changed before the invocation")
				}
				if bytes.Contains(before, []byte(script)) && script != "" {
					t.Fatal("desired MCP update was applied before the invocation")
				}
				content := testkit.ReadFile(t, destination)
				if script != "" && !bytes.Contains(content, []byte(script)) || script == "" && bytes.Contains(content, []byte("updated.js")) {
					t.Fatalf("MCP suffix did not settle its selected change: %s", content)
				}
				if test.outside != "" {
					requireNativeSharedValues(t, test.syntax, content, test.outside, true)
				}
				if test.host == "codex" {
					requireNativeSharedValues(t, test.syntax, content, "model='fixture-model'\n", true)
					if test.outside != "" && !bytes.Contains(content, []byte("# User settings\n")) {
						t.Fatal("shared MCP suffix lost an unmanaged comment")
					}
				}
				previous = content
				fixture.require(t, "status", "--check", "--json")
				calls := testkit.ReadFile(t, fixture.calls)
				fixture.require(t, "apply", "--yes")
				if !bytes.Equal(calls, testkit.ReadFile(t, fixture.calls)) || !bytes.Equal(content, testkit.ReadFile(t, destination)) {
					t.Fatal("converged MCP installation repeated effects")
				}
			}
			if got := string(testkit.ReadFile(t, fixture.calls)); got != "install\ninstall\ninstall\ninstall\n" {
				t.Fatal("MCP changes skipped or repeated a selected installation")
			}
			testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
		})
	}
}
