package clipresent

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

func AuthoringSelectionDiagnostic(err error) (FailureDiagnostic, bool) {
	var missing *authoring.ResourceSelectionNotFoundError
	var ambiguous *authoring.ResourceSelectionAmbiguousError
	var message string
	switch {
	case errors.As(err, &missing):
		message = fmt.Sprintf("%s resource %q not found", ResourceKindName(missing.Kind), missing.Name)
		if missing.Detail != "" {
			message += " " + missing.Detail
		}
	case errors.As(err, &ambiguous):
		message = fmt.Sprintf("%s resource %q is ambiguous; narrow with --target/--scope", ResourceKindName(ambiguous.Kind), ambiguous.Name)
	default:
		return FailureDiagnostic{}, false
	}

	var projected error = errors.New(message)
	var operation authoring.OperationError
	if errors.As(err, &operation) {
		operation.Err = projected
		projected = operation
	}
	return FailureDiagnosticFromError(projected), true
}

func MCPServerNotFoundGuidance(err error) (Guidance, bool) {
	var missing *authoring.ResourceSelectionNotFoundError
	if !errors.As(err, &missing) || missing.Kind != entity.KindMCPServer {
		return Guidance{}, false
	}
	return InstructionGuidance("inspect declared [[mcp_server]] entries in the selected manifest, or add the server before removing it"), true
}
