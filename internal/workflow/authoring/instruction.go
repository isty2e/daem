package authoring

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
)

func BuildAddInstructionChange(document ManifestDocument, request AddInstructionRequest) (Change, error) {
	if err := document.validateOriginal(); err != nil {
		return Change{}, err
	}

	header, err := declaration.DecodeManifestHeader(document.Content)
	if err != nil {
		return Change{}, err
	}
	name, err := CleanInstructionName(request.Name)
	if err != nil {
		return Change{}, err
	}
	instruction, err := InstructionFromAddRequest(request, document.Root, header)
	if err != nil {
		return Change{}, err
	}
	content, changeKind, err := ApplyAddInstructionToManifest(document.Content, name, instruction, header)
	if err != nil {
		return Change{}, err
	}
	if err := document.validateResult(content); err != nil {
		return Change{}, err
	}

	return Change{
		ManifestPath:  document.Path,
		Original:      document.Content,
		Content:       content,
		ResourceID:    name,
		ChangeKind:    changeKind,
		ManifestBlock: strings.TrimRight(declarationcodec.RenderInstructionBlock(name, instruction), "\n"),
		Warnings:      instructionWarnings(instruction, document.Root),
	}, nil
}

func InstructionFromAddRequest(request AddInstructionRequest, manifestRoot string, header declaration.ManifestHeader) (declarationcodec.Instruction, error) {
	name, err := CleanInstructionName(request.Name)
	if err != nil {
		return declarationcodec.Instruction{}, err
	}
	effectiveScope := declarationcodec.InstructionEffectiveScope(name, request.Scope, header)
	source, err := localInstructionSource(request.SourceArg, manifestRoot, effectiveScope)
	if err != nil {
		return declarationcodec.Instruction{}, err
	}
	return declarationcodec.Instruction{
		Source:  source,
		Targets: append([]string(nil), request.Targets...),
		Scope:   request.Scope,
	}, nil
}

func BuildRemoveInstructionChange(document ManifestDocument, request RemoveInstructionRequest) (Change, error) {
	if err := document.validateOriginal(); err != nil {
		return Change{}, err
	}
	name, err := CleanInstructionName(request.ResourceName)
	if err != nil {
		return Change{}, err
	}
	request.ResourceName = name
	content, changeKind, err := ApplyRemoveInstructionToManifest(document.Content, request)
	if err != nil {
		return Change{}, err
	}
	if err := document.validateResult(content); err != nil {
		return Change{}, err
	}
	return Change{
		ManifestPath: document.Path,
		Original:     document.Content,
		Content:      content,
		ResourceID:   name,
		ChangeKind:   changeKind,
	}, nil
}

func ApplyAddInstructionToManifest(original []byte, name string, instruction declarationcodec.Instruction, header declaration.ManifestHeader) ([]byte, string, error) {
	change, err := declarationcodec.ApplyInstructionAdd(original, header, name, instruction)
	if err != nil {
		return nil, "", err
	}
	changeKind, err := addDeclarationChangeKind(change.Outcome, "append instruction resource", "update instruction targets")
	if err != nil {
		return nil, "", err
	}
	return change.Content, changeKind, nil
}

func ApplyRemoveInstructionToManifest(original []byte, request RemoveInstructionRequest) ([]byte, string, error) {
	header, err := declaration.DecodeManifestHeader(original)
	if err != nil {
		return nil, "", err
	}
	candidates, err := removeInstructionCandidates(original, header)
	if err != nil {
		return nil, "", err
	}
	matches := filterRemoveInstructionCandidates(candidates, request)
	if len(matches) == 0 {
		return nil, "", fmt.Errorf("instruction resource %q not found", request.ResourceName)
	}
	if len(matches) > 1 {
		return nil, "", fmt.Errorf("instruction resource key %q is ambiguous; narrow with --target/--scope", request.ResourceName)
	}
	return applyRemoveInstructionCandidate(original, matches[0], request.Targets)
}

type removeInstructionCandidate struct {
	resourceName string
	scope        string
	targets      []string
	start        int
	end          int
}

func removeInstructionCandidates(content []byte, header declaration.ManifestHeader) ([]removeInstructionCandidate, error) {
	blocks, err := declarationcodec.ScanInstructionBlocks(content)
	if err != nil {
		return nil, err
	}
	candidates := make([]removeInstructionCandidate, 0, len(blocks))
	for _, block := range blocks {
		candidates = append(candidates, removeInstructionCandidate{
			resourceName: block.Name,
			scope:        declarationcodec.InstructionEffectiveScope(block.Name, block.Instruction.Scope, header),
			targets:      header.EffectiveTargets(block.Instruction.Targets),
			start:        block.Start,
			end:          block.End,
		})
	}
	return candidates, nil
}

func filterRemoveInstructionCandidates(candidates []removeInstructionCandidate, request RemoveInstructionRequest) []removeInstructionCandidate {
	matches := make([]removeInstructionCandidate, 0)
	for _, candidate := range candidates {
		if candidate.resourceName != request.ResourceName {
			continue
		}
		if request.Scope != "" && candidate.scope != request.Scope {
			continue
		}
		if len(request.Targets) != 0 && !declaration.Targets(candidate.targets).Intersects(declaration.Targets(request.Targets)) {
			continue
		}
		matches = append(matches, candidate)
	}
	return matches
}

func applyRemoveInstructionCandidate(original []byte, candidate removeInstructionCandidate, selectedTargets []string) ([]byte, string, error) {
	change, err := declaration.ApplyTargetRemoval(declaration.TargetRemovalInput{
		Original:        original,
		Range:           declaration.DocumentRange{Start: candidate.start, End: candidate.end},
		ExistingTargets: declaration.Targets(candidate.targets),
		SelectedTargets: declaration.Targets(selectedTargets),
		NoSelectedTargetsError: func() error {
			return fmt.Errorf("instruction resource %q does not include selected targets", candidate.resourceName)
		},
		BeforeTargetReplace: func(originalBlock string) string {
			return declarationcodec.RemoveInstructionTargetTables(originalBlock, candidate.resourceName, selectedTargets)
		},
		RenderBlockWithTargets: func(originalBlock string, remainingTargets declaration.Targets) (string, error) {
			return declarationcodec.ReplaceInstructionTargets(originalBlock, candidate.resourceName, remainingTargets.Values())
		},
	})
	if err != nil {
		return nil, "", err
	}
	changeKind, err := targetRemovalChangeKind(change.Outcome, "remove instruction resource", "update instruction targets")
	if err != nil {
		return nil, "", err
	}
	return change.Content, changeKind, nil
}
