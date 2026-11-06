//go:build darwin || linux

package subprocess

import (
	"context"
	"os/exec"

	"github.com/isty2e/daem/internal/subprocess/executioncontext"
	"github.com/isty2e/daem/internal/subprocess/workdir"
)

func protectedCommandRunner(ctx context.Context, request CommandRequest, state *protectedInvocationRun) CommandResult {
	path, err := exec.LookPath(request.Command)
	if err != nil {
		return CommandResult{MissingRunner: true, Err: err}
	}
	gate, err := workdir.PrepareGatedCommand(ctx, path, request.Args, request.Env, request.nativeWorkDir, state.invocation)
	if err != nil {
		return CommandResult{WorkDirAuthorityFailed: true, Err: err}
	}
	defer gate.Close()
	command := gate.Command()
	var group *ProcessGroup
	result := runPreparedCommand(ctx, request, command, func() (*ProcessGroup, error) {
		var err error
		group, err = BindProcessGroup(command)
		if err != nil {
			return nil, err
		}
		err = gate.StartAndRelease(state.confirm)
		state.binding, state.releaseAttempted = gate.Binding(), gate.ReleaseAttempted()
		if command.Process == nil {
			group = nil
		}
		return group, err
	})
	if !state.releaseAttempted && group != nil {
		result.RunnerSetupFailed = true
		result.HasExitCode, result.ExitCode = false, 0
	}
	if state.binding.Validate() == nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), DefaultCommandTimeout)
		defer cancel()
		// A cached cancel cleanup can precede Darwin's final ESRCH. This wait is
		// only for the fresh live group; the qualified observation still decides.
		_, _ = waitForDedicatedProcessGroup(command.Process.Pid, defaultKillWait)
		state.quiescence, state.quiescenceErr = executioncontext.ObserveExecutionQuiescence(cleanupCtx, state.binding)
	}
	return result
}
