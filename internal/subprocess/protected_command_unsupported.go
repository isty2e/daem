//go:build !darwin && !linux

package subprocess

import "context"

func protectedCommandRunner(context.Context, CommandRequest, *protectedInvocationRun) CommandResult {
	return CommandResult{RunnerSetupFailed: true, Err: errProcessTreeUnsupported}
}
