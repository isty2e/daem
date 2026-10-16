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
	case "outdated":
		return prepareOutdatedInvocation(args[1:], stdout, stderr, options)
	case "init":
		return prepareInitInvocation(args[1:], stdout, stderr, options)
	case "doctor":
		return prepareDoctorInvocation(args[1:], stdout, stderr, options)
	case "import":
		return prepareImportInvocation(args[1:], stdout, stderr, options)
	case "recover":
		return prepareRecoverInvocation(args[1:], stdout, stderr, options)
	case "apply":
		return prepareApplyInvocation(args[1:], stdout, stderr, options)
	case "probe":
		if len(args) > 1 && args[1] == "mcp-server" {
			return prepareProbeMCPInvocation(args[2:], stdout, stderr, options)
		}
		return nil
	case "refresh":
		if len(args) > 1 && args[1] == "extension" {
			return prepareRefreshExtensionInvocation(args[2:], stdout, stderr, options)
		}
		return nil
	case "migrate":
		if len(args) > 1 && args[1] == "state" {
			return prepareMigrateStateInvocation(args[2:], stdout, stderr, options)
		}
		return nil
	case "list":
		if len(args) > 1 && isListSubject(args[1]) {
			return prepareListInvocation(args[1], args[2:], stdout, stderr, options)
		}
		return nil
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
