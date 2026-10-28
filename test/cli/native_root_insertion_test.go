package cli_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/test/testkit"
)

func TestNativeRootInsertionWithMCPKeepsLeadingComment(t *testing.T) {
	const mcp = "\n[[mcp_server]]\nname='search'\ntransport='stdio'\ncommand='node'\n"
	for _, source := range []string{
		"# File settings",
		"# File settings\n\n[tui]\ntheme='light'\n",
		"# File settings\n\nkeep=true\n[tui]\ntheme='light'\n",
	} {
		t.Run(source, func(t *testing.T) {
			fixture := nativePresentationFixture(t, "model='native-test'\n", source)
			workspace := filepath.Dir(fixture.manifest)
			manifest := string(testkit.ReadFile(t, fixture.manifest)) + mcp
			testkit.WriteFile(t, workspace, filepath.Base(fixture.manifest), manifest)
			runNativeLifecycleCommand(t, "lock", "--manifest", fixture.manifest)
			runNativeLifecycleCommand(t, "apply", "--manifest", fixture.manifest, "--yes")

			content := testkit.ReadFile(t, fixture.destination)
			if !bytes.HasPrefix(content, []byte("# File settings\n")) {
				t.Fatalf("joint native/MCP Apply displaced the document comment: %s", content)
			}
			requireNativeSharedValues(t, configdocument.TOML, content, "model='native-test'\n[mcp_servers.search]\ncommand='node'\n", true)
			requireNativeSharedValues(t, configdocument.TOML, content, source, true)
			requireNativeLifecycleClean(t, runNativeLifecycleCommand(t, "status", "--manifest", fixture.manifest, "--check", "--json"))
			runNativeLifecycleCommand(t, "apply", "--manifest", fixture.manifest, "--yes")

			testkit.WriteFile(t, workspace, filepath.Base(fixture.manifest), "version=1\ntargets=['codex']\n"+mcp)
			runNativeLifecycleCommand(t, "lock", "--manifest", fixture.manifest)
			runNativeLifecycleCommand(t, "apply", "--manifest", fixture.manifest, "--yes")
			content = testkit.ReadFile(t, fixture.destination)
			if !bytes.HasPrefix(content, []byte("# File settings\n")) {
				t.Fatalf("root-key removal displaced the document comment: %s", content)
			}
			requireNativeSharedValues(t, configdocument.TOML, content, "model='native-test'\n", false)
			requireNativeSharedValues(t, configdocument.TOML, content, "[mcp_servers.search]\ncommand='node'\n", true)
			requireNativeLifecycleClean(t, runNativeLifecycleCommand(t, "status", "--manifest", fixture.manifest, "--check", "--json"))
		})
	}
}
