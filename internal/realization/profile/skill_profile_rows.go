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
		ID: "skill.project.agents", ConsumerTargets: []target.Target{target.TargetCodex, target.TargetAntigravityCLI},
		ResourceKind: entity.KindSkill, Scope: target.ScopeProject, Root: ".agents/skills",
		DefaultPlacement: true, ContentKind: realization.PathProjectionDirectory,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "skill.project.claude", ConsumerTargets: []target.Target{target.TargetClaudeCode},
		ResourceKind: entity.KindSkill, Scope: target.ScopeProject, Root: ".claude/skills",
		DefaultPlacement: true, ContentKind: realization.PathProjectionDirectory,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "skill.project.opencode", ConsumerTargets: []target.Target{target.TargetOpenCode},
		ResourceKind: entity.KindSkill, Scope: target.ScopeProject, Root: ".opencode/skills",
		DefaultPlacement: true, ContentKind: realization.PathProjectionDirectory,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "skill.project.pi", ConsumerTargets: []target.Target{target.TargetPi},
		ResourceKind: entity.KindSkill, Scope: target.ScopeProject, Root: ".pi/skills",
		DefaultPlacement: true, ContentKind: realization.PathProjectionDirectory,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "skill.global.agents", ConsumerTargets: []target.Target{target.TargetCodex},
		ResourceKind: entity.KindSkill, Scope: target.ScopeGlobal, Root: "~/.agents/skills",
		DefaultPlacement: true, ContentKind: realization.PathProjectionDirectory,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "skill.global.claude", ConsumerTargets: []target.Target{target.TargetClaudeCode},
		ResourceKind: entity.KindSkill, Scope: target.ScopeGlobal, Root: "~/.claude/skills",
		DefaultPlacement: true, ContentKind: realization.PathProjectionDirectory,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "skill.global.opencode", ConsumerTargets: []target.Target{target.TargetOpenCode},
		ResourceKind: entity.KindSkill, Scope: target.ScopeGlobal, Root: "~/.config/opencode/skills",
		DefaultPlacement: true, ContentKind: realization.PathProjectionDirectory,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "skill.global.pi", ConsumerTargets: []target.Target{target.TargetPi},
		ResourceKind: entity.KindSkill, Scope: target.ScopeGlobal, Root: "~/.pi/agent/skills",
		DefaultPlacement: true, ContentKind: realization.PathProjectionDirectory,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "skill.global.antigravity", ConsumerTargets: []target.Target{target.TargetAntigravityCLI},
		ResourceKind: entity.KindSkill, Scope: target.ScopeGlobal, Root: "~/.gemini/config/skills",
		DefaultPlacement: true, ContentKind: realization.PathProjectionDirectory,
	}),
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
