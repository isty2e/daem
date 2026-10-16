package cli

import (
	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

func renderAuthoringResult(response *commandResponse, operation clipresent.AuthoringOperation, resource clipresent.AuthoringResourceKind, result authoring.OperationResult, requestedManifestPath string, verbose, showDiff bool) int {
	if err := response.selectOrdinaryResult(); err != nil {
		return response.failure(operationFailure, humanFailureDiagnostic(err))
	}
	if response.output == requestedJSON {
		if err := clipresent.PrintManifestAuthoringJSON(response.stdout, clipresent.ManifestAuthoringJSONFrom(operation, resource, result)); err != nil {
			return response.failure(operationFailure, humanFailureDiagnostic(err).WithPrefix(string(operation)+" failed: write json: "))
		}
		return 0
	}
	label := string(operation)
	options := clipresent.HumanOptions{Verbose: verbose}
	if result.Mode != authoring.AuthoringModeDryRun {
		if operation == clipresent.AuthoringOperationAdd {
			label = "added"
		} else {
			label = "removed"
		}
		options = manifestHintOptions(requestedManifestPath, verbose)
	}
	clipresent.PrintAuthoringChangeWithOptions(response.stdout, clipresent.AuthoringChangeFrom(label, operation, resource, result), options)
	if showDiff {
		clipresent.PrintManifestDiff(response.stdout, result.ManifestPath, result.Original, result.ManifestPath, result.Content)
	}
	return 0
}
