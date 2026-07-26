package cli_test

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/hook"
	instructionsresource "github.com/isty2e/daem/internal/desired/instructions"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/desired/skill"
	sourcepkg "github.com/isty2e/daem/internal/supply/source"
)

func assertInstructionLocalSource(t *testing.T, instructions instructionsresource.Instructions, wantPath string) {
	t.Helper()

	source, ok := instructions.Source().Local()
	if !ok {
		t.Fatalf("Source = %#v, want local source", instructions.Source())
	}
	if source.Path() != wantPath || source.Mode() != sourcepkg.LocalSourceModeVendor {
		t.Fatalf("local source = %#v, want path %q vendor", source, wantPath)
	}
}

func findImportedSkill(t *testing.T, skills []skill.Skill, name string) skill.Skill {
	t.Helper()

	for _, skill := range skills {
		if skill.ID().Name() == name {
			return skill
		}
	}
	t.Fatalf("skill %q not found in %#v", name, skills)
	panic("unreachable")
}

func assertSkillLocalSource(t *testing.T, skill skill.Skill, wantPath string) {
	t.Helper()

	source, ok := skill.Source().Local()
	if !ok {
		t.Fatalf("Source = %#v, want local source", skill.Source())
	}
	if source.Path() != wantPath || source.Mode() != sourcepkg.LocalSourceModeVendor {
		t.Fatalf("local source = %#v, want path %q vendor", source, wantPath)
	}
}

func findImportedHook(t *testing.T, hooks []hook.Hook, name string) hook.Hook {
	t.Helper()

	for _, hook := range hooks {
		if hook.ID().Name() == name {
			return hook
		}
	}
	t.Fatalf("hooks = %#v, want %q", hooks, name)
	panic("unreachable")
}

func findImportedMCPServer(t *testing.T, servers []desiredmcp.Server, name string) desiredmcp.Server {
	t.Helper()

	for _, server := range servers {
		if server.ID().Name() == name {
			return server
		}
	}
	t.Fatalf("mcp servers = %#v, want %q", servers, name)
	panic("unreachable")
}

func findImportedInstruction(t *testing.T, instructions []instructionsresource.Instructions, name string) instructionsresource.Instructions {
	t.Helper()

	for _, instruction := range instructions {
		if instruction.ID().Name() == name {
			return instruction
		}
	}
	t.Fatalf("instructions = %#v, want %q", instructions, name)
	panic("unreachable")
}
