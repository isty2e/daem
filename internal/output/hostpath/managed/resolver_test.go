package managed

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/output"
	pihostpath "github.com/isty2e/daem/internal/output/hostpath/pi"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestResolverComposesPiOverrideWithGenericFallback(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	projectRoot := t.TempDir()
	t.Setenv(pihostpath.AgentRootEnvironmentVariable, root)
	t.Setenv("HOME", home)
	resolver := Resolver(daempaths.Paths{
		ManifestRoot: projectRoot,
		DataDir:      filepath.Join(projectRoot, ".daem"),
	})

	piDestination, err := output.Parse(aggregate.PiGlobalMCPConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := resolver.Resolve(piDestination)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(root, "mcp.json") {
		t.Fatalf("Pi global MCP path = %q", got)
	}

	other, err := output.Parse("~/AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	got, err = resolver.Resolve(other)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(home, "AGENTS.md") {
		t.Fatalf("generic fallback path = %q", got)
	}
}
