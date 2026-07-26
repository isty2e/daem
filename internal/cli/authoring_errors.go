package cli

import (
	"errors"
	"fmt"
	"io"

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
}

func authoringManifestInitHintApplies(err error) bool {
	var operationErr authoring.OperationError
	if !errors.As(err, &operationErr) {
		return true
	}
	return operationErr.Phase == authoring.OperationPhaseLoadManifest ||
		operationErr.Phase == authoring.OperationPhaseBuildManifestChange
}
