package clipresent

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	probeworkflow "github.com/isty2e/daem/internal/workflow/probe"
)

// MCPProbeSelectionDiagnostic projects named selection failures, not executor errors.
func MCPProbeSelectionDiagnostic(err error) (FailureDiagnostic, bool) {
	kind := ResourceKindName(entity.KindMCPServer)
	var missing probeworkflow.NoAdmittedBindingError
	var ambiguous probeworkflow.AmbiguousBindingError
	var admission probeworkflow.BindingAdmissionError

	switch {
	case errors.As(err, &missing):
		return NewFailureDiagnostic(fmt.Sprintf(
			"%s %q has no admitted runtime-probe row matching target=%s scope=%s",
			kind, missing.Name, missing.Target, missing.Scope,
		)), true
	case errors.As(err, &ambiguous):
		return NewFailureDiagnostic(fmt.Sprintf(
			"%s %q is ambiguous across %d runtime-probe rows; narrow with --target and --scope",
			kind, ambiguous.Name, ambiguous.Matches,
		)), true
	case errors.As(err, &admission):
		return FailureDiagnosticFromError(admission.Err).WithPrefix(fmt.Sprintf(
			"%s %q runtime-probe admission: ", kind, admission.Name,
		)), true
	default:
		return FailureDiagnostic{}, false
	}
}
