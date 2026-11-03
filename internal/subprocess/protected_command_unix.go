//go:build darwin || linux

package subprocess

import (
	"context"
	"os/exec"

	"github.com/isty2e/daem/internal/subprocess/executioncontext"
)

func protectedCommandRunner(ctx context.Context, request CommandRequest, state *protectedInvocationRun) CommandResult {
	path, err := exec.LookPath(request.Command)
	if err != nil {
		return CommandResult{MissingRunner: true, Err: err}
	}
	gate, err := prepareGatedCommandInWorkingDirectory(ctx, path, request.Args, request.Env, request.nativeWorkDir, state.invocation)
	if err != nil {
		return CommandResult{WorkDirAuthorityFailed: true, Err: err}
	}
	defer gate.close()
	result := runPreparedCommand(ctx, request, gate.command, func() (*ProcessGroup, error) {
		err := gate.startAndRelease(state.confirm)
		state.binding, state.releaseAttempted = gate.binding, gate.releaseAttempted
		return gate.group, err
	})
	if !state.releaseAttempted && gate.group != nil {
		result.RunnerSetupFailed = true
		result.HasExitCode, result.ExitCode = false, 0
	}
	if state.binding.Validate() == nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), DefaultCommandTimeout)
		defer cancel()
		// A cached cancel cleanup can precede Darwin's final ESRCH. This wait is
		// only for the fresh live group; the qualified observation still decides.
		_, _ = waitForDedicatedProcessGroup(gate.command.Process.Pid, defaultKillWait)
		state.quiescence, state.quiescenceErr = executioncontext.ObserveExecutionQuiescence(cleanupCtx, state.binding)
	}
	return result
}
