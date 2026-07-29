package pihostpath

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestResolveAgentRootUsesDefaultCustomRelativeAndTildeRoots(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tilde and HOME semantics are Unix-specific")
	}
	home := t.TempDir()
	workDir := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(AgentRootEnvironmentVariable, "")

	defaultRoot, err := ResolveAgentRoot(AgentRootInput{WorkDir: workDir})
	if err != nil {
		t.Fatal(err)
	}
	if defaultRoot != filepath.Join(home, ".pi", "agent") {
		t.Fatalf("default root = %q", defaultRoot)
	}

	t.Setenv(AgentRootEnvironmentVariable, "relative/../agent")
	relativeRoot, err := ResolveAgentRoot(AgentRootInput{WorkDir: workDir})
	if err != nil {
		t.Fatal(err)
	}
	if relativeRoot != filepath.Join(workDir, "agent") {
		t.Fatalf("relative root = %q", relativeRoot)
	}

	t.Setenv(AgentRootEnvironmentVariable, "~/.custom-pi")
	tildeRoot, err := ResolveAgentRoot(AgentRootInput{WorkDir: workDir})
	if err != nil {
		t.Fatal(err)
	}
	if tildeRoot != filepath.Join(home, ".custom-pi") {
		t.Fatalf("tilde root = %q", tildeRoot)
	}
}

func TestResolveAgentRootRejectsUnsafeInputs(t *testing.T) {
	for _, input := range []AgentRootInput{
		{ExplicitRoot: " relative ", WorkDir: t.TempDir()},
		{ExplicitRoot: "relative", WorkDir: "relative"},
		{ExplicitRoot: "bad\x00root", WorkDir: t.TempDir()},
	} {
		if _, err := ResolveAgentRoot(input); err == nil {
			t.Fatalf("ResolveAgentRoot(%#v) succeeded", input)
		}
	}
}

func TestResolveAgentRootReadsCurrentEnvironment(t *testing.T) {
	workDir := t.TempDir()
	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	t.Setenv(AgentRootEnvironmentVariable, first)

	gotFirst, err := ResolveAgentRoot(AgentRootInput{WorkDir: workDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv(AgentRootEnvironmentVariable, second); err != nil {
		t.Fatal(err)
	}
	gotSecond, err := ResolveAgentRoot(AgentRootInput{WorkDir: workDir})
	if err != nil {
		t.Fatal(err)
	}
	if gotFirst != first || gotSecond != second {
		t.Fatalf("roots = %q, %q", gotFirst, gotSecond)
	}
}

func TestDestinationOverrideMatchesOnlyPiGlobalMCPDestination(t *testing.T) {
	root := t.TempDir()
	projectRoot := t.TempDir()
	t.Setenv(AgentRootEnvironmentVariable, root)
	override := DestinationOverride(projectRoot)

	piDestination, err := output.Parse(aggregate.PiGlobalMCPConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	got, matched, err := override(piDestination)
	if err != nil {
		t.Fatal(err)
	}
	if !matched || got != filepath.Join(root, "mcp.json") {
		t.Fatalf("Pi global MCP override = %q, %v", got, matched)
	}

	other, err := output.Parse("~/.agents/mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	if got, matched, err := override(other); err != nil || matched || got != "" {
		t.Fatalf("unrelated override = %q, %v, %v", got, matched, err)
	}
}
