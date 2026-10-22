package cli

import (
	"errors"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/workflow/authoring"
	workflowhelp "github.com/isty2e/daem/internal/workflow/help"
)

func authoringExecutionOptions(manifestPath string, dryRun bool) authoring.ExecutionOptions {
	mode := authoring.AuthoringModeWrite
	if dryRun {
		mode = authoring.AuthoringModeDryRun
	}
	return authoring.ExecutionOptions{
		ManifestPath: manifestPath,
		Mode:         mode,
	}
}

func authoringFailureDiagnostic(command, manifestPath string, err error) clipresent.FailureDiagnostic {
	diagnostic, projected := clipresent.AuthoringSelectionDiagnostic(err)
	if !projected {
		diagnostic = humanFailureDiagnostic(err)
	}
	diagnostic = diagnostic.WithPrefix(command + " failed: ")
	if authoringManifestInitHintApplies(err) {
		if init, present := missingManifestInitGuidance(manifestPath, err); present {
			diagnostic = diagnostic.WithGuidance(init)
		}
	}
	diagnostic = diagnostic.WithGuidance(missingResourceSelectionGuidance(manifestPath, err)...)
	if recovery, present := clipresent.DeclarationRecoveryGuidance(err); present {
		diagnostic = diagnostic.WithGuidance(recovery)
	}
	if source, present := clipresent.MissingSourceGuidance(err); present {
		diagnostic = diagnostic.WithGuidance(source)
	}
	if command == "remove" {
		if missing, present := clipresent.MCPServerNotFoundGuidance(err); present {
			diagnostic = diagnostic.WithGuidance(missing)
		}
		var split authoring.SkillGroupPartialTargetRemovalError
		if errors.As(err, &split) {
			diagnostic = diagnostic.WithGuidance(skillGroupPartialTargetRemovalGuidance(split.ResourceID, split.RemainingTargets)...)
		}
	}
	return diagnostic
}

func missingResourceSelectionGuidance(manifestPath string, err error) []clipresent.Guidance {
	var missing *authoring.ResourceSelectionNotFoundError
	if !errors.As(err, &missing) {
		return nil
	}
	kind := clipresent.ResourceKindName(missing.Kind)
	hintOptions := manifestHintOptions(manifestPath, false)
	hintManifestPath, resolveErr := workflowhelp.ManifestHintPath(manifestPath)
	if resolveErr != nil {
		hintManifestPath = manifestPath
	}

	var guidance []clipresent.Guidance
	for _, selection := range missing.AvailableSelections() {
		var args []string
		for _, target := range selection.Targets {
			args = append(args, "--target", target)
		}
		if selection.Scope != "" {
			args = append(args, "--scope", selection.Scope)
		}
		guidance = append(guidance, manifestCommandGuidance([]string{"remove", kind, selection.Name}, hintManifestPath, hintOptions, args...).WithHumanPrefix("hint: available selection: "))
	}
	return guidance
}

func authoringArgumentDiagnostic(path []string, err error, args []string) clipresent.FailureDiagnostic {
	diagnostic := humanFailureDiagnostic(err).WithPrefix(path[0] + " failed: ")
	if guidance, present := unexpectedArgumentTargetGuidance(err, args); present {
		return diagnostic.WithGuidance(guidance)
	}
	return diagnostic.WithGuidance(commandHelpGuidance(path))
}

func authoringManifestInitHintApplies(err error) bool {
	var operationErr authoring.OperationError
	if !errors.As(err, &operationErr) {
		return true
	}
	return operationErr.Phase == authoring.OperationPhaseLoadManifest ||
		operationErr.Phase == authoring.OperationPhaseBuildManifestChange
}
