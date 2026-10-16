package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/target"
	workflowhelp "github.com/isty2e/daem/internal/workflow/help"
)

func lockCommandHint(manifestPath, requestedManifestPath string) string {
	command, err := clipresent.ManifestCommand([]string{"lock"}, manifestPath, manifestHintOptions(requestedManifestPath, false))
	if err != nil {
		return ""
	}
	return command
}

func printLockCommandHint(output io.Writer, manifestPath, requestedManifestPath string) {
	clipresent.PrintManifestCommand(output, "next: run ", []string{"lock"}, manifestPath, manifestHintOptions(requestedManifestPath, false))
}

func unexpectedArgumentDiagnostic(arg string, targetValues targetFlagValues) clipresent.FailureDiagnostic {
	diagnostic := clipresent.NewFailureDiagnostic((&unexpectedArgumentError{argument: arg}).Error())
	if len(targetValues) == 0 || !isSupportedTargetLiteral(arg) {
		return diagnostic
	}
	return diagnostic.WithGuidance(targetListSyntaxGuidance())
}

func unexpectedArgumentTargetGuidance(err error, args []string) (clipresent.Guidance, bool) {
	var unexpected *unexpectedArgumentError
	if !errors.As(err, &unexpected) || !hasSpacedTargetListAfterTargetFlag(args) {
		return clipresent.Guidance{}, false
	}
	return targetListSyntaxGuidance(), true
}

func targetListSyntaxGuidance() clipresent.Guidance {
	return clipresent.InstructionGuidance("repeat the flag: --target codex --target claude-code")
}

func isSupportedTargetLiteral(value string) bool {
	_, err := target.ParseTarget(value)
	return err == nil
}

func isSupportedTargetListLiteral(value string) bool {
	for part := range strings.SplitSeq(value, ",") {
		if !isSupportedTargetLiteral(strings.TrimSpace(part)) {
			return false
		}
	}
	return strings.TrimSpace(value) != ""
}

func hasSpacedTargetListAfterTargetFlag(args []string) bool {
	for index, arg := range args {
		if arg == "--target" {
			if index+2 < len(args) && isSupportedTargetListLiteral(args[index+1]) && isSupportedTargetLiteral(args[index+2]) {
				return true
			}
			continue
		}
		if strings.HasPrefix(arg, "--target=") {
			if index+1 < len(args) && isSupportedTargetLiteral(args[index+1]) {
				return true
			}
		}
	}
	return false
}

func importNothingToImportGuidance(manifestPath string, merge bool) []clipresent.Guidance {
	guidance := []clipresent.Guidance{
		clipresent.InstructionGuidance("verify that the selected --target and --scope have live agent files to import"),
		clipresent.InstructionGuidance("try another selection, such as --scope global or a different --target"),
	}
	if merge {
		guidance = append(guidance, clipresent.InstructionGuidance("confirm --manifest points to the existing manifest you want to merge into"))
	} else {
		guidance = append(guidance, clipresent.InstructionGuidance("choose the destination with --manifest <path>, or add --merge when importing into an existing manifest"))
	}
	return append(
		guidance,
		clipresent.InstructionGuidance("if there is no live config to import, initialize the selected manifest and add resources explicitly"),
		manifestCommandGuidance([]string{"init"}, manifestPath, creationHintOptions(), "--dry-run"),
	)
}

func skillGroupPartialTargetRemovalGuidance(resourceKey string, remainingTargets []string) []clipresent.Guidance {
	targets := "<remaining-targets>"
	if len(remainingTargets) != 0 {
		targets = tomlStringArray(remainingTargets)
	}
	guidance := []clipresent.Guidance{
		clipresent.InstructionGuidance(fmt.Sprintf("edit the manifest so %q is in its own [[skill_group]] block with targets = %s", resourceKey, targets)),
		clipresent.InstructionGuidance("keep the same source, scope, and install_mode on the original and split skill_group blocks"),
	}
	if command, err := clipresent.ShellCommand("daem", "remove", "skill", resourceKey); err == nil {
		guidance = append(guidance, clipresent.InstructionGuidance("then rerun "+command+" --target <target> --dry-run"))
	}
	return guidance
}

func printMissingManifestInitHint(output io.Writer, manifestPath string, err error) {
	if guidance, present := missingManifestInitGuidance(manifestPath, err); present {
		fmt.Fprintln(output, guidance.HumanText())
	}
}

func missingManifestInitGuidance(manifestPath string, err error) (clipresent.Guidance, bool) {
	resolvedManifestPath, ok := missingManifestInitHintPath(manifestPath, err)
	if !ok {
		return clipresent.Guidance{}, false
	}
	return manifestCommandGuidance([]string{"init"}, resolvedManifestPath, creationHintOptions(), "--dry-run"), true
}

func manifestCommandGuidance(command []string, manifestPath string, options clipresent.HumanOptions, arguments ...string) clipresent.Guidance {
	argv := clipresent.ManifestCommandArguments(command, manifestPath, options, arguments...)
	if guidance, err := clipresent.CommandGuidance(argv, ""); err == nil {
		return guidance
	}
	return clipresent.InstructionGuidance("the follow-up arguments cannot be represented safely; inspect the selected manifest and retry manually")
}

func targetSelectionGuidance(manifestPath, requestedManifestPath string) []clipresent.Guidance {
	return []clipresent.Guidance{
		manifestCommandGuidance([]string{"lock"}, manifestPath, manifestHintOptions(requestedManifestPath, false), "--dry-run"),
		clipresent.NoteGuidance("omit --target to inspect all effective targets; accepted targets: " + supportedTargetValuesForHelp()),
	}
}

func missingManifestInitHintPath(manifestPath string, err error) (string, bool) {
	if !errors.Is(err, os.ErrNotExist) {
		return "", false
	}
	resolvedManifestPath, pathErr := workflowhelp.ManifestHintPath(manifestPath)
	if pathErr != nil {
		return "", false
	}
	if _, statErr := os.Stat(resolvedManifestPath); !errors.Is(statErr, os.ErrNotExist) {
		return "", false
	}
	return resolvedManifestPath, true
}
