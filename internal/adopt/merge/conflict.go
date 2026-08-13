package merge

import (
	"fmt"
	"path/filepath"
	"strings"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	"github.com/isty2e/daem/internal/desired/entity"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredhook "github.com/isty2e/daem/internal/desired/hook"
	desiredinstructions "github.com/isty2e/daem/internal/desired/instructions"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/realization/profile"
	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	targetpkg "github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func classifyImportInstructionMerge(existing existingDeclarations, source adoptmodel.Source) (adoptmodel.MergeResult, []targetpkg.Target) {
	resource := "instructions/" + source.ResourceName
	for _, instruction := range existing.Instructions {
		if instruction.ID().Name() != source.ResourceName {
			continue
		}
		if !sameImportedInstruction(instruction, source, existing.ManifestRoot) {
			return adoptmodel.MergeResult{
				Resource: resource,
				Status:   adoptmodel.MergeStatusConflict,
				Detail:   "existing instruction has the same name with a different source, scope, or rendering",
			}, nil
		}
		return classifyImportTargets(resource, instruction.Targets(), []targetpkg.Target{source.Target})
	}
	return adoptmodel.MergeResult{Resource: resource, Status: adoptmodel.MergeStatusAdd, Detail: "append imported instruction"}, nil
}

func sameImportedInstruction(
	existing desiredinstructions.Instructions,
	imported adoptmodel.Source,
	manifestRoot string,
) bool {
	if existing.Scope() != imported.Scope ||
		!sameImportedLocalVendorSource(existing.Source(), imported.SourcePath, manifestRoot) {
		return false
	}

	renderTo := ""
	mode := desiredinstructions.RenderModeCopy
	if rendering, explicit := existing.Renderings()[imported.Target]; explicit {
		renderTo = rendering.RenderTo()
		mode = rendering.Mode()
	}
	return renderTo == imported.RenderTo && mode == desiredinstructions.RenderModeCopy
}

func classifyImportSkillMerge(existing existingDeclarations, skill adoptmodel.Skill) (adoptmodel.MergeResult, []targetpkg.Target) {
	resource := "skill/" + skill.ResourceName
	importedTargets := skill.Targets
	if len(importedTargets) == 0 {
		importedTargets = []targetpkg.Target{skill.Target}
	}
	for _, declaration := range existing.Skills {
		existingSkill := declaration.Skill
		if existingSkill.ID().Name() != skill.ResourceName {
			continue
		}
		if !sameImportedSkillBase(existingSkill, skill, existing.ManifestRoot) {
			return adoptmodel.MergeResult{
				Resource: resource,
				Status:   adoptmodel.MergeStatusConflict,
				Detail:   "existing skill has the same id with different canonical declaration semantics",
			}, nil
		}
		if !sameImportedSkillPlacements(existingSkill, skill, importedTargets) {
			return adoptmodel.MergeResult{
				Resource: resource,
				Status:   adoptmodel.MergeStatusConflict,
				Detail:   "existing skill target placement differs from the imported skill location",
			}, nil
		}
		result, missing := classifyImportTargets(resource, existingSkill.Targets(), importedTargets)
		if len(missing) != 0 && !declaration.CanMergeTargets {
			return adoptmodel.MergeResult{
				Resource: resource,
				Status:   adoptmodel.MergeStatusConflict,
				Detail:   "existing skill_group member cannot receive a member-specific target merge",
			}, nil
		}
		return result, missing
	}
	if conflict := conflictingSkillDestination(existing.Skills, skill); conflict != "" {
		return adoptmodel.MergeResult{
			Resource: resource,
			Status:   adoptmodel.MergeStatusConflict,
			Detail:   conflict,
		}, nil
	}
	return adoptmodel.MergeResult{Resource: resource, Status: adoptmodel.MergeStatusAdd, Detail: "append imported skill"}, nil
}

func sameImportedSkillBase(existing desiredskill.Skill, imported adoptmodel.Skill, manifestRoot string) bool {
	return existing.InstallName() == imported.InstallName &&
		existing.Scope() == imported.Scope &&
		existing.InstallMode() == desiredskill.InstallModeCopy &&
		existing.Portable() &&
		!existing.CompatRepair() &&
		sameImportedLocalVendorSource(existing.Source(), imported.SourcePath, manifestRoot)
}

func sameImportedSkillPlacements(
	existing desiredskill.Skill,
	imported adoptmodel.Skill,
	importedTargets []targetpkg.Target,
) bool {
	existingPlacements := existing.TargetPlacements()
	for _, selectedTarget := range importedTargets {
		if !containsTarget(existing.Targets(), selectedTarget) {
			continue
		}
		defaultPlacement, err := profile.Profile(selectedTarget).DefaultPlacement(entity.KindSkill, imported.Scope)
		if err != nil {
			return false
		}
		expected := defaultPlacement.Root().String()
		if installTo := imported.Placements[selectedTarget]; installTo != "" {
			expected = installTo
		}
		actual := defaultPlacement.Root().String()
		if placement, explicit := existingPlacements[selectedTarget]; explicit {
			actual = placement.InstallTo()
		}
		if actual != expected {
			return false
		}
	}
	return true
}

func classifyImportHookMerge(existing existingDeclarations, hook adoptmodel.Hook) (adoptmodel.MergeResult, []targetpkg.Target, error) {
	resource := "hook/" + hook.ResourceName
	for _, existingHook := range existing.Hooks {
		if existingHook.ID().Name() != hook.ResourceName {
			continue
		}
		if !sameImportedHookBase(existingHook, hook) {
			return adoptmodel.MergeResult{
				Resource: resource,
				Status:   adoptmodel.MergeStatusConflict,
				Detail:   "existing hook has the same name with a different command hook shape",
			}, nil, nil
		}
		if containsTarget(existingHook.Targets(), hook.Target) {
			same, err := sameImportedHookEffectiveMatch(existingHook, hook)
			if err != nil {
				return adoptmodel.MergeResult{}, nil, err
			}
			if !same {
				return adoptmodel.MergeResult{
					Resource: resource,
					Status:   adoptmodel.MergeStatusConflict,
					Detail:   "existing hook target matcher or condition differs from imported effective semantics",
				}, nil, nil
			}
		} else if strings.TrimSpace(existingHook.Matcher()) != strings.TrimSpace(hook.Matcher) {
			return adoptmodel.MergeResult{
				Resource: resource,
				Status:   adoptmodel.MergeStatusConflict,
				Detail:   "existing hook base matcher cannot represent the imported target matcher",
			}, nil, nil
		}
		result, missing := classifyImportTargets(resource, existingHook.Targets(), []targetpkg.Target{hook.Target})
		return result, missing, nil
	}
	return adoptmodel.MergeResult{Resource: resource, Status: adoptmodel.MergeStatusAdd, Detail: "append imported hook"}, nil, nil
}

func classifyImportMCPServerMerge(existing existingDeclarations, server adoptmodel.MCPServer) (adoptmodel.MergeResult, error) {
	resource := "mcp_server/" + server.ResourceName
	importedSubject, err := topologymcp.ProjectionSubject(server.Target, server.Scope, server.ResourceName)
	if err != nil {
		return adoptmodel.MergeResult{}, fmt.Errorf("imported %s projection identity: %w", resource, err)
	}
	imported := manifestMCPServerFromImportMCPServer(server)
	var matching declarationcodec.MCPServer
	matchingFound := false
	for _, block := range existing.MCPServers {
		if block.Server.Name != server.ResourceName {
			continue
		}
		existingSubject, err := existingMCPServerProjectionSubject(existing.Header, block.Server)
		if err != nil {
			return adoptmodel.MergeResult{}, fmt.Errorf("existing %s projection identity: %w", resource, err)
		}
		if existingSubject != importedSubject {
			continue
		}
		if matchingFound {
			return adoptmodel.MergeResult{}, fmt.Errorf(
				"existing manifest has duplicate mcp_server subject %q",
				importedSubject.String(),
			)
		}
		matching = block.Server
		matchingFound = true
	}
	if !matchingFound {
		return adoptmodel.MergeResult{
			Resource: resource,
			Subject:  importedSubject,
			Status:   adoptmodel.MergeStatusAdd,
			Detail: fmt.Sprintf(
				"append imported mcp_server projection target=%s scope=%s",
				server.Target,
				server.Scope,
			),
		}, nil
	}
	if declarationcodec.SameMCPServerProjectionPayload(matching, imported) {
		return adoptmodel.MergeResult{
			Resource: resource,
			Subject:  importedSubject,
			Status:   adoptmodel.MergeStatusNoop,
			Detail: fmt.Sprintf(
				"existing mcp_server projection target=%s scope=%s already matches imported standalone payload",
				server.Target,
				server.Scope,
			),
		}, nil
	}
	return adoptmodel.MergeResult{
		Resource: resource,
		Subject:  importedSubject,
		Status:   adoptmodel.MergeStatusConflict,
		Detail: fmt.Sprintf(
			"existing mcp_server projection target=%s scope=%s has a different standalone payload",
			server.Target,
			server.Scope,
		),
	}, nil
}

func existingMCPServerProjectionSubject(
	header declaration.ManifestHeader,
	server declarationcodec.MCPServer,
) (topology.SubjectID, error) {
	effectiveTargets := header.EffectiveTargets(server.Targets)
	if len(effectiveTargets) != 1 {
		return topology.SubjectID{}, fmt.Errorf("MCP server projection requires exactly one effective target")
	}
	selectedTarget, err := targetpkg.ParseTarget(effectiveTargets[0])
	if err != nil {
		return topology.SubjectID{}, err
	}
	selectedScope, err := targetpkg.ParseScope(header.EffectiveScope(server.Scope))
	if err != nil {
		return topology.SubjectID{}, err
	}
	return topologymcp.ProjectionSubject(selectedTarget, selectedScope, server.Name)
}

func classifyImportExtensionMerge(
	existing existingDeclarations,
	imported desiredextension.Extension,
) adoptmodel.MergeResult {
	resource := "extension/" + imported.ID().Name()
	for _, block := range existing.Extensions {
		if block.Extension.ID != imported.ID().Name() {
			continue
		}
		if sameImportedExtension(block.Extension, imported) {
			return adoptmodel.MergeResult{
				Resource: resource,
				Status:   adoptmodel.MergeStatusNoop,
				Detail:   "existing extension already matches imported exact relation",
			}
		}
		return adoptmodel.MergeResult{
			Resource: resource,
			Status:   adoptmodel.MergeStatusConflict,
			Detail:   "existing extension has the same id with a different exact relation",
		}
	}
	for _, block := range existing.Extensions {
		if sameImportedExtension(block.Extension, imported) {
			return adoptmodel.MergeResult{
				Resource: resource,
				Status:   adoptmodel.MergeStatusConflict,
				Detail:   "existing extension relation is already declared under a different id",
			}
		}
	}
	return adoptmodel.MergeResult{
		Resource: resource,
		Status:   adoptmodel.MergeStatusAdd,
		Detail:   "insert imported extension in exact host order",
	}
}

func sameImportedExtension(
	existing declaration.Extension,
	imported desiredextension.Extension,
) bool {
	if existing.Carrier != string(imported.Carrier()) ||
		existing.Scope != string(imported.Scope()) ||
		len(existing.Targets) != 1 ||
		existing.Targets[0] != string(imported.Target()) {
		return false
	}
	switch imported.Source().Kind() {
	case desiredextension.SourceKindMarketplace:
		return existing.Source.Marketplace == imported.Source().Ref() &&
			existing.Source.HostSource == ""
	case desiredextension.SourceKindHostSource:
		return existing.Source.HostSource == imported.Source().Ref() &&
			existing.Source.Marketplace == ""
	default:
		return false
	}
}

func classifyImportTargets(resource string, existingTargets []targetpkg.Target, importedTargets []targetpkg.Target) (adoptmodel.MergeResult, []targetpkg.Target) {
	missing := missingCanonicalImportTargets(existingTargets, importedTargets)
	if len(missing) == 0 {
		return adoptmodel.MergeResult{
			Resource: resource,
			Status:   adoptmodel.MergeStatusNoop,
			Detail:   "existing resource already covers imported target(s)",
		}, nil
	}
	return adoptmodel.MergeResult{
		Resource: resource,
		Status:   adoptmodel.MergeStatusMergeTargets,
		Detail:   "add targets " + importDomainTargetsText(missing) + " to existing resource",
	}, missing
}

func conflictingSkillDestination(existing []existingSkillDeclaration, imported adoptmodel.Skill) string {
	importedTargets := imported.Targets
	if len(importedTargets) == 0 {
		importedTargets = []targetpkg.Target{imported.Target}
	}
	for _, declaration := range existing {
		existingSkill := declaration.Skill
		if existingSkill.Scope() != imported.Scope || existingSkill.InstallName() != imported.InstallName {
			continue
		}
		for _, target := range importedTargets {
			if containsTarget(existingSkill.Targets(), target) {
				return fmt.Sprintf(
					"skill destination target=%s scope=%s name=%q is already used by skill id %q",
					target,
					imported.Scope,
					imported.InstallName,
					existingSkill.ID().Name(),
				)
			}
		}
	}
	return ""
}

func sameImportedHookBase(existing desiredhook.Hook, imported adoptmodel.Hook) bool {
	return strings.TrimSpace(existing.Event()) == strings.TrimSpace(imported.Event) &&
		existing.Type() == desiredhook.TypeCommand &&
		strings.TrimSpace(existing.Command()) == strings.TrimSpace(imported.Command) &&
		existing.TimeoutSeconds() == imported.Timeout &&
		strings.TrimSpace(existing.StatusMessage()) == strings.TrimSpace(imported.StatusMessage) &&
		existing.Scope() == imported.Scope
}

func sameImportedHookEffectiveMatch(existing desiredhook.Hook, imported adoptmodel.Hook) (bool, error) {
	effective, err := existing.EffectiveMatch(imported.Target)
	if err != nil {
		return false, err
	}
	return effective.Matcher() == strings.TrimSpace(imported.Matcher) &&
		effective.Condition() == strings.TrimSpace(imported.Condition), nil
}

func sameImportedLocalVendorSource(existing sourcepkg.Source, importedPath string, manifestRoot string) bool {
	local, ok := existing.Local()
	if !ok || local.Mode() != sourcepkg.LocalSourceModeVendor {
		return false
	}
	existingPath, ok := manifestLocalSourcePath(manifestRoot, local.Path())
	if !ok {
		return false
	}
	candidatePath, ok := manifestLocalSourcePath(manifestRoot, importedPath)
	return ok && existingPath == candidatePath
}

func manifestLocalSourcePath(manifestRoot string, value string) (string, bool) {
	if value == "" {
		return "", false
	}
	path := filepath.FromSlash(value)
	if !filepath.IsAbs(path) {
		path = filepath.Join(manifestRoot, path)
	}
	return filepath.Clean(path), true
}
