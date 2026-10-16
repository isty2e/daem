package cli

import (
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
)

// preparedInvocation retains invocation-local metadata and a resolved output
// request before platform admission. Its execute closure owns all later effects.
type preparedInvocation struct {
	response *commandResponse
	execute  func() int
}

func prepareInvocation(args []string, stdout, stderr io.Writer, options commandOptions) *preparedInvocation {
	switch args[0] {
	case "status":
		return prepareStatusInvocation(args[1:], stdout, stderr, options)
	case "lock":
		return prepareLockInvocation(args[1:], stdout, stderr, options)
	default:
		return nil
	}
}

func flagBindingDiagnostic(path []string, err error) clipresent.FailureDiagnostic {
	diagnostic := humanFailureDiagnostic(err)
	if guidance, guidanceErr := clipresent.CommandGuidance(append([]string{"daem", "help"}, path...), ""); guidanceErr == nil {
		diagnostic = diagnostic.WithGuidance(guidance)
	}
	return diagnostic
}
