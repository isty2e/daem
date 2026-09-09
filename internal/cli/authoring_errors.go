package cli

import (
	"errors"
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/workflow/authoring"
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

func printAuthoringOperationError(output io.Writer, command string, manifestPath string, err error) {
	fmt.Fprintf(output, "%s failed: %s\n", command, humanDiagnosticError(err))
	if authoringManifestInitHintApplies(err) {
		printMissingManifestInitHint(output, manifestPath, err)
	}
	printMissingResourceSelectionHint(output, manifestPath, err)
	clipresent.PrintMissingSourceHint(output, err)
}

func printMissingResourceSelectionHint(output io.Writer, manifestPath string, err error) {
	var missing *authoring.ResourceSelectionNotFoundError
	if !errors.As(err, &missing) {
		return
	}
	kind := map[string]string{
		"instructions": "instruction", "mcp_server": "mcp-server",
		"skill": "skill", "hook": "hook", "extension": "extension",
	}[missing.Kind]
	if kind == "" {
		kind = missing.Kind
	}
	for _, selection := range missing.AvailableSelections() {
		args := []string{"daem", "remove", kind, selection.Name, "--manifest", manifestPath}
		for _, target := range selection.Targets {
			args = append(args, "--target", target)
		}
		if selection.Scope != "" {
			args = append(args, "--scope", selection.Scope)
		}
		clipresent.PrintShellCommand(output, "hint: available selection: ", args...)
	}
}

func authoringManifestInitHintApplies(err error) bool {
	var operationErr authoring.OperationError
	if !errors.As(err, &operationErr) {
		return true
	}
	return operationErr.Phase == authoring.OperationPhaseLoadManifest ||
		operationErr.Phase == authoring.OperationPhaseBuildManifestChange
}
