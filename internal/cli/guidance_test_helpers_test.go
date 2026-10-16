package cli

import (
	"fmt"
	"io"
)

func printAuthoringOperationError(output io.Writer, command, manifestPath string, err error) {
	fmt.Fprintln(output, authoringFailureDiagnostic(command, manifestPath, err).HumanText())
}

func printMissingResourceSelectionHint(output io.Writer, manifestPath string, err error) {
	for _, guidance := range missingResourceSelectionGuidance(manifestPath, err) {
		fmt.Fprintln(output, guidance.HumanText())
	}
}

func printTargetSelectionHint(output io.Writer, manifestPath, requestedManifestPath string) {
	for _, guidance := range targetSelectionGuidance(manifestPath, requestedManifestPath) {
		fmt.Fprintln(output, guidance.HumanText())
	}
}

func printUnexpectedArgumentTargetHintFromArgs(output io.Writer, err error, args []string) bool {
	guidance, present := unexpectedArgumentTargetGuidance(err, args)
	if present {
		fmt.Fprintln(output, guidance.HumanText())
	}
	return present
}

func printRemoveMCPServerNotFoundHint(output io.Writer, err error) {
	if guidance, present := removeMCPServerNotFoundGuidance(err); present {
		fmt.Fprintln(output, guidance.HumanText())
	}
}

func rejectUnsupportedPlatform(args []string, options commandOptions, stderr io.Writer) (int, bool) {
	diagnostic, rejected := platformRefusal(args, options)
	if !rejected {
		return 0, false
	}
	fmt.Fprintln(stderr, diagnostic.HumanText())
	return 1, true
}
