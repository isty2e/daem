package listworkflow

import (
	"context"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

func TestRunPathsCoversEveryLocationFamilyAndManifestSelection(t *testing.T) {
	manifestPath := writePathInventoryManifest(t, `
version = 1
targets = ["codex", "claude-code", "opencode", "pi", "antigravity-cli"]

[defaults]
scope = "project"
install_mode = "copy"

[instructions.project]
source = "AGENTS.md"
targets = ["codex"]

[[skill]]
name = "review"
source = { path = "skills/review", mode = "vendor" }
targets = ["opencode"]

[skill.target.opencode]
install_to = ".agents/skills"

[hook_asset.guard]
source = "hooks/guard.sh"
kind = "file"
scope = "project"
executable = true

[[hook]]
name = "guard"
event = "PreToolUse"
command = "{hook_file:guard} --check"
targets = ["codex", "opencode"]

[[mcp_server]]
name = "repo-tools"
targets = ["pi"]
scope = "project"
transport = "stdio"
command = "repo-tools"

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
targets = ["claude-code"]
scope = "project"
source = { marketplace = "context7@market" }
`)

	result, err := RunPaths(context.Background(), Input{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("RunPaths returned error: %v", err)
	}
	entries := result.Inventory.Entries()
	if result.ManifestPath != manifestPath || len(entries) == 0 {
		t.Fatalf("RunPaths result = %#v, want populated inventory for %q", result, manifestPath)
	}

	assertLocationEntry(t, entries, locationExpectation{
		target: target.TargetCodex, scope: target.ScopeProject, resource: entity.KindInstructions,
		role: LocationRoleWrite, path: "AGENTS.md",
		selected: true, requested: true, defaultChoice: true,
		selectionSource: LocationSelectionManifestDefault,
	})
	assertLocationEntry(t, entries, locationExpectation{
		target: target.TargetOpenCode, scope: target.ScopeProject, resource: entity.KindSkill,
		role: LocationRoleWrite, path: ".agents/skills",
		selected: true, requested: true, defaultChoice: false,
		selectionSource: LocationSelectionManifestExplicit,
	})
	assertLocationEntry(t, entries, locationExpectation{
		target: target.TargetOpenCode, scope: target.ScopeProject, resource: entity.KindSkill,
		role: LocationRoleWrite, path: ".opencode/skills",
		selected: false, requested: true, defaultChoice: true,
		selectionSource: LocationSelectionProfileDefault,
	})
	assertLocationEntry(t, entries, locationExpectation{
		target: target.TargetCodex, scope: target.ScopeProject, resource: entity.KindHook,
		role: LocationRoleConfig, path: ".codex/hooks.json",
		selected: true, requested: true,
	})
	assertLocationEntry(t, entries, locationExpectation{
		target: target.TargetCodex, scope: target.ScopeProject, resource: entity.KindHookAsset,
		role: LocationRoleInternal, path: ".daem/hook-assets",
		selected: true, requested: true,
	})
	assertLocationEntry(t, entries, locationExpectation{
		target: target.TargetOpenCode, scope: target.ScopeProject, resource: entity.KindHook,
		role: LocationRoleUnsupported, reason: string(profile.UnsupportedReasonBridgeRequired),
		selected: false, requested: true,
	})
	assertLocationEntry(t, entries, locationExpectation{
		target: target.TargetPi, scope: target.ScopeProject, resource: entity.KindMCPServer,
		role: LocationRoleUnsupported, reason: "not-implemented",
		selected: false, requested: true,
	})
	for _, operation := range []profile.Operation{
		profile.OperationInstall,
		profile.OperationRefresh,
		profile.OperationRemove,
	} {
		entry := assertLocationEntry(t, entries, locationExpectation{
			target: target.TargetClaudeCode, scope: target.ScopeProject, resource: entity.KindExtension,
			variant: "claude-code-plugin", role: LocationRoleDelegated, operation: operation,
			selected: true, requested: true,
		})
		if entry.Route() == "" || entry.Path() != "" {
			t.Fatalf("extension %s row = %#v, want route without path", operation, entry)
		}
	}
	assertLocationEntry(t, entries, locationExpectation{
		target: target.TargetCodex, scope: target.ScopeGlobal, resource: entity.KindSkill,
		role: LocationRoleRuntime, path: "/etc/codex/skills",
	})

	assertLocationInventoryOrder(t, entries)
	copyEntries := result.Inventory.Entries()
	copyEntries[0] = LocationEntry{}
	if result.Inventory.Entries()[0].Kind() == "" {
		t.Fatal("LocationInventory.Entries returned an aliased slice")
	}
}

func TestRunPathsReportsUnadmittedSkillRootWithoutGrantingAuthority(t *testing.T) {
	manifestPath := writePathInventoryManifest(t, `
version = 1
targets = ["opencode"]

[[skill]]
name = "review"
source = { path = "skills/review", mode = "vendor" }
targets = ["opencode"]
scope = "project"

[skill.target.opencode]
install_to = ".future/skills"

[[skill]]
name = "review-two"
source = { path = "skills/review-two", mode = "vendor" }
targets = ["opencode"]
scope = "project"

[skill.target.opencode]
install_to = ".future/skills"
`)

	result, err := RunPaths(context.Background(), Input{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("RunPaths returned error: %v", err)
	}
	entries := result.Inventory.Entries()
	entry := assertLocationEntry(t, entries, locationExpectation{
		target: target.TargetOpenCode, scope: target.ScopeProject, resource: entity.KindSkill,
		role: LocationRoleUnsupported, reason: "requested-placement-not-admitted",
		selected: false, requested: true,
	})
	if entry.Path() != "" || !strings.Contains(entry.Detail(), ".future/skills") {
		t.Fatalf("unadmitted entry = %#v, want non-actionable requested-root detail", entry)
	}
	unadmittedCount := 0
	for _, candidate := range entries {
		if candidate.Reason() == "requested-placement-not-admitted" {
			unadmittedCount++
		}
		if candidate.Target() == target.TargetOpenCode &&
			candidate.Scope() == target.ScopeProject &&
			candidate.ResourceKind() == entity.KindSkill &&
			candidate.Role() == LocationRoleWrite &&
			candidate.Selected() {
			t.Fatalf("unadmitted root selected an admitted write row: %#v", candidate)
		}
	}
	if unadmittedCount != 1 {
		t.Fatalf("unadmitted diagnostic count = %d, want one deduplicated row", unadmittedCount)
	}
}

func TestRunPathsUsesInstructionRenderToAndSelectorSkillGroupPlacement(t *testing.T) {
	manifestPath := writePathInventoryManifest(t, `
version = 1
targets = ["antigravity-cli", "opencode", "pi"]

[instructions.project]
source = "AGENTS.md"
targets = ["antigravity-cli"]

[instructions.project.target.antigravity-cli]
render_to = "GEMINI.md"
mode = "copy"

[[skill_group]]
include = ["glob:*"]
source = { path = "skills", mode = "vendor" }
targets = ["opencode", "pi"]
scope = "project"

[skill_group.target.opencode]
install_to = ".agents/skills"
`)

	result, err := RunPaths(context.Background(), Input{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("RunPaths returned error: %v", err)
	}
	entries := result.Inventory.Entries()
	assertLocationEntry(t, entries, locationExpectation{
		target: target.TargetAntigravityCLI, scope: target.ScopeProject, resource: entity.KindInstructions,
		role: LocationRoleWrite, path: "GEMINI.md",
		selected: true, requested: true, defaultChoice: false,
		selectionSource: LocationSelectionManifestExplicit,
	})
	assertLocationEntry(t, entries, locationExpectation{
		target: target.TargetAntigravityCLI, scope: target.ScopeProject, resource: entity.KindInstructions,
		role: LocationRoleWrite, path: "AGENTS.md",
		selected: false, requested: true, defaultChoice: true,
		selectionSource: LocationSelectionProfileDefault,
	})
	assertLocationEntry(t, entries, locationExpectation{
		target: target.TargetOpenCode, scope: target.ScopeProject, resource: entity.KindSkill,
		role: LocationRoleWrite, path: ".agents/skills",
		selected: true, requested: true, defaultChoice: false,
		selectionSource: LocationSelectionManifestExplicit,
	})
	assertLocationEntry(t, entries, locationExpectation{
		target: target.TargetPi, scope: target.ScopeProject, resource: entity.KindSkill,
		role: LocationRoleWrite, path: ".pi/skills",
		selected: true, requested: true, defaultChoice: true,
		selectionSource: LocationSelectionManifestDefault,
	})
}

func TestRunPathsReportsUnadmittedInstructionRenderToWithoutSelectingDefault(t *testing.T) {
	manifestPath := writePathInventoryManifest(t, `
version = 1
targets = ["codex"]

[instructions.project]
source = "AGENTS.md"
targets = ["codex"]

[instructions.project.target.codex]
render_to = "CLAUDE.md"
mode = "copy"
`)

	result, err := RunPaths(context.Background(), Input{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("RunPaths returned error: %v", err)
	}
	entries := result.Inventory.Entries()
	entry := assertLocationEntry(t, entries, locationExpectation{
		target: target.TargetCodex, scope: target.ScopeProject, resource: entity.KindInstructions,
		role: LocationRoleUnsupported, reason: "requested-placement-not-admitted",
		selected: false, requested: true,
	})
	if entry.Path() != "" || !strings.Contains(entry.Detail(), "CLAUDE.md") {
		t.Fatalf("unadmitted entry = %#v, want non-actionable requested-placement detail", entry)
	}
	for _, candidate := range entries {
		if candidate.Target() == target.TargetCodex &&
			candidate.Scope() == target.ScopeProject &&
			candidate.ResourceKind() == entity.KindInstructions &&
			candidate.Role() == LocationRoleWrite &&
			candidate.Selected() {
			t.Fatalf("unadmitted render_to selected an admitted write row: %#v", candidate)
		}
	}
}

func TestRunPathsPreservesMultipleSelectedSkillRoots(t *testing.T) {
	manifestPath := writePathInventoryManifest(t, `
version = 1
targets = ["opencode"]

[[skill]]
name = "native"
source = { path = "skills/native", mode = "vendor" }
targets = ["opencode"]
scope = "project"

[[skill]]
name = "shared"
source = { path = "skills/shared", mode = "vendor" }
targets = ["opencode"]
scope = "project"

[skill.target.opencode]
install_to = ".agents/skills"
`)

	result, err := RunPaths(context.Background(), Input{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("RunPaths returned error: %v", err)
	}
	for path, selection := range map[string]struct {
		source        LocationSelectionSource
		defaultChoice bool
	}{
		".opencode/skills": {source: LocationSelectionManifestDefault, defaultChoice: true},
		".agents/skills":   {source: LocationSelectionManifestExplicit},
	} {
		assertLocationEntry(t, result.Inventory.Entries(), locationExpectation{
			target: target.TargetOpenCode, scope: target.ScopeProject, resource: entity.KindSkill,
			role: LocationRoleWrite, path: path,
			selected: true, requested: true, defaultChoice: selection.defaultChoice,
			selectionSource: selection.source,
		})
	}
}

func TestRunPathsListsStaticCatalogBeforeResourcesAreDeclared(t *testing.T) {
	manifestPath := writePathInventoryManifest(t, `
version = 1
targets = ["codex", "claude-code", "opencode", "pi", "antigravity-cli"]
`)

	result, err := RunPaths(context.Background(), Input{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("RunPaths returned error: %v", err)
	}
	entries := result.Inventory.Entries()
	if len(entries) == 0 {
		t.Fatal("RunPaths returned no static catalog rows")
	}
	for _, selectedTarget := range target.SupportedTargets() {
		for _, scope := range locationInventoryScopes {
			for _, resourceKind := range []entity.Kind{
				entity.KindInstructions,
				entity.KindSkill,
				entity.KindHook,
				entity.KindHookAsset,
				entity.KindMCPServer,
				entity.KindExtension,
			} {
				found := false
				for _, entry := range entries {
					if entry.Target() == selectedTarget &&
						entry.Scope() == scope &&
						entry.ResourceKind() == resourceKind {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf(
						"static catalog missing target=%q scope=%q resource=%q",
						selectedTarget,
						scope,
						resourceKind,
					)
				}
			}
		}
	}
	for _, entry := range entries {
		if entry.Selected() || entry.Requested() {
			t.Fatalf("undeclared resource produced manifest markers: %#v", entry)
		}
	}
}

func TestRunPathsDistinguishesExplicitDefaultSkillPlacement(t *testing.T) {
	manifestPath := writePathInventoryManifest(t, `
version = 1
targets = ["codex"]

[[skill]]
name = "review"
source = { path = "skills/review", mode = "vendor" }
targets = ["codex"]
scope = "project"

[skill.target.codex]
install_to = ".agents/skills"
`)

	result, err := RunPaths(context.Background(), Input{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("RunPaths returned error: %v", err)
	}
	assertLocationEntry(t, result.Inventory.Entries(), locationExpectation{
		target: target.TargetCodex, scope: target.ScopeProject, resource: entity.KindSkill,
		role: LocationRoleWrite, path: ".agents/skills",
		selected: true, requested: true, defaultChoice: true,
		selectionSource: LocationSelectionManifestExplicit,
	})
}

func TestRunPathsDistinguishesExplicitDefaultInstructionPlacement(t *testing.T) {
	manifestPath := writePathInventoryManifest(t, `
version = 1
targets = ["codex"]

[instructions.project]
source = "AGENTS.md"
targets = ["codex"]

[instructions.project.target.codex]
render_to = "AGENTS.md"
mode = "copy"
`)

	result, err := RunPaths(context.Background(), Input{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("RunPaths returned error: %v", err)
	}
	assertLocationEntry(t, result.Inventory.Entries(), locationExpectation{
		target: target.TargetCodex, scope: target.ScopeProject, resource: entity.KindInstructions,
		role: LocationRoleWrite, path: "AGENTS.md",
		selected: true, requested: true, defaultChoice: true,
		selectionSource: LocationSelectionManifestExplicit,
	})
}
