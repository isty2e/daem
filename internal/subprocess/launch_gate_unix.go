//go:build darwin || linux

package subprocess

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"

	"github.com/isty2e/daem/internal/executionidentity"
	"github.com/isty2e/daem/internal/subprocess/executioncontext"
	"golang.org/x/sys/unix"
)

const (
	descriptorGateMode      = "gated-v1"
	descriptorGateArg       = "--daem-internal-gated-workdir-v1"
	descriptorReleaseFD     = 4
	descriptorReadyFD       = 5
	invocationGateFrameSize = 33
)

// invocationGate never makes an Armed record or grants filesystem authority.
// Its caller must confirm Armed before start and retain every live-group result.
type invocationGate struct {
	ctx              context.Context
	command          *exec.Cmd
	invocation       executionidentity.Invocation
	ready            *os.File
	childReady       *os.File
	release          *os.File
	childRelease     *os.File
	group            *ProcessGroup
	binding          executionidentity.Binding
	used             bool
	releaseAttempted bool
}

func prepareGatedCommandInWorkingDirectory(
	ctx context.Context,
	target string,
	args []string,
	env []string,
	directory *os.File,
	invocation executionidentity.Invocation,
) (*invocationGate, error) {
	if err := invocation.Validate(); err != nil {
		return nil, err
	}
	if err := ValidateWorkingDirectory(directory); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.New("launch context is required")
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve gated descriptor helper: %w", err)
	}
	gate := &invocationGate{ctx: ctx, invocation: invocation}
	gate.ready, gate.childReady, err = os.Pipe()
	if err != nil {
		return nil, err
	}
	gate.childRelease, gate.release, err = os.Pipe()
	if err != nil {
		gate.close()
		return nil, err
	}
	helperArgs := append([]string{descriptorGateArg, invocation.String(), target}, args...)
	command := exec.CommandContext(ctx, executable, helperArgs...)
	command.Env = appendEnvironmentValue(env, descriptorHelperEnv, descriptorGateMode)
	command.ExtraFiles = []*os.File{directory, gate.childRelease, gate.childReady}
	gate.command = command
	return gate, nil
}

func (gate *invocationGate) startAndRelease(
	confirm func(context.Context, executionidentity.Binding) error,
) error {
	if gate == nil || gate.used || gate.command == nil || gate.ctx == nil || confirm == nil || gate.invocation.Validate() != nil {
		return errors.New("one unused gate and binding publication are required")
	}
	ctx, command := gate.ctx, gate.command
	gate.used = true
	defer gate.closeChildEndpoints()
	defer gate.release.Close()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	current, err := executioncontext.ObserveExecutionContext(ctx)
	if err != nil {
		return err
	}
	group, err := BindProcessGroup(command)
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}
	gate.group = group
	gate.closeChildEndpoints()
	stopRead := context.AfterFunc(ctx, func() { _ = gate.ready.Close() })
	defer stopRead()
	var frame [invocationGateFrameSize]byte
	if _, err := io.ReadFull(gate.ready, frame[:]); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("read descriptor helper readiness: %w", err)
	}
	if string(frame[:]) != gate.invocation.String()+"\n" {
		return errors.New("descriptor helper readiness does not match the invocation")
	}
	pgid, err := unix.Getpgid(command.Process.Pid)
	if err != nil || pgid != command.Process.Pid {
		return errors.New("descriptor helper has no qualified dedicated group")
	}
	gate.binding, err = executionidentity.NewBinding(gate.invocation, current, int64(pgid))
	if err != nil {
		return err
	}
	if err := confirm(ctx, gate.binding); err != nil {
		return fmt.Errorf("confirm invocation binding: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	gate.releaseAttempted = true
	if _, err := io.WriteString(gate.release, gate.invocation.String()+"\n"); err != nil {
		return fmt.Errorf("release descriptor helper: %w", err)
	}
	return nil
}

func (gate *invocationGate) closeChildEndpoints() {
	if gate.childReady != nil {
		_ = gate.childReady.Close()
	}
	if gate.childRelease != nil {
		_ = gate.childRelease.Close()
	}
}

func (gate *invocationGate) close() {
	if gate == nil {
		return
	}
	gate.closeChildEndpoints()
	if gate.ready != nil {
		_ = gate.ready.Close()
	}
	if gate.release != nil {
		_ = gate.release.Close()
	}
}

func runGatedDescriptorHelper() {
	invocation, err := executionidentity.NewInvocation(os.Args[2])
	if err != nil {
		return
	}
	if err := unix.Fchdir(descriptorHelperFD); err != nil {
		writeDescriptorHelperError("working-directory setup", err)
		return
	}
	_ = unix.Close(descriptorHelperFD)
	ready := os.NewFile(descriptorReadyFD, "invocation-ready")
	release := os.NewFile(descriptorReleaseFD, "invocation-release")
	if ready == nil || release == nil {
		return
	}
	defer ready.Close()
	defer release.Close()
	var readyStat, releaseStat unix.Stat_t
	if unix.Fstat(descriptorReadyFD, &readyStat) != nil || readyStat.Mode&unix.S_IFMT != unix.S_IFIFO ||
		unix.Fstat(descriptorReleaseFD, &releaseStat) != nil || releaseStat.Mode&unix.S_IFMT != unix.S_IFIFO {
		return
	}
	if _, err := io.WriteString(ready, invocation.String()+"\n"); err != nil {
		return
	}
	_ = ready.Close()
	var frame [invocationGateFrameSize]byte
	if _, err := io.ReadFull(release, frame[:]); err != nil || string(frame[:]) != invocation.String()+"\n" {
		return
	}
	_ = release.Close()
	execDescriptorTarget(os.Args[3], os.Args[4:])
}
