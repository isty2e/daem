//go:build darwin || linux

package workdir

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

// Gate never makes an Armed record or grants filesystem authority.
// Its caller must confirm Armed before start and retain every live-group result.
type Gate struct {
	ctx              context.Context
	command          *exec.Cmd
	invocation       executionidentity.Invocation
	ready            *os.File
	childReady       *os.File
	release          *os.File
	childRelease     *os.File
	binding          executionidentity.Binding
	used             bool
	releaseAttempted bool
}

func PrepareGatedCommand(
	ctx context.Context,
	target string,
	args []string,
	env []string,
	directory *os.File,
	invocation executionidentity.Invocation,
) (*Gate, error) {
	if err := invocation.Validate(); err != nil {
		return nil, err
	}
	if err := Validate(directory); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.New("launch context is required")
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve gated descriptor helper: %w", err)
	}
	gate := &Gate{ctx: ctx, invocation: invocation}
	gate.ready, gate.childReady, err = os.Pipe()
	if err != nil {
		return nil, err
	}
	gate.childRelease, gate.release, err = os.Pipe()
	if err != nil {
		gate.Close()
		return nil, err
	}
	helperArgs := append([]string{descriptorGateArg, invocation.String(), target}, args...)
	command := exec.CommandContext(ctx, executable, helperArgs...)
	command.Env = appendEnvironmentValue(env, descriptorHelperEnv, descriptorGateMode)
	command.ExtraFiles = []*os.File{directory, gate.childRelease, gate.childReady}
	gate.command = command
	return gate, nil
}

func (gate *Gate) Command() *exec.Cmd { return gate.command }

func (gate *Gate) Binding() executionidentity.Binding { return gate.binding }

func (gate *Gate) ReleaseAttempted() bool { return gate.releaseAttempted }

// StartAndRelease requires the caller's dedicated-group configuration. Once the
// command has started, its caller retains responsibility for waiting and cleanup
// even when the handshake or durable publication fails.
func (gate *Gate) StartAndRelease(
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
	if err := command.Start(); err != nil {
		return err
	}
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

func (gate *Gate) closeChildEndpoints() {
	if gate.childReady != nil {
		_ = gate.childReady.Close()
	}
	if gate.childRelease != nil {
		_ = gate.childRelease.Close()
	}
}

func (gate *Gate) Close() {
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
