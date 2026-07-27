package profile

import (
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/target"
)

const (
	managedDirectoryAdapterVersion = "managed-directory-v1"
	managedDirectoryWriteRoute     = "managed-directory.write"
	skillManagedPathRemoveRoute    = "managed-path.remove"
)

var skillPlacements = []ManagedPathPlacement{
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "skill.project.agents", ResourceKind: entity.KindSkill,
		Scope: target.ScopeProject, Root: ".agents/skills", ContentKind: realization.PathProjectionDirectory,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "skill.project.claude", ResourceKind: entity.KindSkill,
		Scope: target.ScopeProject, Root: ".claude/skills", ContentKind: realization.PathProjectionDirectory,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "skill.project.opencode", ResourceKind: entity.KindSkill,
		Scope: target.ScopeProject, Root: ".opencode/skills", ContentKind: realization.PathProjectionDirectory,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "skill.project.pi", ResourceKind: entity.KindSkill,
		Scope: target.ScopeProject, Root: ".pi/skills", ContentKind: realization.PathProjectionDirectory,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "skill.global.agents", ResourceKind: entity.KindSkill,
		Scope: target.ScopeGlobal, Root: "~/.agents/skills", ContentKind: realization.PathProjectionDirectory,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "skill.global.codex", ResourceKind: entity.KindSkill,
		Scope: target.ScopeGlobal, Root: "~/.codex/skills", ContentKind: realization.PathProjectionDirectory,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "skill.global.claude", ResourceKind: entity.KindSkill,
		Scope: target.ScopeGlobal, Root: "~/.claude/skills", ContentKind: realization.PathProjectionDirectory,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "skill.global.opencode", ResourceKind: entity.KindSkill,
		Scope: target.ScopeGlobal, Root: "~/.config/opencode/skills", ContentKind: realization.PathProjectionDirectory,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "skill.global.pi", ResourceKind: entity.KindSkill,
		Scope: target.ScopeGlobal, Root: "~/.pi/agent/skills", ContentKind: realization.PathProjectionDirectory,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "skill.global.antigravity", ResourceKind: entity.KindSkill,
		Scope: target.ScopeGlobal, Root: "~/.gemini/config/skills", ContentKind: realization.PathProjectionDirectory,
	}),
}

var skillPlacementAdmissions = []PlacementAdmission{
	mustPlacementAdmission(target.TargetCodex, "skill.project.agents", true),
	mustPlacementAdmission(target.TargetOpenCode, "skill.project.agents", false),
	mustPlacementAdmission(target.TargetPi, "skill.project.agents", false),
	mustPlacementAdmission(target.TargetAntigravityCLI, "skill.project.agents", true),
	mustPlacementAdmission(target.TargetClaudeCode, "skill.project.claude", true),
	mustPlacementAdmission(target.TargetOpenCode, "skill.project.claude", false),
	mustPlacementAdmission(target.TargetOpenCode, "skill.project.opencode", true),
	mustPlacementAdmission(target.TargetPi, "skill.project.pi", true),
	mustPlacementAdmission(target.TargetCodex, "skill.global.agents", true),
	mustPlacementAdmission(target.TargetOpenCode, "skill.global.agents", false),
	mustPlacementAdmission(target.TargetPi, "skill.global.agents", false),
	mustPlacementAdmission(target.TargetCodex, "skill.global.codex", false),
	mustPlacementAdmission(target.TargetClaudeCode, "skill.global.claude", true),
	mustPlacementAdmission(target.TargetOpenCode, "skill.global.claude", false),
	mustPlacementAdmission(target.TargetOpenCode, "skill.global.opencode", true),
	mustPlacementAdmission(target.TargetPi, "skill.global.pi", true),
	mustPlacementAdmission(target.TargetAntigravityCLI, "skill.global.antigravity", true),
}

var skillDiscoveries = []DiscoveryLocation{
	mustDiscoveryLocation(target.TargetCodex, entity.KindSkill, target.ScopeProject, ".agents/skills", 0),
	mustDiscoveryLocation(target.TargetCodex, entity.KindSkill, target.ScopeGlobal, "~/.agents/skills", 0),
	mustDiscoveryLocation(target.TargetCodex, entity.KindSkill, target.ScopeGlobal, "~/.codex/skills", 10),
	mustDiscoveryLocation(target.TargetClaudeCode, entity.KindSkill, target.ScopeProject, ".claude/skills", 0),
	mustDiscoveryLocation(target.TargetClaudeCode, entity.KindSkill, target.ScopeGlobal, "~/.claude/skills", 0),
	mustDiscoveryLocation(target.TargetOpenCode, entity.KindSkill, target.ScopeProject, ".opencode/skills", 0),
	mustDiscoveryLocation(target.TargetOpenCode, entity.KindSkill, target.ScopeProject, ".claude/skills", 10),
	mustDiscoveryLocation(target.TargetOpenCode, entity.KindSkill, target.ScopeProject, ".agents/skills", 20),
	mustDiscoveryLocation(target.TargetOpenCode, entity.KindSkill, target.ScopeGlobal, "~/.config/opencode/skills", 0),
	mustDiscoveryLocation(target.TargetOpenCode, entity.KindSkill, target.ScopeGlobal, "~/.claude/skills", 10),
	mustDiscoveryLocation(target.TargetOpenCode, entity.KindSkill, target.ScopeGlobal, "~/.agents/skills", 20),
	mustDiscoveryLocation(target.TargetPi, entity.KindSkill, target.ScopeProject, ".pi/skills", 0),
	mustDiscoveryLocation(target.TargetPi, entity.KindSkill, target.ScopeProject, ".agents/skills", 10),
	mustDiscoveryLocation(target.TargetPi, entity.KindSkill, target.ScopeGlobal, "~/.pi/agent/skills", 0),
	mustDiscoveryLocation(target.TargetPi, entity.KindSkill, target.ScopeGlobal, "~/.agents/skills", 10),
	mustDiscoveryLocation(target.TargetAntigravityCLI, entity.KindSkill, target.ScopeProject, ".agents/skills", 0),
	mustDiscoveryLocation(target.TargetAntigravityCLI, entity.KindSkill, target.ScopeGlobal, "~/.gemini/config/skills", 0),
}

var skillRuntimeLocations = []RuntimeLocation{
	mustRuntimeLocation(target.TargetCodex, entity.KindSkill, target.ScopeGlobal, "/etc/codex/skills"),
}

func skillOperationRoutes() []OperationRoute {
	return managedPathOperationRoutes(
		skillPlacements,
		managedDirectoryWriteRoute,
		skillManagedPathRemoveRoute,
		managedDirectoryAdapterVersion,
	)
}
