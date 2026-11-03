package cli_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativeRootInsertionWithMCPKeepsLeadingComment(t *testing.T) {
	const mcp = "\n[[mcp_server]]\nname='search'\ntransport='stdio'\ncommand='node'\n"
	for _, source := range []string{
		"# File settings",
		"# File settings\n\n[tui]\ntheme='light'\n",
		"# File settings\n\nkeep=true\n[tui]\ntheme='light'\n",
	} {
		t.Run(source, func(t *testing.T) {
			fixture := nativefixture.NewPresentation(t, "model='native-test'\n", source)
			workspace := filepath.Dir(fixture.Manifest)
			manifest := string(testkit.ReadFile(t, fixture.Manifest)) + mcp
			testkit.WriteFile(t, workspace, filepath.Base(fixture.Manifest), manifest)
			nativefixture.RunLifecycleCommand(t, "lock", "--manifest", fixture.Manifest)
			nativefixture.RunLifecycleCommand(t, "apply", "--manifest", fixture.Manifest, "--yes")

			content := testkit.ReadFile(t, fixture.Destination)
			if !bytes.HasPrefix(content, []byte("# File settings\n")) {
				t.Fatalf("joint native/MCP Apply displaced the document comment: %s", content)
			}
			nativefixture.RequireSharedValues(t, configdocument.TOML, content, "model='native-test'\n[mcp_servers.search]\ncommand='node'\n", true)
			nativefixture.RequireSharedValues(t, configdocument.TOML, content, source, true)
			nativefixture.RequireLifecycleClean(t, nativefixture.RunLifecycleCommand(t, "status", "--manifest", fixture.Manifest, "--check", "--json"))
			nativefixture.RunLifecycleCommand(t, "apply", "--manifest", fixture.Manifest, "--yes")

			testkit.WriteFile(t, workspace, filepath.Base(fixture.Manifest), "version=1\ntargets=['codex']\n"+mcp)
			nativefixture.RunLifecycleCommand(t, "lock", "--manifest", fixture.Manifest)
			nativefixture.RunLifecycleCommand(t, "apply", "--manifest", fixture.Manifest, "--yes")
			content = testkit.ReadFile(t, fixture.Destination)
			if !bytes.HasPrefix(content, []byte("# File settings\n")) {
				t.Fatalf("root-key removal displaced the document comment: %s", content)
			}
			nativefixture.RequireSharedValues(t, configdocument.TOML, content, "model='native-test'\n", false)
			nativefixture.RequireSharedValues(t, configdocument.TOML, content, "[mcp_servers.search]\ncommand='node'\n", true)
			nativefixture.RequireLifecycleClean(t, nativefixture.RunLifecycleCommand(t, "status", "--manifest", fixture.Manifest, "--check", "--json"))
		})
	}
}
