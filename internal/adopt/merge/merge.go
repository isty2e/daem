package merge

import (
	"fmt"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	targetpkg "github.com/isty2e/daem/internal/target"
)

const (
	declarationHookTypeCommand = "command"
	declarationInstallModeCopy = "copy"
)

type importMergeActionKind string

const (
	importMergeActionInstruction importMergeActionKind = "instruction"
	importMergeActionSkill       importMergeActionKind = "skill"
	importMergeActionHook        importMergeActionKind = "hook"
)

type importMergeTargetAction struct {
	Kind    importMergeActionKind
	Source  adoptmodel.Source
	Skill   adoptmodel.Skill
	Hook    adoptmodel.Hook
	Targets []targetpkg.Target
}

func IntoManifest(
	request adoptmodel.Request,
	originalContent []byte,
	candidates adoptmodel.CandidateSet,
) (adoptmodel.Plan, error) {
	if !request.Merge() {
		return adoptmodel.Plan{}, fmt.Errorf("manifest merge requires a merge adoption request")
	}
	if err := candidates.Validate(); err != nil {
		return adoptmodel.Plan{}, err
	}
	existing, err := scanExistingDeclarations(originalContent)
	if err != nil {
		return adoptmodel.Plan{}, err
	}

	sources := candidates.Sources()
	skills := candidates.Skills()
	hooks := candidates.Hooks()
	mcpServers := candidates.MCPServers()
	extensions := candidates.Extensions()
	addSources := make([]adoptmodel.Source, 0, len(sources))
	addSkills := make([]adoptmodel.Skill, 0, len(skills))
	addHooks := make([]adoptmodel.Hook, 0, len(hooks))
	addMCPServers := make([]adoptmodel.MCPServer, 0, len(mcpServers))
	addExtensions := make([]desiredextension.Extension, 0, len(extensions))
	targetActions := make([]importMergeTargetAction, 0)
	mergeResults := make(
		[]adoptmodel.MergeResult,
		0,
		len(sources)+len(skills)+len(hooks)+len(mcpServers)+len(extensions),
	)

	for _, source := range sources {
		action, missingTargets := classifyImportInstructionMerge(existing, source)
		mergeResults = append(mergeResults, action)
		switch action.Status {
		case adoptmodel.MergeStatusAdd:
			addSources = append(addSources, source)
		case adoptmodel.MergeStatusMergeTargets:
			targetActions = append(targetActions, importMergeTargetAction{
				Kind:    importMergeActionInstruction,
				Source:  source,
				Targets: missingTargets,
			})
		}
	}

	for _, skill := range skills {
		action, missingTargets := classifyImportSkillMerge(existing, skill)
		mergeResults = append(mergeResults, action)
		switch action.Status {
		case adoptmodel.MergeStatusAdd:
			addSkills = append(addSkills, skill)
		case adoptmodel.MergeStatusMergeTargets:
			targetActions = append(targetActions, importMergeTargetAction{
				Kind:    importMergeActionSkill,
				Skill:   skill,
				Targets: missingTargets,
			})
		}
	}

	for _, hook := range hooks {
		action, missingTargets := classifyImportHookMerge(existing, hook)
		mergeResults = append(mergeResults, action)
		switch action.Status {
		case adoptmodel.MergeStatusAdd:
			addHooks = append(addHooks, hook)
		case adoptmodel.MergeStatusMergeTargets:
			targetActions = append(targetActions, importMergeTargetAction{
				Kind:    importMergeActionHook,
				Hook:    hook,
				Targets: missingTargets,
			})
		}
	}
	for _, server := range mcpServers {
		action, err := classifyImportMCPServerMerge(existing, server)
		if err != nil {
			return adoptmodel.Plan{}, err
		}
		mergeResults = append(mergeResults, action)
		if action.Status == adoptmodel.MergeStatusAdd {
			addMCPServers = append(addMCPServers, server)
		}
	}
	for _, extension := range extensions {
		action := classifyImportExtensionMerge(existing, extension)
		mergeResults = append(mergeResults, action)
		if action.Status == adoptmodel.MergeStatusAdd {
			addExtensions = append(addExtensions, extension)
		}
	}

	filteredExtensionResult, err := candidates.ExtensionResult().WithExtensions(addExtensions)
	if err != nil {
		return adoptmodel.Plan{}, err
	}

	filteredCandidates, err := adoptmodel.NewCandidateSet(
		adoptmodel.CandidateSetInput{
			Sources:    addSources,
			Skills:     addSkills,
			Hooks:      addHooks,
			MCPServers: addMCPServers,
			Extensions: filteredExtensionResult,
			Scans:      candidates.Scans(),
			Skipped:    candidates.Skipped(),
		},
	)
	if err != nil {
		return adoptmodel.Plan{}, err
	}
	if hasMergeConflicts(mergeResults) {
		return adoptmodel.NewPlan(request, originalContent, originalContent, filteredCandidates, mergeResults)
	}

	content := append([]byte{}, originalContent...)
	for _, action := range targetActions {
		var changeKind string
		switch action.Kind {
		case importMergeActionInstruction:
			instruction := manifestInstructionFromImportSource(action.Source, action.Targets)
			content, changeKind, err = applyImportInstructionTargetMerge(content, action.Source.ResourceName, instruction)
		case importMergeActionSkill:
			skill := manifestSkillFromImportSkill(action.Skill, action.Targets)
			content, changeKind, err = applyImportSkillTargetMerge(content, skill)
		case importMergeActionHook:
			hook := manifestHookFromImportHook(action.Hook, action.Targets)
			content, changeKind, err = applyImportHookTargetMerge(content, hook)
		default:
			err = fmt.Errorf("unknown import merge action kind %q", action.Kind)
		}
		if err != nil {
			return adoptmodel.Plan{}, err
		}
		_ = changeKind
	}

	body, err := adoptmodel.RenderManifestBodyContent(
		addSources,
		addSkills,
		addHooks,
		addMCPServers,
		nil,
	)
	if err != nil {
		return adoptmodel.Plan{}, err
	}
	content = declarationcodec.AppendImportManifestBody(content, body)
	content, err = insertImportedExtensions(
		content,
		filteredExtensionResult.OrderedExtensions(),
		addExtensions,
	)
	if err != nil {
		return adoptmodel.Plan{}, err
	}
	if err := validateCanonicalManifest(content); err != nil {
		mergeResults = append(mergeResults, adoptmodel.MergeResult{
			Resource: "manifest",
			Status:   adoptmodel.MergeStatusConflict,
			Detail:   err.Error(),
		})
		return adoptmodel.NewPlan(request, originalContent, originalContent, filteredCandidates, mergeResults)
	}
	return adoptmodel.NewPlan(request, originalContent, content, filteredCandidates, mergeResults)
}

func hasMergeConflicts(results []adoptmodel.MergeResult) bool {
	for _, result := range results {
		if result.Status == adoptmodel.MergeStatusConflict {
			return true
		}
	}
	return false
}
