package authoring

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	"github.com/isty2e/daem/internal/desired/entity"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	"github.com/isty2e/daem/internal/realization/aggregate/hook"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

func BuildAddHookChange(document ManifestDocument, request AddHookRequest) (Change, error) {
	if err := document.validateOriginal(); err != nil {
		return Change{}, err
	}
	header, err := declaration.DecodeManifestHeader(document.Content)
	if err != nil {
		return Change{}, err
	}
	name, err := CleanHookName(request.Name)
	if err != nil {
		return Change{}, err
	}
	request.Name = name
	hook, warnings, err := HookFromAddRequest(request, header)
	if err != nil {
		return Change{}, err
	}

	content, changeKind, err := ApplyAddHookToManifest(document.Content, hook, header)
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
		ManifestBlock: strings.TrimRight(declarationcodec.RenderHookBlock(hook), "\n"),
		Warnings:      warnings,
	}, nil
}

func HookFromAddRequest(request AddHookRequest, header declaration.ManifestHeader) (declaration.Hook, []string, error) {
	hook := declaration.Hook{
		Name:            request.Name,
		Event:           request.Event,
		Matcher:         request.Matcher,
		Command:         request.Command,
		TimeoutSeconds:  request.TimeoutSeconds,
		StatusMessage:   request.StatusMessage,
		Targets:         append([]string(nil), request.Targets...),
		Scope:           request.Scope,
		TargetOverrides: append([]declaration.HookTargetOverride(nil), request.TargetOverrides...),
	}
	if strings.TrimSpace(hook.Event) == "" {
		return declaration.Hook{}, nil, fmt.Errorf("--event is required")
	}
	if strings.TrimSpace(hook.Command) == "" {
		return declaration.Hook{}, nil, fmt.Errorf("--command is required")
	}

	effectiveTargets := header.EffectiveTargets(hook.Targets)
	if len(effectiveTargets) == 0 {
		return declaration.Hook{}, nil, fmt.Errorf("hook %q has no targets; set manifest targets or pass --target", hook.Name)
	}
	if err := validateHookTargetOverrides(hook, effectiveTargets); err != nil {
		return declaration.Hook{}, nil, err
	}
	warnings, err := validateHookTargets(hook, effectiveTargets)
	if err != nil {
		return declaration.Hook{}, nil, err
	}

	return hook, warnings, nil
}

func BuildRemoveHookChange(document ManifestDocument, request RemoveHookRequest) (Change, error) {
	if err := document.validateOriginal(); err != nil {
		return Change{}, err
	}
	name, err := CleanHookName(request.ResourceName)
	if err != nil {
		return Change{}, err
	}
	request.ResourceName = name
	content, changeKind, err := ApplyRemoveHookToManifest(document.Content, request)
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

func ApplyAddHookToManifest(original []byte, hook declaration.Hook, header declaration.ManifestHeader) ([]byte, string, error) {
	change, err := declarationcodec.ApplyHookAdd(original, header, hook, mergeHookTargets)
	if err != nil {
		return nil, "", err
	}
	changeKind, err := addDeclarationChangeKind(change.Outcome, "append hook resource", "update hook targets")
	if err != nil {
		return nil, "", err
	}
	return change.Content, changeKind, nil
}

func ApplyRemoveHookToManifest(original []byte, request RemoveHookRequest) ([]byte, string, error) {
	header, err := declaration.DecodeManifestHeader(original)
	if err != nil {
		return nil, "", err
	}
	candidates, err := removeHookCandidates(original, header)
	if err != nil {
		return nil, "", err
	}
	matches := filterRemoveHookCandidates(candidates, request)
	if len(matches) == 0 {
		return nil, "", fmt.Errorf("hook resource %q not found", request.ResourceName)
	}
	if len(matches) > 1 {
		return nil, "", fmt.Errorf("hook resource key %q is ambiguous; narrow with --target/--scope", request.ResourceName)
	}
	return applyRemoveHookCandidate(original, header, matches[0], request.Targets)
}

type removeHookCandidate struct {
	resourceName string
	scope        string
	targets      []string
	start        int
	end          int
	hook         declaration.Hook
}

func removeHookCandidates(content []byte, header declaration.ManifestHeader) ([]removeHookCandidate, error) {
	blocks, err := declarationcodec.ScanHookBlocks(content)
	if err != nil {
		return nil, err
	}
	candidates := make([]removeHookCandidate, 0, len(blocks))
	for _, block := range blocks {
		candidates = append(candidates, removeHookCandidate{
			resourceName: block.Hook.Name,
			scope:        header.EffectiveScope(block.Hook.Scope),
			targets:      header.EffectiveTargets(block.Hook.Targets),
			start:        block.Start,
			end:          block.End,
			hook:         block.Hook,
		})
	}
	return candidates, nil
}

func filterRemoveHookCandidates(candidates []removeHookCandidate, request RemoveHookRequest) []removeHookCandidate {
	matches := make([]removeHookCandidate, 0)
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

func applyRemoveHookCandidate(original []byte, header declaration.ManifestHeader, candidate removeHookCandidate, selectedTargets []string) ([]byte, string, error) {
	change, err := declaration.ApplyTargetRemoval(declaration.TargetRemovalInput{
		Original:        original,
		Range:           declaration.DocumentRange{Start: candidate.start, End: candidate.end},
		ExistingTargets: declaration.Targets(candidate.targets),
		SelectedTargets: declaration.Targets(selectedTargets),
		NoSelectedTargetsError: func() error {
			return fmt.Errorf("hook resource %q does not include selected targets", candidate.resourceName)
		},
		RenderBlockWithTargets: func(originalBlock string, remainingTargets declaration.Targets) (string, error) {
			remainingHook := candidate.hook
			remainingHook.TargetOverrides = declarationcodec.FilterHookOverrides(candidate.hook.TargetOverrides, remainingTargets.Values())
			if remainingTargets.EqualMembership(declaration.Targets(header.EffectiveTargets(nil))) {
				remainingHook.Targets = nil
			} else {
				remainingHook.Targets = remainingTargets.Values()
			}
			return declarationcodec.UpdateHookTargets(originalBlock, candidate.hook, remainingHook)
		},
	})
	if err != nil {
		return nil, "", err
	}
	changeKind, err := targetRemovalChangeKind(change.Outcome, "remove hook resource", "update hook targets")
	if err != nil {
		return nil, "", err
	}
	return change.Content, changeKind, nil
}

func validateHookTargetOverrides(hook declaration.Hook, targets []string) error {
	targetSet := make(map[string]struct{}, len(targets))
	for _, selectedTarget := range targets {
		targetSet[selectedTarget] = struct{}{}
	}
	for _, override := range hook.TargetOverrides {
		if _, ok := targetSet[override.Target]; !ok {
			return fmt.Errorf("hook %q target_override target %q is not declared for hook", hook.Name, override.Target)
		}
		selectedTarget := target.Target(override.Target)
		support, ok := hostsurfacecatalog.Product().ResourceSupport(selectedTarget, entity.KindHook)
		if !ok || !support.Supported() {
			return fmt.Errorf(
				"hook %q target %q: target_override is not supported because %s",
				hook.Name,
				selectedTarget,
				hookSupportDetail(support, ok),
			)
		}
	}
	return nil
}

func validateHookTargets(hook declaration.Hook, targets []string) ([]string, error) {
	overrideByTarget := declarationcodec.HookOverridesByTarget(hook.TargetOverrides)
	warnings := make([]string, 0)
	for _, targetValue := range targets {
		selectedTarget := target.Target(targetValue)
		warning, err := admitCommandHookAuthoring(hook.Name, selectedTarget)
		if err != nil {
			return nil, err
		}
		if warning != "" {
			warnings = append(warnings, warning)
			continue
		}
		matcher := hook.Matcher
		condition := ""
		if override, ok := overrideByTarget[targetValue]; ok {
			if strings.TrimSpace(override.Matcher) != "" {
				matcher = strings.TrimSpace(override.Matcher)
			}
			condition = strings.TrimSpace(override.Condition)
		}
		if err := commandhook.ValidateShape(hook.Name, selectedTarget, hook.Event, matcher, condition); err != nil {
			return nil, err
		}
	}
	return warnings, nil
}

// CommandHookAdmissionError reports that one target has no direct command-hook
// authoring route. It carries semantic facts only; CLI wording belongs to the
// presentation boundary.
type CommandHookAdmissionError struct {
	hookName       string
	selectedTarget target.Target
	reason         profile.UnsupportedReason
	detail         string
}

func (failure CommandHookAdmissionError) Error() string {
	return fmt.Sprintf(
		"hook %q target %q: add hook cannot author command hooks because no direct command-hook route is admitted; %s",
		failure.hookName,
		failure.selectedTarget,
		failure.detail,
	)
}

// HookName returns the rejected authored hook identity.
func (failure CommandHookAdmissionError) HookName() string { return failure.hookName }

// Target returns the target whose direct route was not admitted.
func (failure CommandHookAdmissionError) Target() target.Target { return failure.selectedTarget }

// Reason returns the structural profile reason for the missing route.
func (failure CommandHookAdmissionError) Reason() profile.UnsupportedReason { return failure.reason }

// Detail returns the structured-support diagnostic selected by authoring.
func (failure CommandHookAdmissionError) Detail() string { return failure.detail }

func admitCommandHookAuthoring(name string, selectedTarget target.Target) (string, error) {
	support, ok := hostsurfacecatalog.Product().ResourceSupport(selectedTarget, entity.KindHook)
	if ok && support.Supported() {
		return "", nil
	}
	detail := hookSupportDetail(support, ok)
	if ok && support.Reason() == profile.UnsupportedReasonBridgeRequired {
		return fmt.Sprintf(
			"target %q: %s; hook remains lock-only until apply/status support exists",
			selectedTarget,
			detail,
		), nil
	}
	return "", CommandHookAdmissionError{
		hookName:       name,
		selectedTarget: selectedTarget,
		reason:         support.Reason(),
		detail:         detail,
	}
}

func hookSupportDetail(support profile.Support, known bool) string {
	if !known {
		return "command hook reconciliation is not implemented"
	}
	switch support.Reason() {
	case profile.UnsupportedReasonBridgeRequired:
		return "command hook reconciliation requires an extension bridge surface"
	case profile.UnsupportedReasonNotImplemented:
		return "command hook reconciliation is not implemented"
	case profile.UnsupportedReasonDirectCLIRouteNotAdmitted:
		return "command hook reconciliation is not implemented"
	default:
		return "command hook reconciliation is supported"
	}
}

func mergeHookTargets(existing declaration.Hook, addition declaration.Hook, mergedTargets []string, header declaration.ManifestHeader) (declaration.Hook, error) {
	result := existing
	result.Targets = mergedTargets
	overrideByTarget := declarationcodec.HookOverridesByTarget(existing.TargetOverrides)
	for _, override := range addition.TargetOverrides {
		if _, exists := overrideByTarget[override.Target]; exists {
			return declaration.Hook{}, fmt.Errorf("hook %q already has target_override for target %q", existing.Name, override.Target)
		}
		result.TargetOverrides = append(result.TargetOverrides, override)
	}
	effectiveTargets := header.EffectiveTargets(result.Targets)
	if err := validateHookTargetOverrides(result, effectiveTargets); err != nil {
		return declaration.Hook{}, err
	}
	if _, err := validateHookTargets(result, effectiveTargets); err != nil {
		return declaration.Hook{}, err
	}
	return result, nil
}
