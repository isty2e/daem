package managed

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/hostpath"
	codexhostpath "github.com/isty2e/daem/internal/output/hostpath/codex"
	pihostpath "github.com/isty2e/daem/internal/output/hostpath/pi"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestResolverComposesHostRootsWithEveryGenericRoot(t *testing.T) {
	piRoot := filepath.Join(t.TempDir(), "missing Pi root")
	codexRoot := filepath.Join(t.TempDir(), "missing Codex root")
	home := t.TempDir()
	projectRoot := t.TempDir()
	dataRoot := filepath.Join(projectRoot, ".daem")
	t.Setenv(pihostpath.AgentRootEnvironmentVariable, piRoot)
	t.Setenv(codexhostpath.HomeEnvironmentVariable, codexRoot)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	resolver := Resolver(daempaths.Paths{
		ManifestRoot: projectRoot,
		DataDir:      dataRoot,
	})

	cases := []struct {
		name     string
		portable string
		want     string
	}{
		{name: "legacy Pi MCP exception", portable: aggregate.PiGlobalMCPConfigPath, want: filepath.Join(piRoot, "mcp.json")},
		{name: "Pi host root", portable: "@pi-agent/agents/reviewer.md", want: filepath.Join(piRoot, "agents", "reviewer.md")},
		{name: "Codex host root", portable: "@codex-home/config.toml", want: filepath.Join(codexRoot, "config.toml")},
		{name: "project root", portable: ".pi/AGENTS.md", want: filepath.Join(projectRoot, ".pi", "AGENTS.md")},
		{name: "home root", portable: "~/.agents/AGENTS.md", want: filepath.Join(home, ".agents", "AGENTS.md")},
		{name: "old Pi home destination", portable: "~/.pi/agent/AGENTS.md", want: filepath.Join(home, ".pi", "agent", "AGENTS.md")},
		{name: "old Codex home destination", portable: "~/.codex/config.toml", want: filepath.Join(home, ".codex", "config.toml")},
		{name: "data root", portable: "@data/hook-assets/example", want: filepath.Join(dataRoot, "hook-assets", "example")},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			destination, err := output.Parse(test.portable)
			if err != nil {
				t.Fatal(err)
			}
			got, err := resolver.Resolve(destination)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("Resolve(%q) = %q, want %q", test.portable, got, test.want)
			}
		})
	}
}

func TestResolverKeepsHostRootErrorsTargetSpecific(t *testing.T) {
	piRoot := t.TempDir()
	codexRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	projectRoot := t.TempDir()

	t.Setenv(pihostpath.AgentRootEnvironmentVariable, "file://%zz")
	t.Setenv(codexhostpath.HomeEnvironmentVariable, codexRoot)
	resolver := Resolver(daempaths.Paths{ManifestRoot: projectRoot, DataDir: filepath.Join(projectRoot, ".daem")})
	if _, err := resolver.Resolve(mustDestination(t, "@codex-home/config.toml")); err != nil {
		t.Fatalf("invalid Pi root blocked Codex resolution: %v", err)
	}
	if _, err := resolver.Resolve(mustDestination(t, "~/.agents/AGENTS.md")); err != nil {
		t.Fatalf("invalid Pi root blocked generic home resolution: %v", err)
	}
	if _, err := resolver.Resolve(mustDestination(t, "@pi-agent/agents/reviewer.md")); err == nil {
		t.Fatal("Pi root resolution ignored its invalid configured root")
	}

	t.Setenv(pihostpath.AgentRootEnvironmentVariable, piRoot)
	t.Setenv(codexhostpath.HomeEnvironmentVariable, "relative-codex-root")
	resolver = Resolver(daempaths.Paths{ManifestRoot: "relative-workdir", DataDir: filepath.Join(projectRoot, ".daem")})
	if _, err := resolver.Resolve(mustDestination(t, "@pi-agent/agents/reviewer.md")); err != nil {
		t.Fatalf("invalid Codex work directory blocked Pi resolution: %v", err)
	}
	if _, err := resolver.Resolve(mustDestination(t, "~/.agents/AGENTS.md")); err != nil {
		t.Fatalf("invalid Codex work directory blocked generic home resolution: %v", err)
	}
	if _, err := resolver.Resolve(mustDestination(t, "@codex-home/config.toml")); err == nil {
		t.Fatal("Codex root resolution ignored its invalid operation working directory")
	}
}

func TestResolverScopesRelativeCodexHomeToOperationWorkDir(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(pihostpath.AgentRootEnvironmentVariable, "")
	t.Setenv(codexhostpath.HomeEnvironmentVariable, "relative Codex root")
	resolver := Resolver(daempaths.Paths{ManifestRoot: workDir, DataDir: filepath.Join(workDir, ".daem")})

	got, err := resolver.Resolve(mustDestination(t, "@codex-home/config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(workDir, "relative Codex root", "config.toml")
	if got != want {
		t.Fatalf("relative Codex path = %q, want %q", got, want)
	}
}

func TestGenericResolverDoesNotInventHostRootMappings(t *testing.T) {
	resolver := hostpath.NewResolver(t.TempDir())
	for _, portable := range []string{"@pi-agent/agents/reviewer.md", "@codex-home/config.toml"} {
		destination := mustDestination(t, portable)
		if _, err := resolver.Resolve(destination); err == nil {
			t.Fatalf("generic resolver invented a physical mapping for %q", portable)
		}
	}
}

func mustDestination(t *testing.T, value string) output.Destination {
	t.Helper()
	destination, err := output.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return destination
}
