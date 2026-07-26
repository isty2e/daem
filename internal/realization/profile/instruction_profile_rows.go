package profile

import (
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/target"
)

const (
	managedInstructionAdapterVersion  = "managed-instruction-file-v1"
	managedInstructionWriteRoute      = "managed-instruction-file.write"
	instructionManagedPathRemoveRoute = "managed-path.remove"
)

var instructionPlacements = []ManagedPathPlacement{
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "instructions.project.agents", ConsumerTargets: []target.Target{
			target.TargetCodex, target.TargetOpenCode, target.TargetPi, target.TargetAntigravityCLI,
		}, ResourceKind: entity.KindInstructions, Scope: target.ScopeProject, Root: "AGENTS.md",
		DefaultPlacement: true, ContentKind: realization.PathProjectionFile,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "instructions.project.claude", ConsumerTargets: []target.Target{target.TargetClaudeCode},
		ResourceKind: entity.KindInstructions, Scope: target.ScopeProject, Root: "CLAUDE.md",
		DefaultPlacement: true, ContentKind: realization.PathProjectionFile,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "instructions.project.gemini", ConsumerTargets: []target.Target{target.TargetAntigravityCLI},
		ResourceKind: entity.KindInstructions, Scope: target.ScopeProject, Root: "GEMINI.md",
		ContentKind: realization.PathProjectionFile,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "instructions.global.codex", ConsumerTargets: []target.Target{target.TargetCodex},
		ResourceKind: entity.KindInstructions, Scope: target.ScopeGlobal, Root: "~/.codex/AGENTS.md",
		DefaultPlacement: true, ContentKind: realization.PathProjectionFile,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "instructions.global.claude", ConsumerTargets: []target.Target{target.TargetClaudeCode},
		ResourceKind: entity.KindInstructions, Scope: target.ScopeGlobal, Root: "~/.claude/CLAUDE.md",
		DefaultPlacement: true, ContentKind: realization.PathProjectionFile,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "instructions.global.opencode", ConsumerTargets: []target.Target{target.TargetOpenCode},
		ResourceKind: entity.KindInstructions, Scope: target.ScopeGlobal, Root: "~/.config/opencode/AGENTS.md",
		DefaultPlacement: true, ContentKind: realization.PathProjectionFile,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "instructions.global.pi", ConsumerTargets: []target.Target{target.TargetPi},
		ResourceKind: entity.KindInstructions, Scope: target.ScopeGlobal, Root: "~/.pi/agent/AGENTS.md",
		DefaultPlacement: true, ContentKind: realization.PathProjectionFile,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "instructions.global.antigravity", ConsumerTargets: []target.Target{target.TargetAntigravityCLI},
		ResourceKind: entity.KindInstructions, Scope: target.ScopeGlobal, Root: "~/.gemini/GEMINI.md",
		DefaultPlacement: true, ContentKind: realization.PathProjectionFile,
	}),
}

var instructionDiscoveries = []DiscoveryLocation{
	mustDiscoveryLocation(target.TargetCodex, entity.KindInstructions, target.ScopeProject, "AGENTS.override.md", -10),
	mustDiscoveryLocation(target.TargetCodex, entity.KindInstructions, target.ScopeProject, "AGENTS.md", 0),
	mustDiscoveryLocation(target.TargetCodex, entity.KindInstructions, target.ScopeGlobal, "~/.codex/AGENTS.override.md", -10),
	mustDiscoveryLocation(target.TargetCodex, entity.KindInstructions, target.ScopeGlobal, "~/.codex/AGENTS.md", 0),
	mustDiscoveryLocation(target.TargetClaudeCode, entity.KindInstructions, target.ScopeProject, "CLAUDE.md", 0),
	mustDiscoveryLocation(target.TargetClaudeCode, entity.KindInstructions, target.ScopeProject, ".claude/CLAUDE.md", 10),
	mustDiscoveryLocation(target.TargetClaudeCode, entity.KindInstructions, target.ScopeGlobal, "~/.claude/CLAUDE.md", 0),
	mustDiscoveryLocation(target.TargetOpenCode, entity.KindInstructions, target.ScopeProject, "AGENTS.md", 0),
	mustDiscoveryLocation(target.TargetOpenCode, entity.KindInstructions, target.ScopeProject, "CLAUDE.md", 10),
	mustDiscoveryLocation(target.TargetOpenCode, entity.KindInstructions, target.ScopeGlobal, "~/.config/opencode/AGENTS.md", 0),
	mustDiscoveryLocation(target.TargetOpenCode, entity.KindInstructions, target.ScopeGlobal, "~/.claude/CLAUDE.md", 10),
	mustDiscoveryLocation(target.TargetPi, entity.KindInstructions, target.ScopeProject, "AGENTS.md", 0),
	mustDiscoveryLocation(target.TargetPi, entity.KindInstructions, target.ScopeProject, "CLAUDE.md", 10),
	mustDiscoveryLocation(target.TargetPi, entity.KindInstructions, target.ScopeGlobal, "~/.pi/agent/AGENTS.md", 0),
	mustDiscoveryLocation(target.TargetAntigravityCLI, entity.KindInstructions, target.ScopeProject, "AGENTS.md", 0),
	mustDiscoveryLocation(target.TargetAntigravityCLI, entity.KindInstructions, target.ScopeProject, "GEMINI.md", 5),
	mustDiscoveryLocation(target.TargetAntigravityCLI, entity.KindInstructions, target.ScopeGlobal, "~/.gemini/GEMINI.md", 0),
}

var instructionRuntimeLocations = []RuntimeLocation{
	mustRuntimeLocation(target.TargetClaudeCode, entity.KindInstructions, target.ScopeProject, "CLAUDE.local.md"),
}

func instructionOperationRoutes() []OperationRoute {
	return managedPathOperationRoutes(
		instructionPlacements,
		managedInstructionWriteRoute,
		instructionManagedPathRemoveRoute,
		managedInstructionAdapterVersion,
	)
}
