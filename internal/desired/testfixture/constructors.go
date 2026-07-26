// Package testfixture provides constructor-only helpers for tests that consume
// canonical desired values across package boundaries.
package testfixture

import (
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/desired/hook"
	"github.com/isty2e/daem/internal/desired/hookasset"
	"github.com/isty2e/daem/internal/desired/instructions"
	"github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/target"
)

// TB is the test capability required by constructor helpers.
type TB interface {
	Helper()
	Fatalf(format string, args ...any)
}

// Defaults constructs validated environment defaults or fails the test.
func Defaults(test TB, scope target.Scope, installMode skill.InstallMode) desired.Defaults {
	test.Helper()
	value, err := desired.NewDefaults(scope, installMode)
	if err != nil {
		test.Fatalf("desired.NewDefaults returned error: %v", err)
	}
	return value
}

// Environment constructs a validated canonical environment or fails the test.
func Environment(test TB, spec desired.Spec) desired.Environment {
	test.Helper()
	value, err := desired.New(spec)
	if err != nil {
		test.Fatalf("desired.New returned error: %v", err)
	}
	return value
}

// Skill constructs a validated canonical skill or fails the test.
func Skill(test TB, spec skill.Spec) skill.Skill {
	test.Helper()
	value, err := skill.New(spec)
	if err != nil {
		test.Fatalf("skill.New returned error: %v", err)
	}
	return value
}

// SkillSet constructs a validated canonical skill set or fails the test.
func SkillSet(test TB, spec skill.SkillSetSpec) skill.SkillSet {
	test.Helper()
	value, err := skill.NewSkillSet(spec)
	if err != nil {
		test.Fatalf("skill.NewSkillSet returned error: %v", err)
	}
	return value
}

// Selector parses a canonical skill selector or fails the test.
func Selector(test TB, expression string) skill.Selector {
	test.Helper()
	value, err := skill.ParseSelector(expression)
	if err != nil {
		test.Fatalf("skill.ParseSelector returned error: %v", err)
	}
	return value
}

// Hook constructs a validated canonical hook or fails the test.
func Hook(test TB, spec hook.Spec) hook.Hook {
	test.Helper()
	value, err := hook.New(spec)
	if err != nil {
		test.Fatalf("hook.New returned error: %v", err)
	}
	return value
}

// HookAsset constructs a validated canonical hook asset or fails the test.
func HookAsset(test TB, spec hookasset.Spec) hookasset.HookAsset {
	test.Helper()
	value, err := hookasset.New(spec)
	if err != nil {
		test.Fatalf("hookasset.New returned error: %v", err)
	}
	return value
}

// Rendering constructs a validated canonical instruction rendering or fails the test.
func Rendering(test TB, renderTo string, mode instructions.RenderMode) instructions.Rendering {
	test.Helper()
	value, err := instructions.NewRendering(renderTo, mode)
	if err != nil {
		test.Fatalf("instructions.NewRendering returned error: %v", err)
	}
	return value
}

// Instructions constructs validated canonical instructions or fails the test.
func Instructions(test TB, spec instructions.Spec) instructions.Instructions {
	test.Helper()
	value, err := instructions.New(spec)
	if err != nil {
		test.Fatalf("instructions.New returned error: %v", err)
	}
	return value
}

// MCPCommand constructs an ambient MCP command or fails the test.
func MCPCommand(test TB, name string) mcp.Command {
	test.Helper()
	value, err := mcp.NewAmbientCommand(name)
	if err != nil {
		test.Fatalf("mcp.NewAmbientCommand returned error: %v", err)
	}
	return value
}

// MCPEnvReference constructs a secret-free MCP environment reference or fails the test.
func MCPEnvReference(test TB, name string) mcp.EnvReference {
	test.Helper()
	value, err := mcp.NewEnvReference(name)
	if err != nil {
		test.Fatalf("mcp.NewEnvReference returned error: %v", err)
	}
	return value
}

// MCPStdio constructs a validated stdio MCP transport or fails the test.
func MCPStdio(
	test TB,
	command mcp.Command,
	args []string,
	env map[string]mcp.EnvReference,
) mcp.Transport {
	test.Helper()
	value, err := mcp.NewStdioTransport(command, args, env)
	if err != nil {
		test.Fatalf("mcp.NewStdioTransport returned error: %v", err)
	}
	return value
}

// MCPBinding constructs a validated MCP binding or fails the test.
func MCPBinding(test TB, selected target.Target, scope target.Scope, transport mcp.Transport, onAbsent mcp.OnAbsent) mcp.Binding {
	test.Helper()
	value, err := mcp.NewBinding(selected, scope, transport, onAbsent)
	if err != nil {
		test.Fatalf("mcp.NewBinding returned error: %v", err)
	}
	return value
}

// MCPServer constructs a validated canonical MCP server or fails the test.
func MCPServer(test TB, spec mcp.Spec) mcp.Server {
	test.Helper()
	value, err := mcp.New(spec)
	if err != nil {
		test.Fatalf("mcp.New returned error: %v", err)
	}
	return value
}

// ExtensionSource constructs a validated extension source or fails the test.
func ExtensionSource(test TB, kind extension.SourceKind, ref string) extension.SourceRef {
	test.Helper()
	value, err := extension.NewSourceRef(kind, ref)
	if err != nil {
		test.Fatalf("extension.NewSourceRef returned error: %v", err)
	}
	return value
}

// Extension constructs a validated canonical extension or fails the test.
func Extension(test TB, spec extension.Spec) extension.Extension {
	test.Helper()
	value, err := extension.New(spec)
	if err != nil {
		test.Fatalf("extension.New returned error: %v", err)
	}
	return value
}
