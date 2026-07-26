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

func lockCommandHint(manifestPath string) string {
	command, err := clipresent.ShellCommand("daem", "lock", "--manifest", manifestPath)
	if err != nil {
		return ""
	}
	return command
}

func printLockCommandHint(output io.Writer, manifestPath string) {
	clipresent.PrintShellCommand(output, "next: run ", "daem", "lock", "--manifest", manifestPath)
}

func printUnsupportedCapabilityHint(output io.Writer, err error) {
	target, ok := unsupportedCapabilityTarget(err)
	if !ok {
		return
	}

	command, commandErr := clipresent.ShellCommand("daem", "doctor", "--target", target)
	if commandErr != nil {
		return
	}
	fmt.Fprintf(output, "next: run %s\n", command)
	fmt.Fprintln(output, "note: inspect target capabilities")
}

func unsupportedCapabilityTarget(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	message := err.Error()
	if !strings.Contains(message, "is not implemented") {
		return "", false
	}

	const marker = `target "`
	start := strings.Index(message, marker)
	if start < 0 {
		return "", false
	}
	start += len(marker)
	end := strings.Index(message[start:], `"`)
	if end < 0 {
		return "", false
	}

	return message[start : start+end], true
}

func printTargetSelectionHint(output io.Writer, manifestPath string) {
	clipresent.PrintShellCommand(output, "next: run ", "daem", "lock", "--manifest", manifestPath, "--dry-run")
	fmt.Fprintf(output, "note: omit --target to inspect all effective targets; accepted targets: %s\n", supportedTargetValuesForHelp())
}

func printUnexpectedArgumentWithTargetHint(output io.Writer, arg string, targetValues targetFlagValues) {
	fmt.Fprintf(output, "unexpected argument %q\n", arg)
	if len(targetValues) == 0 || !isSupportedTargetLiteral(arg) {
		return
	}
	printTargetListSyntaxHint(output)
}

func printAuthoringArgumentCorrection(output io.Writer, path []string, err error, args []string) {
	if printUnexpectedArgumentTargetHintFromArgs(output, err, args) {
		return
	}
	printCommandHelpHint(output, path)
}

func printUnexpectedArgumentTargetHintFromArgs(output io.Writer, err error, args []string) bool {
	if err == nil || !strings.Contains(err.Error(), "unexpected argument") || !hasSpacedTargetListAfterTargetFlag(args) {
		return false
	}
	printTargetListSyntaxHint(output)
	return true
}

func printTargetListSyntaxHint(output io.Writer) {
	fmt.Fprintln(output, "next: repeat the flag: --target codex --target claude-code")
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

func printImportNothingToImportHint(output io.Writer, manifestPath string, merge bool) {
	fmt.Fprintln(output, "next: verify that the selected --target and --scope have live agent files to import")
	fmt.Fprintln(output, "next: try another selection, such as --scope global or a different --target")
	if merge {
		fmt.Fprintln(output, "next: confirm --manifest points to the existing manifest you want to merge into")
	} else {
		fmt.Fprintln(output, "next: choose the destination with --manifest <path>, or add --merge when importing into an existing manifest")
	}
	fmt.Fprintln(output, "next: if there is no live config to import, initialize the selected manifest and add resources explicitly")
	clipresent.PrintShellCommand(output, "next: run ", "daem", "init", "--manifest", manifestPath, "--dry-run")
}

func printLockMissingSourceHint(output io.Writer, manifestPath string, err error) {
	if !isMissingSourcePathError(err) {
		return
	}
	fmt.Fprintln(output, "next: create the missing source file or directory, edit the manifest source path, or remove the resource declaration")
	clipresent.PrintShellCommand(output, "next: run ", "daem", "lock", "--manifest", manifestPath, "--dry-run")
}

func isMissingSourcePathError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	if strings.Contains(message, "git source path ") {
		return false
	}
	return strings.Contains(message, "source path \"") && strings.Contains(message, "\" does not exist")
}

func printSkillGroupPartialTargetRemovalHint(output io.Writer, resourceKey string, remainingTargets []string) {
	targets := "<remaining-targets>"
	if len(remainingTargets) != 0 {
		targets = tomlStringArray(remainingTargets)
	}
	fmt.Fprintf(output, "next: edit the manifest so %q is in its own [[skill_group]] block with targets = %s\n", resourceKey, targets)
	fmt.Fprintln(output, "next: keep the same source, scope, and install_mode on the original and split skill_group blocks")
	if command, err := clipresent.ShellCommand("daem", "remove", "skill", resourceKey); err == nil {
		fmt.Fprintf(output, "next: then rerun %s --target <target> --dry-run\n", command)
	}
}

func printMissingManifestInitHint(output io.Writer, manifestPath string, err error) {
	if !errors.Is(err, os.ErrNotExist) {
		return
	}
	resolvedManifestPath, pathErr := workflowhelp.InitHintManifestPath(manifestPath)
	if pathErr != nil {
		return
	}
	if _, statErr := os.Stat(resolvedManifestPath); !errors.Is(statErr, os.ErrNotExist) {
		return
	}
	clipresent.PrintShellCommand(output, "next: run ", "daem", "init", "--manifest", resolvedManifestPath, "--dry-run")
}
