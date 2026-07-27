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
		ID: "instructions.project.agents", ResourceKind: entity.KindInstructions,
		Scope: target.ScopeProject, Root: "AGENTS.md", ContentKind: realization.PathProjectionFile,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "instructions.project.claude", ResourceKind: entity.KindInstructions,
		Scope: target.ScopeProject, Root: "CLAUDE.md", ContentKind: realization.PathProjectionFile,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "instructions.project.gemini", ResourceKind: entity.KindInstructions,
		Scope: target.ScopeProject, Root: "GEMINI.md", ContentKind: realization.PathProjectionFile,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "instructions.global.codex", ResourceKind: entity.KindInstructions,
		Scope: target.ScopeGlobal, Root: "~/.codex/AGENTS.md", ContentKind: realization.PathProjectionFile,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "instructions.global.claude", ResourceKind: entity.KindInstructions,
		Scope: target.ScopeGlobal, Root: "~/.claude/CLAUDE.md", ContentKind: realization.PathProjectionFile,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "instructions.global.opencode", ResourceKind: entity.KindInstructions,
		Scope: target.ScopeGlobal, Root: "~/.config/opencode/AGENTS.md", ContentKind: realization.PathProjectionFile,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "instructions.global.pi", ResourceKind: entity.KindInstructions,
		Scope: target.ScopeGlobal, Root: "~/.pi/agent/AGENTS.md", ContentKind: realization.PathProjectionFile,
	}),
	mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "instructions.global.antigravity", ResourceKind: entity.KindInstructions,
		Scope: target.ScopeGlobal, Root: "~/.gemini/GEMINI.md", ContentKind: realization.PathProjectionFile,
	}),
}

var instructionPlacementAdmissions = []PlacementAdmission{
	mustPlacementAdmission(target.TargetCodex, "instructions.project.agents", true),
	mustPlacementAdmission(target.TargetOpenCode, "instructions.project.agents", true),
	mustPlacementAdmission(target.TargetPi, "instructions.project.agents", true),
	mustPlacementAdmission(target.TargetAntigravityCLI, "instructions.project.agents", true),
	mustPlacementAdmission(target.TargetClaudeCode, "instructions.project.claude", true),
	mustPlacementAdmission(target.TargetAntigravityCLI, "instructions.project.gemini", false),
	mustPlacementAdmission(target.TargetCodex, "instructions.global.codex", true),
	mustPlacementAdmission(target.TargetClaudeCode, "instructions.global.claude", true),
	mustPlacementAdmission(target.TargetOpenCode, "instructions.global.opencode", true),
	mustPlacementAdmission(target.TargetPi, "instructions.global.pi", true),
	mustPlacementAdmission(target.TargetAntigravityCLI, "instructions.global.antigravity", true),
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
